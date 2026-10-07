package cli

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/adamsiwiec1/runhug/internal/config"
	"github.com/adamsiwiec1/runhug/internal/gcp"
	"github.com/adamsiwiec1/runhug/internal/jobs"
	"github.com/adamsiwiec1/runhug/internal/runtime"
	"github.com/adamsiwiec1/runhug/internal/sizing"
	"github.com/adamsiwiec1/runhug/internal/store"
)

// EnsureOpts controls EnsureReady behavior.
type EnsureOpts struct {
	ServeModel string
	APIKeyEnv  string
	Timeout    time.Duration // 0 = backend default
	LocalPort  int           // GCP; 0 → BaseURL port or LocalTunnelPort
	Writer     io.Writer     // progress; default os.Stderr
	GCP        *gcp.Client   // optional test inject
}

// ReadyHandle is a chat target that is (or was just made) reachable.
type ReadyHandle struct {
	Target  EndpointTarget
	Cleanup func() // stop background tunnel if we started one; always non-nil
}

// EnsureReady starts cloud resources if needed and waits until OpenAI /v1/models
// responds. Local backends only probe (and may start ollama). Skipped by callers
// when the user passed an explicit --base-url.
func EnsureReady(ctx context.Context, m store.Model, opts EnsureOpts) (ReadyHandle, error) {
	w := opts.Writer
	if w == nil {
		w = os.Stderr
	}
	cleanup := func() {}
	switch m.Kind() {
	case store.BackendGCP:
		return ensureGCP(ctx, m, opts, w)
	case store.BackendLocal:
		h, err := ensureLocal(ctx, m, opts, w)
		if err != nil {
			return ReadyHandle{Cleanup: cleanup}, err
		}
		return h, nil
	default:
		return ensureRunpod(ctx, m, opts, w)
	}
}

func ensureLocal(ctx context.Context, m store.Model, opts EnsureOpts, w io.Writer) (ReadyHandle, error) {
	env := config.Load()
	key := resolveAPIKey(opts.APIKeyEnv, env.RunpodAPIKey)
	target, err := targetFromModel(m, key, opts.ServeModel)
	if err != nil {
		return ReadyHandle{Cleanup: func() {}}, err
	}
	heading(w, "Ensure")
	printKV(w, "model", bold(m.HFRepo))
	printKV(w, "backend", cyan("local"))
	printKV(w, "base_url", cyan(target.BaseURL))
	fmt.Fprintln(w)

	if err := probeOpenAIModels(ctx, target.BaseURL, target.APIKey); err == nil {
		printEnsureStep(w, 1, 1, "ready", target.BaseURL)
		fmt.Fprintln(w)
		return ReadyHandle{Target: target, Cleanup: func() {}}, nil
	}
	if strings.EqualFold(m.Runtime, runtime.Ollama) {
		printEnsureStep(w, 1, 2, "ollama", "starting serve…")
		if err := runtime.EnsureOllama(""); err != nil {
			return ReadyHandle{Cleanup: func() {}}, err
		}
		if err := waitOpenAIReady(ctx, target.BaseURL, target.APIKey, 2*time.Second, 45*time.Second, w, 2, 2); err != nil {
			return ReadyHandle{Cleanup: func() {}}, fmt.Errorf("local ollama not ready: %w", err)
		}
		printEnsureStep(w, 2, 2, "ready", target.BaseURL)
		fmt.Fprintln(w)
		return ReadyHandle{Target: target, Cleanup: func() {}}, nil
	}
	return ReadyHandle{Cleanup: func() {}}, fmt.Errorf(
		"local endpoint not reachable at %s\nHint: run `runhug local start --model %s`",
		target.BaseURL, m.HFRepo,
	)
}

