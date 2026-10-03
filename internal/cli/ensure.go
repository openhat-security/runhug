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
		if err := waitOpenAIReady(ctx, target.BaseURL, target.APIKey, 2*time.Second, 45*time.Second); err != nil {
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
				if err := waitOpenAIReady(ctx, target.BaseURL, target.APIKey, 2*time.Second, 90*time.Second); err == nil {
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
	localPort := opts.LocalPort
	if localPort <= 0 {
		localPort = localPortFromBaseURL(m.BaseURL)
	}
	if localPort <= 0 {
		localPort = gcp.LocalTunnelPort
	}
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
	printKV(w, "wait", dim(fmt.Sprintf("est. ~%d–%d min if cold (Spot boot + GGUF load)", cost.ColdStartMinM, cost.ColdStartMaxM)))
	fmt.Fprintln(w)

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cleanup := func() {}
	// Fast path: tunnel + llama already answering.
	if err := probeOpenAIModels(ctx, target.BaseURL, target.APIKey); err == nil {
		printEnsureStep(w, 1, 4, "instance", "already reachable")
		printEnsureStep(w, 4, 4, "ready", target.BaseURL)
		fmt.Fprintln(w)
		return ReadyHandle{Target: target, Cleanup: cleanup}, nil
	}

	if err := client.RequireADC(ctx); err != nil {
		return ReadyHandle{Cleanup: cleanup}, err
	}

	printEnsureStep(w, 1, 4, "instance", "checking status…")
	st, err := client.DescribeInstance(ctx, project, zone, instance)
	if err != nil {
		return ReadyHandle{Cleanup: cleanup}, fmt.Errorf("gcp describe %s: %w\nHint: deploy with `runhug gcp deploy`", instance, err)
	}
	status := strings.ToUpper(st.Status)
	switch status {
	case "RUNNING":
		printEnsureStep(w, 1, 4, "instance", green("RUNNING"))
	case "STAGING", "PROVISIONING", "STARTING":
		printEnsureStep(w, 1, 4, "instance", yellow(status)+" — waiting…")
		if _, err := client.WaitInstanceStatus(ctx, project, zone, instance, "RUNNING", 5*time.Second); err != nil {
			return ReadyHandle{Cleanup: cleanup}, err
		}
		printEnsureStep(w, 1, 4, "instance", green("RUNNING"))
	case "TERMINATED", "STOPPED", "SUSPENDED":
		printEnsureStep(w, 1, 4, "instance", yellow(status)+" — starting (cold)…")
		if err := client.StartInstance(ctx, project, zone, instance); err != nil {
			return ReadyHandle{Cleanup: cleanup}, fmt.Errorf("gcp start: %w", err)
		}
		if _, err := client.WaitInstanceStatus(ctx, project, zone, instance, "RUNNING", 5*time.Second); err != nil {
			return ReadyHandle{Cleanup: cleanup}, err
		}
		printEnsureStep(w, 1, 4, "instance", green("RUNNING"))
	default:
		return ReadyHandle{Cleanup: cleanup}, fmt.Errorf("gcp instance %s status %s — cannot start chat", instance, st.Status)
	}

	// Tunnel
	if runtime.PortOpen(localPort) {
		printEnsureStep(w, 2, 4, "tunnel", fmt.Sprintf("127.0.0.1:%d (reusing)", localPort))
	} else {
		printEnsureStep(w, 2, 4, "tunnel", fmt.Sprintf("opening 127.0.0.1:%d…", localPort))
		th, err := client.StartTunnelBackground(ctx, gcp.TunnelOpts{
			Project:    project,
			Zone:       zone,
			Instance:   instance,
			RemotePort: gcp.ServerPort,
			LocalPort:  localPort,
		})
		if err != nil {
			return ReadyHandle{Cleanup: cleanup}, err
		}
		cleanup = th.Stop
		if err := th.WaitLocal(ctx, 2*time.Minute); err != nil {
			cleanup()
			return ReadyHandle{Cleanup: func() {}}, err
		}
		printEnsureStep(w, 2, 4, "tunnel", green(fmt.Sprintf("127.0.0.1:%d", localPort)))
	}

	printEnsureStep(w, 3, 4, "openai", "waiting /v1/models…")
	if err := waitOpenAIReady(ctx, target.BaseURL, target.APIKey, 3*time.Second, remainingOr(ctx, 20*time.Minute)); err != nil {
		cleanup()
		return ReadyHandle{Cleanup: func() {}}, fmt.Errorf(
			"gcp OpenAI not ready: %w\nHint: check `runhug gcp status %s` — container may still be pulling the GGUF",
			err, m.HFRepo,
		)
	}
	printEnsureStep(w, 4, 4, "ready", green(target.BaseURL))
	fmt.Fprintln(w)
	return ReadyHandle{Target: target, Cleanup: cleanup}, nil
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

func waitOpenAIReady(ctx context.Context, baseURL, apiKey string, every, maxWait time.Duration) error {
	if every <= 0 {
		every = 2 * time.Second
	}
	deadline := time.Now().Add(maxWait)
	if dl, ok := ctx.Deadline(); ok && dl.Before(deadline) {
		deadline = dl
	}
	var last error
	for time.Now().Before(deadline) {
		if err := ctx.Err(); err != nil {
			if last != nil {
				return fmt.Errorf("%w (%v)", err, last)
			}
			return err
		}
		if err := probeOpenAIModels(ctx, baseURL, apiKey); err == nil {
			return nil
		} else {
			last = err
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
	return fmt.Errorf("timed out waiting for %s/models", strings.TrimRight(baseURL, "/"))
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