func ensureRunpod(ctx context.Context, m store.Model, opts EnsureOpts, w io.Writer) (ReadyHandle, error) {
	env := config.Load()
	if err := env.RequireRunpod(); err != nil {
		return ReadyHandle{Cleanup: func() {}}, err
	}
	key := resolveAPIKey(opts.APIKeyEnv, env.RunpodAPIKey)
	target, err := targetFromModel(m, key, opts.ServeModel)
	if err != nil {
		return ReadyHandle{Cleanup: func() {}}, err
	}
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = 8 * time.Minute
	}
	coldMin, coldMax := sizing.ColdStartSeconds(0)
	heading(w, "Ensure")
	printKV(w, "model", bold(m.HFRepo))
	printKV(w, "backend", green("runpod"))
	printKV(w, "endpoint", cyan(m.EndpointID))
	printKV(w, "wait", dim(fmt.Sprintf("est. cold start ~%d–%ds if no warm workers", coldMin, coldMax)))
	fmt.Fprintln(w)

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	if err := probeOpenAIModels(ctx, target.BaseURL, target.APIKey); err == nil {
		printEnsureStep(w, 1, 2, "workers", "already warm")
		printEnsureStep(w, 2, 2, "ready", target.BaseURL)
		fmt.Fprintln(w)
		return ReadyHandle{Target: target, Cleanup: func() {}}, nil
	}

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if err := ctx.Err(); err != nil {
			return ReadyHandle{Cleanup: func() {}}, fmt.Errorf("runpod not ready: %w", err)
		}
		status := "checking…"
		if h, err := jobs.Health(key, m.EndpointID); err == nil && h != nil && h.Workers != nil {
			wr := h.Workers
			status = fmt.Sprintf("ready=%s running=%s initializing=%s",
				itoaPtr(wr.Ready), itoaPtr(wr.Running), itoaPtr(wr.Initializing))
			if ptrInt(wr.Ready) > 0 || ptrInt(wr.Idle) > 0 || ptrInt(wr.Running) > 0 {
				printEnsureStep(w, 1, 2, "workers", status)
				if err := waitOpenAIReady(ctx, target.BaseURL, target.APIKey, 2*time.Second, 90*time.Second, w, 2, 2); err == nil {
					printEnsureStep(w, 2, 2, "ready", target.BaseURL)
					fmt.Fprintln(w)
					return ReadyHandle{Target: target, Cleanup: func() {}}, nil
				}
			} else {
				printEnsureStep(w, 1, 2, "workers", status+" — waking…")
			}
		} else {
			printEnsureStep(w, 1, 2, "workers", status)
		}
		// Probing /models can itself enqueue work and wake a cold endpoint.
		if err := probeOpenAIModels(ctx, target.BaseURL, target.APIKey); err == nil {
			printEnsureStep(w, 2, 2, "ready", target.BaseURL)
			fmt.Fprintln(w)
			return ReadyHandle{Target: target, Cleanup: func() {}}, nil
		}
		select {
		case <-ctx.Done():
			return ReadyHandle{Cleanup: func() {}}, fmt.Errorf("runpod not ready within %s — try `runhug status %s`", timeout, m.HFRepo)
		case <-time.After(4 * time.Second):
		}
	}
	return ReadyHandle{Cleanup: func() {}}, fmt.Errorf("runpod not ready within %s — try `runhug status %s`", timeout, m.HFRepo)
}

func ensureGCP(ctx context.Context, m store.Model, opts EnsureOpts, w io.Writer) (ReadyHandle, error) {
	client := opts.GCP
	if client == nil {
		client = gcp.NewClient()
	}
	project := strings.TrimSpace(m.EndpointID)
	zone := strings.TrimSpace(m.EndpointType)
	instance := strings.TrimSpace(m.PodID)
	if instance == "" {
		instance = gcp.InstanceName(m.HFRepo)
	}
	if project == "" || zone == "" {
		return ReadyHandle{Cleanup: func() {}}, fmt.Errorf(
			"gcp registry entry incomplete (need project + zone)\nHint: redeploy with `runhug gcp deploy %s --project <id>`",
			m.HFRepo,
		)
	}
	localPort := gcpListenPort(opts, m)
	// GCP chat always goes through the local SSH forward.
	baseURL := gcp.LocalOpenAIURL(localPort)
	tok, _ := gcp.LoadBearer(instance)
	apiKey := tok
	if apiKey == "" {
		apiKey = resolveAPIKey(opts.APIKeyEnv, "")
	}
	modelName := strings.TrimSpace(opts.ServeModel)
	if modelName == "" {
		modelName = m.UpstreamModel()
	}
	target := EndpointTarget{
		BaseURL:  baseURL,
		APIKey:   apiKey,
		Model:    modelName,
		Source:   "gcp " + instance,
		HFRepo:   m.HFRepo,
		Sampling: m.Sampling,
	}

	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = 25 * time.Minute
	}
	cost := gcp.EstimateSpotCost(gcp.GPUTarget{Name: gcp.GPUTypeL4, MachineType: gcp.MachineTypeL4}, 600, false, 0)
	heading(w, "Ensure")
	printKV(w, "model", bold(m.HFRepo))
	printKV(w, "backend", yellow("gcp"))
	printKV(w, "instance", cyan(instance))
	printKV(w, "project", project)
	printKV(w, "zone", zone)
	printKV(w, "wait", dim(fmt.Sprintf("est. ~%d–%d min if cold (Spot boot + GGUF load)", cost.ColdStartMinM, cost.ColdStartMaxM)))
	printKV(w, "local", cyan(fmt.Sprintf("127.0.0.1:%d → instance :%d", localPort, gcp.ServerPort)))
	fmt.Fprintln(w)

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cleanup := func() {}
	printEnsureStep(w, 1, 4, "probe", fmt.Sprintf("GET %s/models", strings.TrimRight(target.BaseURL, "/")))
	if err := probeOpenAIModels(ctx, target.BaseURL, target.APIKey); err == nil {
		printEnsureStep(w, 1, 4, "probe", green("already answering"))
		printEnsureStep(w, 4, 4, "ready", target.BaseURL)
		fmt.Fprintln(w)
		return ReadyHandle{Target: target, Cleanup: cleanup}, nil
	} else {
		printEnsureStep(w, 1, 4, "probe", dim(formatProbeErr(err)+" — starting ensure"))
	}

	printEnsureStep(w, 1, 4, "auth", "checking gcloud ADC…")
	if err := client.RequireADC(ctx); err != nil {
		return ReadyHandle{Cleanup: cleanup}, err
	}
	if acc := client.CurrentAccount(ctx); acc != "" {
		printEnsureStep(w, 1, 4, "auth", "connected as "+acc)
	} else {
		printEnsureStep(w, 1, 4, "auth", green("ADC present"))
	}

	printEnsureStep(w, 1, 4, "instance", fmt.Sprintf("describing %s in %s/%s…", instance, project, zone))
	st, err := client.DescribeInstance(ctx, project, zone, instance)
	if err != nil {
		return ReadyHandle{Cleanup: cleanup}, fmt.Errorf("gcp describe %s: %w\nHint: this gcloud account needs compute.instances.get on %s — `gcloud config set account` then retry. Redeploy only if the VM is gone (`runhug gcp deploy`)", instance, err, project)
	}
	status := strings.ToUpper(st.Status)
	switch status {
	case "RUNNING":
		printEnsureStep(w, 1, 4, "instance", green("RUNNING"))
	case "STAGING", "PROVISIONING", "STARTING":
		printEnsureStep(w, 1, 4, "instance", yellow(status)+" — waiting for RUNNING…")
		if _, err := waitGCPInstanceLogged(ctx, client, project, zone, instance, "RUNNING", w); err != nil {
			return ReadyHandle{Cleanup: cleanup}, err
		}
		printEnsureStep(w, 1, 4, "instance", green("RUNNING"))
	case "TERMINATED", "STOPPED", "SUSPENDED":
		printEnsureStep(w, 1, 4, "instance", yellow(status)+" — sending start…")
		if err := client.StartInstance(ctx, project, zone, instance); err != nil {
			return ReadyHandle{Cleanup: cleanup}, fmt.Errorf("gcp start: %w", err)
		}
		printEnsureStep(w, 1, 4, "instance", "start accepted — waiting for RUNNING (Spot boot)…")
		if _, err := waitGCPInstanceLogged(ctx, client, project, zone, instance, "RUNNING", w); err != nil {
			return ReadyHandle{Cleanup: cleanup}, err
		}
		printEnsureStep(w, 1, 4, "instance", green("RUNNING"))
	default:
		return ReadyHandle{Cleanup: cleanup}, fmt.Errorf("gcp instance %s status %s — cannot start chat", instance, st.Status)
	}

	// Tunnel (non-interactive SSH; retry via IAP when the VM has no public IP / direct SSH fails).
	if runtime.PortOpen(localPort) {
		printEnsureStep(w, 2, 4, "tunnel", fmt.Sprintf("127.0.0.1:%d already open — checking llama…", localPort))
		hit := inspectLocalHTTP(ctx, target.BaseURL, target.APIKey)
		if hit.err == nil {
			printEnsureStep(w, 2, 4, "tunnel", green(fmt.Sprintf("reusing 127.0.0.1:%d", localPort)))
			printEnsureStep(w, 4, 4, "ready", green(target.BaseURL))
			fmt.Fprintln(w)
			return ReadyHandle{Target: target, Cleanup: cleanup}, nil
		}
		if hit.foreign {
			printEnsureStep(w, 2, 4, "tunnel", yellow(fmt.Sprintf("127.0.0.1:%d is a local app (%s), not llama — opening SSH on %d", localPort, hit.who, gcp.LocalTunnelPort)))
			if localPort == gcp.LocalTunnelPort {
				return ReadyHandle{Cleanup: cleanup}, fmt.Errorf("127.0.0.1:%d is %s, not the GCP tunnel — stop that process or pass a free --local-port", localPort, hit.who)
			}
			localPort = gcp.LocalTunnelPort
			target.BaseURL = gcp.LocalOpenAIURL(localPort)
			if runtime.PortOpen(localPort) {
				hit2 := inspectLocalHTTP(ctx, target.BaseURL, target.APIKey)
				if hit2.err == nil {
					printEnsureStep(w, 2, 4, "tunnel", green(fmt.Sprintf("reusing 127.0.0.1:%d", localPort)))
					printEnsureStep(w, 4, 4, "ready", green(target.BaseURL))
					fmt.Fprintln(w)
					return ReadyHandle{Target: target, Cleanup: cleanup}, nil
				}
				if hit2.foreign {
					return ReadyHandle{Cleanup: cleanup}, fmt.Errorf("127.0.0.1:%d is also a local app (%s) — free that port", localPort, hit2.who)
				}
			}
		} else {
			printEnsureStep(w, 2, 4, "tunnel", dim(fmt.Sprintf("port open but %s — will wait on this forward", formatProbeErr(hit.err))))
		}
	}
	if !runtime.PortOpen(localPort) || inspectLocalHTTP(ctx, target.BaseURL, target.APIKey).foreign {
		printEnsureStep(w, 2, 4, "tunnel", fmt.Sprintf("attempting SSH -L %d:127.0.0.1:%d to %s…", localPort, gcp.ServerPort, instance))
		th, err := startGCPTunnelLogged(ctx, client, project, zone, instance, localPort, false, w)
		if err != nil {
			printEnsureStep(w, 2, 4, "tunnel", yellow("direct SSH failed — attempting IAP tunnel…"))
			th, err = startGCPTunnelLogged(ctx, client, project, zone, instance, localPort, true, w)
		}
		if err != nil {
			return ReadyHandle{Cleanup: cleanup}, err
		}
		cleanup = th.Stop
		printEnsureStep(w, 2, 4, "tunnel", green(fmt.Sprintf("connected 127.0.0.1:%d → instance :%d", localPort, gcp.ServerPort)))
	}

	printEnsureStep(w, 3, 4, "openai", "waiting for llama.cpp (GGUF load)  "+jobKeysHint())
	retunnel := func() error {
		cleanup()
		cleanup = func() {}
		printEnsureStep(w, 2, 4, "tunnel", fmt.Sprintf("reopening SSH -L %d:127.0.0.1:%d…", localPort, gcp.ServerPort))
		th, err := startGCPTunnelLogged(ctx, client, project, zone, instance, localPort, false, w)
		if err != nil {
			printEnsureStep(w, 2, 4, "tunnel", yellow("direct SSH failed — attempting IAP tunnel…"))
			th, err = startGCPTunnelLogged(ctx, client, project, zone, instance, localPort, true, w)
		}
		if err != nil {
			return err
		}
		cleanup = th.Stop
		printEnsureStep(w, 2, 4, "tunnel", green(fmt.Sprintf("connected 127.0.0.1:%d → instance :%d", localPort, gcp.ServerPort)))
		return nil
	}
	if err := waitGCPLlama(ctx, client, project, zone, instance, target, w, 3, 4, retunnel); err != nil {
		cleanup()
		return ReadyHandle{Cleanup: func() {}}, fmt.Errorf(
			"gcp OpenAI not ready: %w\nHint: VM :8080 never served — check guest docker/startup (often no Cloud NAT / no public IP)",
			err,
		)
	}
	printEnsureStep(w, 4, 4, "ready", green(target.BaseURL))
	fmt.Fprintln(w)
	return ReadyHandle{Target: target, Cleanup: cleanup}, nil
}

func startGCPTunnel(ctx context.Context, client *gcp.Client, project, zone, instance string, localPort int, throughIAP bool) (*gcp.TunnelHandle, error) {
	return startGCPTunnelLogged(ctx, client, project, zone, instance, localPort, throughIAP, nil)
}

func startGCPTunnelLogged(ctx context.Context, client *gcp.Client, project, zone, instance string, localPort int, throughIAP bool, w io.Writer) (*gcp.TunnelHandle, error) {
	via := "direct SSH"
	if throughIAP {
		via = "IAP"
	}
	if w != nil {
		printEnsureStep(w, 2, 4, "tunnel", fmt.Sprintf("starting %s forward (project %s zone %s)…", via, project, zone))
	}
	th, err := client.StartTunnelBackground(ctx, gcp.TunnelOpts{
		Project:    project,
		Zone:       zone,
		Instance:   instance,
		RemotePort: gcp.ServerPort,
		LocalPort:  localPort,
		ThroughIAP: throughIAP,
	})
	if err != nil {
		return nil, err
	}
	started := time.Now()
	deadline := started.Add(90 * time.Second)
	var lastLog time.Time
	for {
		left := time.Until(deadline)
		if left <= 0 {
			th.Stop()
			return nil, fmt.Errorf("timeout waiting for 127.0.0.1:%d after %s tunnel", localPort, via)
		}
		slice := 10 * time.Second
		if slice > left {
			slice = left
		}
		err := th.WaitLocal(ctx, slice)
		if err == nil {
			return th, nil
		}
		if ctx.Err() != nil || strings.Contains(err.Error(), "exited") {
			th.Stop()
			return nil, err
		}
		if w != nil && time.Since(lastLog) >= 9*time.Second {
			printEnsureStep(w, 2, 4, "tunnel", fmt.Sprintf("waiting for local %d to open (%s, %s elapsed)…", localPort, via, time.Since(started).Truncate(time.Second)))
			lastLog = time.Now()
		}
	}
}

func waitGCPInstanceLogged(ctx context.Context, client *gcp.Client, project, zone, name, want string, w io.Writer) (*gcp.InstanceStatus, error) {
	started := time.Now()
	want = strings.ToUpper(strings.TrimSpace(want))
	var lastStatus string
	lastPrint := time.Time{}
	for {
		if err := ctx.Err(); err != nil {
			if lastStatus != "" {
				return nil, fmt.Errorf("%w (last status %s after %s)", err, lastStatus, time.Since(started).Truncate(time.Second))
			}
			return nil, err
		}
		st, err := client.DescribeInstance(ctx, project, zone, name)
		if err != nil {
			return nil, err
		}
		status := strings.ToUpper(st.Status)
		changed := status != lastStatus
		lastStatus = status
		if strings.EqualFold(status, want) {
			return st, nil
		}
		if w != nil && (changed || time.Since(lastPrint) >= 10*time.Second) {
			printEnsureStep(w, 1, 4, "instance", fmt.Sprintf("still %s — %s elapsed, polling describe…", status, time.Since(started).Truncate(time.Second)))
			lastPrint = time.Now()
		}
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("%w (last status %s)", ctx.Err(), lastStatus)
		case <-time.After(5 * time.Second):
		}
	}
}

func printEnsureStep(w io.Writer, n, total int, label, value string) {
	fmt.Fprintf(w, "  %s  %s  %s\n",
		cyan(fmt.Sprintf("%d/%d", n, total)),
		dim(padRight(label, 10)),
		value,
	)
}

func resolveAPIKey(apiKeyEnv, fallback string) string {
	if apiKeyEnv != "" {
		if v := config.SanitizeAPIKey(os.Getenv(apiKeyEnv)); v != "" {
			return v
		}
	}
	return fallback
}

func gcpListenPort(opts EnsureOpts, m store.Model) int {
	if opts.LocalPort > 0 {
		return opts.LocalPort
	}
	p := localPortFromBaseURL(m.BaseURL)
	// Registry used to store the remote llama port (8080). That collides with
	// local Node/dev servers — SSH always listens on LocalTunnelPort (18080).
	if p == 0 || p == gcp.ServerPort {
		return gcp.LocalTunnelPort
	}
	return p
}

type localHTTPHit struct {
	err     error
	status  int
	who     string
	foreign bool
}

func inspectLocalHTTP(ctx context.Context, baseURL, apiKey string) localHTTPHit {
	base := strings.TrimRight(NormalizeOpenAIBase(baseURL), "/")
	if base == "" {
		return localHTTPHit{err: fmt.Errorf("empty base url")}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/models", nil)
	if err != nil {
		return localHTTPHit{err: err}
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	client := &http.Client{Timeout: 8 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		return localHTTPHit{err: err}
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 1<<20))
	who := strings.TrimSpace(res.Header.Get("X-Powered-By"))
	if who == "" {
		who = strings.TrimSpace(res.Header.Get("Server"))
	}
	hit := localHTTPHit{status: res.StatusCode, who: who}
	if res.StatusCode >= 200 && res.StatusCode < 300 {
		return hit
	}
	hit.err = fmt.Errorf("HTTP %d", res.StatusCode)
	low := strings.ToLower(who)
	if strings.Contains(low, "express") || strings.Contains(low, "next.js") || strings.Contains(low, "webpack") {
		hit.foreign = true
		if hit.who == "" {
			hit.who = "local HTTP"
		}
	}
	if res.StatusCode == 404 && who != "" && !strings.Contains(low, "llama") {
		hit.foreign = true
	}
	return hit
}

func localPortFromBaseURL(base string) int {
	base = strings.TrimSpace(base)
	if base == "" {
		return 0
	}
	u, err := url.Parse(base)
	if err != nil {
		return 0
	}
	if u.Port() != "" {
		p, err := strconv.Atoi(u.Port())
		if err == nil && p > 0 {
			return p
		}
	}
	switch u.Scheme {
	case "https":
		return 443
	case "http":
		return 80
	}
	return 0
}

func probeOpenAIModels(ctx context.Context, baseURL, apiKey string) error {
	base := strings.TrimRight(NormalizeOpenAIBase(baseURL), "/")
	if base == "" {
		return fmt.Errorf("empty base url")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/models", nil)
	if err != nil {
		return err
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	client := &http.Client{Timeout: 8 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 1<<20))
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return fmt.Errorf("HTTP %d", res.StatusCode)
	}
	return nil
}

func waitGCPLlama(ctx context.Context, client *gcp.Client, project, zone, instance string, target EndpointTarget, w io.Writer, step, total int, retunnel func() error) error {
	started := time.Now()
	deadline := time.Now().Add(remainingOr(ctx, 20*time.Minute))
	if dl, ok := ctx.Deadline(); ok && dl.Before(deadline) {
		deadline = dl
	}
	healed := false
	lastPrint := time.Time{}
	lastGuest := time.Time{}
	guestLine := ""
	var last error
	n := 0
	for time.Now().Before(deadline) {
		if err := ctx.Err(); err != nil {
			if last != nil {
				return fmt.Errorf("%w (%v)", err, last)
			}
			return err
		}
		n++
		if err := probeOpenAIModels(ctx, target.BaseURL, target.APIKey); err == nil {
			printEnsureStep(w, step, total, "openai", green(fmt.Sprintf("connected in %s", time.Since(started).Truncate(time.Second))))
			return nil
		} else {
			last = err
		}
		if time.Since(lastGuest) >= 20*time.Second {
			lastGuest = time.Now()
			out, gerr := client.GuestExec(ctx, gcp.GuestOpts{
				Project:  project,
				Zone:     zone,
				Instance: instance,
				Command:  gcp.GuestBootCommand(),
				Timeout:  50 * time.Second,
			})
			if gerr == nil {
				guestLine = gcp.SummarizeGuestLogs(out)
				if !healed && gcp.GuestNeedsEgress(out) {
					healed = true
					printEnsureStep(w, step, total, "openai", yellow("guest has no egress — adding ephemeral IP and resetting the VM"))
					if err := client.EnsureExternalIP(ctx, project, zone, instance); err != nil {
						return fmt.Errorf("add external IP: %w (project has no Cloud NAT; llama never bound :8080)", err)
					}
					if err := client.ResetInstance(ctx, project, zone, instance); err != nil {
						return fmt.Errorf("reset instance after adding IP: %w", err)
					}
					select {
					case <-ctx.Done():
						return ctx.Err()
					case <-time.After(20 * time.Second):
					}
					if retunnel != nil {
						if err := retunnel(); err != nil {
							printEnsureStep(w, 2, 4, "tunnel", dim("will retry tunnel: "+err.Error()))
						}
					}
					guestLine = "guest: reset with public IP — installing docker / pulling GGUF"
				}
			}
		}
		if w != nil && (n == 1 || time.Since(lastPrint) >= 12*time.Second) {
			detail := guestLine
			if detail == "" {
				detail = formatProbeErr(last)
			}
			printEnsureStep(w, step, total, "openai", fmt.Sprintf("%s  elapsed %s", detail, time.Since(started).Truncate(time.Second)))
			lastPrint = time.Now()
		}
		select {
		case <-ctx.Done():
			if last != nil {
				return fmt.Errorf("%w (%v)", ctx.Err(), last)
			}
			return ctx.Err()
		case <-time.After(3 * time.Second):
		}
	}
	if guestLine != "" {
		return fmt.Errorf("timed out: %s (%v)", guestLine, last)
	}
	if last != nil {
		return fmt.Errorf("timed out: %w", last)
	}
	return fmt.Errorf("timed out waiting for llama.cpp")
}

func waitOpenAIReady(ctx context.Context, baseURL, apiKey string, every, maxWait time.Duration, w io.Writer, step, total int) error {
	if every <= 0 {
		every = 2 * time.Second
	}
	started := time.Now()
	deadline := started.Add(maxWait)
	if dl, ok := ctx.Deadline(); ok && dl.Before(deadline) {
		deadline = dl
	}
	if step < 1 {
		step, total = 1, 1
	}
	url := strings.TrimRight(NormalizeOpenAIBase(baseURL), "/") + "/models"
	var last error
	n := 0
	lastPrint := time.Time{}
	for time.Now().Before(deadline) {
		if err := ctx.Err(); err != nil {
			if last != nil {
				return fmt.Errorf("%w (%v)", err, last)
			}
			return err
		}
		n++
		if err := probeOpenAIModels(ctx, baseURL, apiKey); err == nil {
			if w != nil {
				printEnsureStep(w, step, total, "openai", green(fmt.Sprintf("connected %s in %s", url, time.Since(started).Truncate(time.Second))))
			}
			return nil
		} else {
			last = err
			if w != nil && (n == 1 || time.Since(lastPrint) >= 10*time.Second) {
				left := time.Until(deadline).Truncate(time.Second)
				if left < 0 {
					left = 0
				}
				printEnsureStep(w, step, total, "openai", fmt.Sprintf("attempt %d  %s  elapsed %s  left %s", n, formatProbeErr(err), time.Since(started).Truncate(time.Second), left))
				lastPrint = time.Now()
			}
		}
		select {
		case <-ctx.Done():
			if last != nil {
				return fmt.Errorf("%w (%v)", ctx.Err(), last)
			}
			return ctx.Err()
		case <-time.After(every):
		}
	}
	if last != nil {
		return fmt.Errorf("timed out: %w", last)
	}
	return fmt.Errorf("timed out waiting for %s", url)
}

func formatProbeErr(err error) string {
	if err == nil {
		return "ok"
	}
	s := err.Error()
	switch {
	case strings.Contains(s, "HTTP 404"):
		return "HTTP 404 (SSH up, llama.cpp /v1/models not serving yet — GGUF still loading)"
	case strings.Contains(s, "HTTP 502"), strings.Contains(s, "HTTP 503"), strings.Contains(s, "HTTP 504"):
		return s + " (proxy up, backend not ready)"
	case strings.Contains(s, "connection refused"):
		return "connection refused (tunnel or llama not listening)"
	case strings.Contains(s, "connection reset"):
		return "connection reset (SSH up, nothing listening on guest :8080)"
	case strings.Contains(s, "i/o timeout"), strings.Contains(s, "Timeout"), strings.Contains(s, "deadline"):
		return "timed out contacting /v1/models"
	default:
		if len(s) > 120 {
			return s[:117] + "…"
		}
		return s
	}
}

func remainingOr(ctx context.Context, fallback time.Duration) time.Duration {
	if dl, ok := ctx.Deadline(); ok {
		d := time.Until(dl)
		if d > 0 {
			return d
		}
		return time.Second
	}
	return fallback
}

func ptrInt(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}

// resolveReadyEndpoint picks/ensures a chat target. Explicit --base-url skips ensure.
func resolveReadyEndpoint(registryKey, baseURL, apiKeyEnv, serveModel string, skipEnsure bool) (EndpointTarget, func(), error) {
	noop := func() {}
	if strings.TrimSpace(baseURL) != "" || skipEnsure {
		t, err := ResolveEndpoint(registryKey, baseURL, apiKeyEnv, serveModel)
		return t, noop, err
	}
	registryKey = strings.TrimSpace(registryKey)
	if registryKey == "" {
		t, err := ResolveEndpoint("", "", apiKeyEnv, serveModel)
		return t, noop, err
	}
	reg, _, err := store.Load()
	if err != nil {
		return EndpointTarget{}, noop, err
	}
	m, ok := reg.Lookup(registryKey)
	if !ok {
		t, err := ResolveEndpoint(registryKey, "", apiKeyEnv, serveModel)
		return t, noop, err
	}
	h, err := EnsureReady(context.Background(), m, EnsureOpts{
		ServeModel: serveModel,
		APIKeyEnv:  apiKeyEnv,
		Writer:     os.Stderr,
	})
	if err != nil {
		return EndpointTarget{}, noop, err
	}
	return h.Target, h.Cleanup, nil
}
