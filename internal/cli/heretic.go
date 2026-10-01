package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/adamsiwiec1/runhug/internal/config"
	"github.com/adamsiwiec1/runhug/internal/hf"
	"github.com/adamsiwiec1/runhug/internal/runpod"
	"github.com/adamsiwiec1/runhug/internal/sizing"
	"github.com/adamsiwiec1/runhug/internal/store"
)

const (
	hereticDefaultTrials = 200
	hereticDefaultCloud  = "SECURE"
)

func cmdHeretic(args []string) error {
	if len(args) == 0 {
		return hereticHelp()
	}
	switch args[0] {
	case "make", "train", "run", "launch":
		return cmdHereticMake(args[1:])
	case "logs":
		return cmdHereticLogs(args[1:])
	case "stop":
		return cmdHereticStop(args[1:])
	case "status":
		return cmdHereticStatus(args[1:])
	default:
		if showCmdHelp(args, "runhug heretic", printHereticHelp, printHereticHelpFull) {
			return nil
		}
		return fmt.Errorf("unknown heretic command %q\nRun `runhug heretic --help` or `runhug heretic --help-full`", args[0])
	}
}

func hereticHelp() error {
	printHereticHelp(os.Stdout)
	return nil
}

func printHereticHelp(w io.Writer) {
	helpUsage(w, "runhug heretic <command>")
	fmt.Fprintln(w, dim("Abliteration training pod on RunPod · live dashboard · optional HF upload"))
	fmt.Fprintln(w)

	helpSection(w, "commands")
	helpCmd(w, "make <model>", "create training pod + follow progress")
	helpCmd(w, "train", "alias for make")
	helpCmd(w, "logs <model>", "tail training log")
	helpCmd(w, "status [model]", "pod status + dashboard link")
	helpCmd(w, "stop <model>", "terminate pod")
	fmt.Fprintln(w)

	helpSection(w, "make flags")
	helpFlag(w, "--gpu --gpu-count", "pool / GPU count (auto-sized)")
	helpFlag(w, "--trials --disk", "heretic trials / container disk")
	helpFlag(w, "--no-upload", "keep weights on pod (skip HF)")
	helpFlag(w, "--yes --dry-run --json", "confirm / plan-only / JSON")
	fmt.Fprintln(w)

	fmt.Fprintf(w, "%s %s\n", dim("pass-through:"), cyan("runhug heretic make <m> -- --seed 1337"))
}

func hereticHelpText() string {
	var b strings.Builder
	printHereticHelp(&b)
	return b.String()
}

func cmdHereticMake(args []string) error {
	fs := newFlagSet("heretic make")
	yes := fs.Bool("yes", false, "create without a prompt")
	dry := fs.Bool("dry-run", false, "print the plan only")
	asJSON := fs.Bool("json", false, "print JSON (implies --no-follow)")
	noFollow := fs.Bool("no-follow", false, "print the dashboard URL and exit")
	gpu := fs.String("gpu", "", "pod GPU pool (ADA_24, AMPERE_80, …)")
	gpuCount := fs.Int("gpu-count", 0, "GPUs (default 1, sized up if needed)")
	trials := fs.Int("trials", hereticDefaultTrials, "heretic trials")
	disk := fs.Int("disk", 0, "container disk GB (0 = repo-implied + 50)")
	cloud := fs.String("cloud", hereticDefaultCloud, "SECURE or COMMUNITY")
	keepAlive := fs.Int("keep-alive", 30, "minutes to stay up after training")
	maxMin := fs.Int("max-min", 720, "hard stop after N minutes of training")
	noUpload := fs.Bool("no-upload", false, "keep the model on the pod disk")
	private := fs.Bool("private", false, "create the upload repo as private")
	uploadRepo := fs.String("upload-repo-id", "", "HF repo to upload to")
	image := fs.String("image", runpod.DefaultHereticImage, "pod image")
	regUser := fs.String("registry-user", "", "registry username for a private --image (ghcr.io: your GitHub name)")
	regToken := fs.String("registry-token", os.Getenv("RUNHUG_REGISTRY_TOKEN"), "registry token for a private --image")
	name := fs.String("name", "", "pod name")
	token := fs.String("token", "", "HF token override (default: stored / env)")
	dashToken := fs.String("dashboard-token", "", "optional shared secret for the dashboard")
	var hereticArgs stringsFlag
	fs.Var(&hereticArgs, "heretic-arg", "extra flag to pass to heretic (repeatable)")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	if fs.NArg() < 1 {
		return fmt.Errorf("usage: runhug heretic make <org/model> [heretic flags…]")
	}
	modelKey := fs.Arg(0)
	passThrough := strings.Join(fs.Args()[1:], " ")
	follow := !*noFollow && !*asJSON

	env := config.Load()
	if err := env.RequireRunpod(); err != nil {
		return err
	}
	hfTok := *token
	if hfTok == "" {
		hfTok = env.HFToken
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	hfClient := hf.New(hfTok)
	model, err := hfClient.Get(ctx, modelKey)
	if err != nil {
		return fmt.Errorf("heretic needs a Hugging Face model id: %w", err)
	}
	modelID := model.RepoID()
	format := hf.DetectFormat(*model)
	if format.Engine == hf.EngineGGUF {
		return fmt.Errorf("%s is GGUF; heretic fine-tunes a transformers checkpoint, not GGUF", modelID)
	}
	if model.IsGated() && hfTok == "" {
		return fmt.Errorf("%s is gated; set HF_TOKEN or pass --token (the pod will 401 on download)", modelID)
	}

	est := sizing.EstimateModel(*model, format, 8192)
	rp := runpod.New(env.RunpodAPIKey)
	gpus, err := rp.ListPodGPUs(ctx)
	if err != nil {
		return err
	}
	choice, err := runpod.PickPod(gpus, est.RequiredGB, *gpu, *gpuCount)
	if err != nil {
		return err
	}
	hourly := choice.HourlyUSD

	diskGB := *disk
	if diskGB <= 0 {
		// base weights + heretic caches, outputs, and the saved/uploaded model.
		diskGB = int(math.Ceil(est.WeightGB)) + 50
	}
	podName := *name
	if podName == "" {
		podName = "heretic-" + hereticSlug(modelID)
	}

	action := "upload"
	if *noUpload {
		action = "save"
	}
	uploadRepoID := *uploadRepo
	if action == "upload" {
		if uploadRepoID == "" {
			if hfTok == "" {
				return fmt.Errorf("uploading to Hugging Face needs HF_TOKEN; set it or pass --no-upload")
			}
			user, err := hfClient.Whoami(ctx)
			if err != nil {
				return fmt.Errorf("Hugging Face token check failed: %w", err)
			}
			if user == "" {
				return fmt.Errorf("could not resolve your Hugging Face username")
			}
			uploadRepoID = user + "/" + "heretic-" + hereticSlug(modelID)
		}
	}

	workerEnv := map[string]string{
		"HERETIC_MODEL":          modelID,
		"HERETIC_TRIALS":         fmt.Sprintf("%d", *trials),
		"HERETIC_ACTION":         action,
		"DASHBOARD_PORT":         "8080",
		"RUNHUH_KEEP_ALIVE_MIN":  fmt.Sprintf("%d", *keepAlive),
		"RUNHUH_MAX_RUNTIME_MIN": fmt.Sprintf("%d", *maxMin),
	}
	if action == "upload" {
		workerEnv["HERETIC_UPLOAD_REPO"] = uploadRepoID
		if *private {
			workerEnv["HERETIC_UPLOAD_PRIVATE"] = "1"
		}
	}
	if passThrough != "" {
		workerEnv["HERETIC_ARGS"] = passThrough
	}
	if len(hereticArgs) > 0 {
		if workerEnv["HERETIC_ARGS"] != "" {
			workerEnv["HERETIC_ARGS"] += " "
		}
		workerEnv["HERETIC_ARGS"] += strings.Join(hereticArgs, " ")
	}
	if hfTok != "" {
		workerEnv["HF_TOKEN"] = hfTok
	}
	if *dashToken != "" {
		workerEnv["RUNHUH_TOKEN"] = *dashToken
	}

	req := runpod.CreatePodRequest{
		Name:  podName,
		Image: *image,
		Disk:  diskGB,
		Ports: []string{"8080/http"},
		Env:   workerEnv,
		GPU: &runpod.PodGPU{
			ID:            choice.GPUTypeID,
			Count:         choice.GPUCount,
			MinVcpuPerGpu: 8,
			MinRamPerGpu:  int(math.Ceil(choice.Pool.MemoryGB)),
		},
		Cloud: strings.ToUpper(strings.TrimSpace(*cloud)),
	}
	if *regUser != "" && *regToken != "" {
		registryID, err := rp.RegistryCredentialFor(ctx, "runhug-heretic", *regUser, *regToken)
		if err != nil {
			return fmt.Errorf("container registry credential: %w", err)
		}
		req.Registry = registryID
	} else {
		// Reuse a credential stored by an earlier run (same name) so private
		// GHCR images pull without re-supplying --registry-user/--registry-token.
		regs, err := rp.ListRegistryCredentials(ctx)
		if err == nil {
			for _, r := range regs {
				if r.Name == "runhug-heretic" && r.ID != "" {
					req.Registry = r.ID
					break
				}
			}
		}
	}

	printHereticPlan(modelID, format, est, choice, hourly, req, action, uploadRepoID, *trials, env.HFToken != "")

	if *dry {
		if *asJSON {
			return writeJSON(map[string]any{"pod": req, "choice": choice, "estimate": est})
		}
		fmt.Println(green("✓") + " " + dim("dry-run: nothing created"))
		return nil
	}
	if !*yes {
		if !confirm(fmt.Sprintf("Create this pod? GPU ~$%.2f/hr for up to %d min.", hourly, *maxMin)) {
			return fmt.Errorf("aborted")
		}
	}

	created, err := rp.CreatePod(ctx, req)
	if err != nil {
		if runpod.IsOutOfStock(err) {
			fmt.Fprintf(os.Stdout, "%s %s is out of stock; retrying…\n", dim("|"), yellow(req.GPU.ID))
			candidates := runpod.FittingPodCards(gpus, est.RequiredGB)
			for _, cand := range candidates {
				if cand.ID == req.GPU.ID {
					continue
				}
				req.GPU.ID = cand.ID
				hourly = cand.PodHourlyPrice() * float64(req.GPU.Count)
				fmt.Fprintf(os.Stdout, "%s trying %s (~$%.2f/hr)…\n", dim("|"), cand.Name, hourly)
				created, err = rp.CreatePod(ctx, req)
				if err == nil {
					break
				}
				if !runpod.IsOutOfStock(err) {
					return err
				}
			}
		}
		if err != nil {
			return err
		}
	}

	reg, _, err := store.Load()
	if err != nil {
		return err
	}
	dashboard := runpod.PodProxyURL(created.ID, runpod.DefaultDashboardPort)
	reg.Put(store.Model{
		HFRepo:        modelID,
		PodID:         created.ID,
		PodCloud:      req.Cloud,
		DashboardURL:  dashboard,
		DashboardPort: runpod.DefaultDashboardPort,
		GPUPool:       req.GPU.ID,
		GPUCount:      req.GPU.Count,
		Image:         req.Image,
		HourlyUSD:     hourly,
		CreatedAt:     time.Now().UTC(),
	})
	reg.Current = modelID
	if err := reg.Save(); err != nil {
		fmt.Fprintf(os.Stderr, "%s saved pod %s but registry write failed: %v\n", yellow("warning:"), created.ID, err)
	}

	result := map[string]any{
		"pod_id":        created.ID,
		"name":          created.Name,
		"model":         modelID,
		"gpu_pool":      req.GPU.ID,
		"gpu_count":     req.GPU.Count,
		"hourly_usd":    hourly,
		"cloud":         req.Cloud,
		"action":        action,
		"upload_repo":   uploadRepoID,
		"dashboard_url": dashboard,
		"status":        created.Status,
	}

	if *asJSON {
		return writeJSON(result)
	}

	fmt.Fprintln(os.Stdout)
	fmt.Fprintf(os.Stdout, "%s  %s  (%s)\n", green("Created"), cyan(created.ID), created.Name)
	printKV(os.Stdout, "model", bold(modelID))
	if hub := hubLink(modelID); hub != "" {
		printKV(os.Stdout, "hub", cyan(hub))
	}
	printKV(os.Stdout, "dashboard", cyan(dashboard))
	if action == "upload" {
		printKV(os.Stdout, "upload", cyan(uploadRepoID))
	}
	printKV(os.Stdout, "billing", fmt.Sprintf("%.2f GB disk, ~$%.2f/hr while running on %s ×%d", float64(req.Disk), hourly, req.GPU.ID, req.GPU.Count))
	fmt.Fprintln(os.Stdout)

	if !follow {
		commands(os.Stdout, "Next:",
			"runhug heretic logs "+modelID,
			"runhug heretic stop "+modelID,
		)
		return nil
	}

	fmt.Fprintln(os.Stdout, dim("Provisioning pod (dashboard + heretic)…"))
	return followHeretic(ctx, rp, created.ID, modelID, dashboard, action, uploadRepoID, *dashToken)
}

func hereticSlug(modelID string) string {
	s := strings.ToLower(strings.TrimSpace(modelID))
	s = strings.ReplaceAll(s, "/", "-")
	s = strings.Trim(s, "-")
	if s == "" {
		s = "model"
	}
	if len(s) > 64 {
		s = s[:64]
	}
	return s
}

func printHereticPlan(modelID string, format hf.Format, est sizing.Estimate, c runpod.Choice, hourly float64, req runpod.CreatePodRequest, action, uploadRepoID string, trials int, hasHF bool) {
	planHeading(os.Stdout, "Heretic plan")
	printKV(os.Stdout, "model", bold(modelID))
	fmt.Fprintf(os.Stdout, "  %s  %s", dim(padRight("format", 9)), cyan(string(format.Engine)))
	if format.Quant != "" {
		fmt.Fprintf(os.Stdout, " (%s)", yellow(format.Quant))
	}
	fmt.Println()
	if est.WeightGB > 0 {
		printKV(os.Stdout, "weights", fmt.Sprintf("%s → %s on a %.0f GB card",
			cyan(fmt.Sprintf("%.1f GB", est.WeightGB)),
			bold(fmt.Sprintf("%.1f GB", est.RequiredGB)), c.Pool.MemoryGB))
	}
	gpuID := c.GPUTypeID
	if gpuID == "" {
		gpuID = c.Pool.ID
	}
	printKV(os.Stdout, "gpu", fmt.Sprintf("%s ×%d  %s  %s/hr pod  stock %s",
		cyan(gpuID), c.GPUCount, c.Pool.ExampleGPU,
		green(fmt.Sprintf("~$%.2f", hourly)), stockLabel(c.Pool.Availability)))
	printKV(os.Stdout, "why", dim(c.Reason))
	printKV(os.Stdout, "trials", bold(fmt.Sprintf("%d", trials)))
	if action == "upload" {
		if uploadRepoID != "" {
			printKV(os.Stdout, "upload", cyan(uploadRepoID))
		}
	} else {
		printKV(os.Stdout, "upload", yellow("off — model stays on pod disk (ephemeral!)"))
	}
	printKV(os.Stdout, "cloud", cyan(req.Cloud))
	printKV(os.Stdout, "image", dim(req.Image))
	printKV(os.Stdout, "disk", fmt.Sprintf("%d GB", req.Disk))
	printKV(os.Stdout, "cost", highlightUSD(fmt.Sprintf("~%s/hr while the pod runs (billed until stop)", formatUSDPlain(hourly))))
	keys := make([]string, 0, len(req.Env))
	for k := range req.Env {
		if k == "HF_TOKEN" || k == "RUNHUH_TOKEN" {
			continue
		}
		keys = append(keys, k+"="+req.Env[k])
	}
	printKV(os.Stdout, "env", dim(strings.Join(keys, "  ")))
	if hasHF {
		printKV(os.Stdout, "hf_token", green("set (not printed)"))
	}
	fmt.Fprintln(os.Stdout)
}

func formatUSDPlain(v float64) string {
	if v < 0.01 && v > 0 {
		return fmt.Sprintf("$%.4f", v)
	}
	return fmt.Sprintf("$%.2f", v)
}

func followHeretic(ctx context.Context, rp *runpod.Client, podID, modelID, dashboard, action, uploadRepoID, dashToken string) error {
	last := ""
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	pollCtx := func() (context.Context, context.CancelFunc) {
		return context.WithTimeout(ctx, 20*time.Second)
	}

	// Phase 1: wait for RUNNING.
	for {
		c, cancel := pollCtx()
		pod, err := rp.GetPod(c, podID)
		cancel()
		if err != nil {
			return fmt.Errorf("pod status: %w", err)
		}
		switch {
		case pod.Running():
			if last != runpod.PodStatusRunning {
				fmt.Fprintf(os.Stdout, "%s  %s  %s\n", dateTime(), cyan(pod.Status), dim(dashboard))
			}
			last = runpod.PodStatusRunning
			goto training
		case pod.Terminal():
			return fmt.Errorf("pod %s ended %s before training started", podID, pod.Status)
		default:
			if pod.Status != last {
				fmt.Fprintf(os.Stdout, "%s  %s  %s\n", dateTime(), cyan(pod.Status), dim("(provisioning)"))
				last = pod.Status
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-ticker.C:
			}
		}
	}

training:
	host := &dashboardClient{base: dashboard, token: dashToken}
	offset := 0
	printedStatus := false
	var firstFail time.Time
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
		c, cancel := pollCtx()
		pod, err := rp.GetPod(c, podID)
		cancel()
		if err == nil && pod.Terminal() && !printedStatus {
			fmt.Fprintf(os.Stdout, "%s  %s\n", dateTime(), yellow("pod "+pod.Status))
		}

		st, err := host.fetchStatus()
		if err != nil {
			if firstFail.IsZero() {
				firstFail = time.Now()
			}
			if time.Since(firstFail) > 2*time.Minute {
				return fmt.Errorf("dashboard unreachable for 2 minutes while the pod is running: %w", err)
			}
			continue
		}
		firstFail = time.Time{}

		if !printedStatus {
			fmt.Fprintf(os.Stdout, "%s  %s  %s\n", dateTime(), green("training"), bold(modelID))
			if action == "upload" && uploadRepoID != "" {
				fmt.Fprintf(os.Stdout, "%s  upload target %s\n", dim("|"), uploadRepoID)
			}
			printedStatus = true
		}

		lg, err := host.fetchLogs(offset)
		if err == nil {
			for _, line := range lg.Lines {
				fmt.Fprintln(os.Stdout, line)
			}
			offset = lg.Offset
		}
		if st.Done {
			return printHereticResult(st)
		}
	}
}

func printHereticResult(st hereticStatus) error {
	fmt.Fprintln(os.Stdout)
	if st.Error != "" {
		fmt.Fprintf(os.Stdout, "%s  %s\n", red("heretic failed"), st.Error)
		return fmt.Errorf("training failed: %s", st.Error)
	}
	fmt.Fprintf(os.Stdout, "%s  %s\n", green("heretic done"), bold(st.Model))
	printKV(os.Stdout, "trials", fmt.Sprintf("%d/%d", st.TrialsDone, st.TrialsTotal))
	refTotal := st.RefusalsTotal
	if refTotal == 0 {
		refTotal = 100
	}
	printKV(os.Stdout, "refusals", fmt.Sprintf("%d/%d remaining", st.BestRefusals, refTotal))
	printKV(os.Stdout, "kl", fmt.Sprintf("%.4f", st.BestKL))
	printKV(os.Stdout, "elapsed", fmt.Sprintf("%dm%02ds", st.ElapsedSec/60, st.ElapsedSec%60))
	if st.UploadRepoID != "" {
		printKV(os.Stdout, "upload", cyan(st.UploadRepoID))
		printKV(os.Stdout, "hub", cyan(hubLink(st.UploadRepoID)))
	}
	fmt.Fprintln(os.Stdout)
	commands(os.Stdout, "Next:",
		"runhug heretic logs "+st.Model,
		"runhug heretic stop "+st.Model,
	)
	return nil
}

func dateTime() string {
	return dim(time.Now().Format("15:04:05"))
}

// hereticStatus mirrors the pod dashboard status file.
type hereticStatus struct {
	Model         string  `json:"model"`
	Status        string  `json:"status"`
	Message       string  `json:"message"`
	TrialsDone    int     `json:"trials_done"`
	TrialsTotal   int     `json:"trials_total"`
	BestRefusals  int     `json:"best_refusals"`
	RefusalsTotal int     `json:"refusals_total"`
	BestKL        float64 `json:"best_kl"`
	Trials        []struct {
		Index    int     `json:"index"`
		Refusals int     `json:"refusals"`
		KL       float64 `json:"kl"`
		Params   string  `json:"params"`
	} `json:"trials,omitempty"`
	UploadRepoID string `json:"upload_repo_id"`
	OutputPath   string `json:"output_path"`
	ElapsedSec   int    `json:"elapsed_sec"`
	Error        string `json:"error"`
	Done         bool   `json:"done"`
}

type hereticLogs struct {
	Lines  []string `json:"lines"`
	Offset int      `json:"offset"`
	Total  int      `json:"total"`
	Done   bool     `json:"done"`
}

type dashboardClient struct {
	base  string
	token string
}

func (d *dashboardClient) get(path string, out any) error {
	u := d.base + path
	if d.token != "" {
		sep := "?"
		if strings.Contains(path, "?") {
			sep = "&"
		}
		u += sep + "token=" + url.QueryEscape(d.token)
	}
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	c := &http.Client{Timeout: 8 * time.Second}
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("dashboard %s -> %s: %s", path, resp.Status, strings.TrimSpace(string(b)))
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (d *dashboardClient) fetchStatus() (hereticStatus, error) {
	var st hereticStatus
	err := d.get("/status.json", &st)
	return st, err
}

func (d *dashboardClient) fetchLogs(offset int) (hereticLogs, error) {
	var lg hereticLogs
	err := d.get(fmt.Sprintf("/logs?offset=%d", offset), &lg)
	return lg, err
}

func cmdHereticLogs(args []string) error {
	fs := newFlagSet("heretic logs")
	tail := fs.Int("tail", 0, "print only the last N lines (0 = all)")
	token := fs.String("token", "", "dashboard token (only if the pod was created with --dashboard-token)")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	reg, _, err := store.Load()
	if err != nil {
		return err
	}
	key := ""
	if fs.NArg() > 0 {
		key = fs.Arg(0)
	}
	m, ok := reg.Lookup(key)
	if !ok || m.PodID == "" {
		return fmt.Errorf("no heretic pod for %q (run `heretic status`)", fs.Arg(0))
	}
	if m.DashboardURL == "" {
		return fmt.Errorf("no dashboard URL recorded for %q", m.HFRepo)
	}
	host := &dashboardClient{base: m.DashboardURL, token: *token}
	st, err := host.fetchStatus()
	if err != nil {
		return fmt.Errorf("dashboard unreachable: %w", err)
	}
	lg, err := host.fetchLogs(0)
	if err != nil {
		return err
	}
	lines := lg.Lines
	if *tail > 0 && len(lines) > *tail {
		lines = lines[len(lines)-*tail:]
	}
	if !st.Done && st.Status == "" && len(lines) == 0 {
		fmt.Fprintln(os.Stdout, dim("no log output yet — the pod is still provisioning"))
		return nil
	}
	for _, line := range lines {
		fmt.Fprintln(os.Stdout, line)
	}
	return nil
}

func cmdHereticStop(args []string) error {
	fs := newFlagSet("heretic stop")
	yes := fs.Bool("yes", false, "terminate without a prompt")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	if fs.NArg() < 1 {
		return fmt.Errorf("usage: runhug heretic stop <model>")
	}
	env := config.Load()
	if err := env.RequireRunpod(); err != nil {
		return err
	}
	reg, _, err := store.Load()
	if err != nil {
		return err
	}
	m, ok := reg.Lookup(fs.Arg(0))
	if !ok || m.PodID == "" {
		return fmt.Errorf("no heretic pod for %q (run `heretic status`)", fs.Arg(0))
	}
	if !*yes && !confirm(fmt.Sprintf("Terminate pod %s (%s)? Training results are lost unless uploaded.", cyan(m.PodID), m.HFRepo)) {
		return fmt.Errorf("aborted")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := runpod.New(env.RunpodAPIKey).DeletePod(ctx, m.PodID); err != nil {
		return err
	}
	reg.Remove(m.HFRepo)
	if err := reg.Save(); err != nil {
		fmt.Fprintf(os.Stderr, "%s pod deleted but registry write failed: %v\n", yellow("warning:"), err)
	}
	fmt.Printf("%s %s\n", red("terminated"), cyan(m.PodID))
	return nil
}

func cmdHereticStatus(args []string) error {
	fs := newFlagSet("heretic status")
	asJSON := fs.Bool("json", false, "print JSON")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	env := config.Load()
	if err := env.RequireRunpod(); err != nil {
		return err
	}
	reg, _, err := store.Load()
	if err != nil {
		return err
	}
	rp := runpod.New(env.RunpodAPIKey)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	key := ""
	if fs.NArg() > 0 {
		key = fs.Arg(0)
	}
	type podRow struct {
		RepoID string
		store.Model
	}
	rows := make([]podRow, 0, len(reg.Models))
	if key != "" {
		m, ok := reg.Lookup(key)
		if !ok {
			return fmt.Errorf("unknown model %q", key)
		}
		if m.PodID == "" {
			return fmt.Errorf("%q is not a heretic pod", m.HFRepo)
		}
		rows = append(rows, podRow{RepoID: m.HFRepo, Model: m})
	} else {
		for _, m := range reg.Models {
			if m.PodID == "" {
				continue
			}
			rows = append(rows, podRow{RepoID: m.HFRepo, Model: m})
		}
	}
	if len(rows) == 0 {
		return fmt.Errorf("no heretic pods in the registry (run `heretic make <org/model>`)")
	}

	out := make([]map[string]any, 0, len(rows))
	for _, r := range rows {
		pod, err := rp.GetPod(ctx, r.PodID)
		if err != nil {
			return err
		}
		if *asJSON {
			rec := map[string]any{
				"model":         r.HFRepo,
				"pod_id":        r.PodID,
				"status":        pod.Status,
				"cloud":         pod.Cloud,
				"gpu":           poolGPUName(pod.GPU),
				"dashboard_url": r.DashboardURL,
			}
			if pod.Runtime != nil && pod.Runtime.Ports != nil {
				rec["uptime_sec"] = pod.Runtime.Uptime
			}
			out = append(out, rec)
			continue
		}
		status := pod.Status
		if pod.Terminal() {
			status = yellow(status)
		} else if pod.Running() {
			status = green(status)
		}
		fmt.Fprintf(os.Stdout, "%s  %s\n", bold(r.HFRepo), status)
		printKV(os.Stdout, "pod", cyan(r.PodID))
		printKV(os.Stdout, "cloud", strings.ToUpper(r.PodCloud))
		printKV(os.Stdout, "gpu", fmt.Sprintf("%s ×%d", r.GPUPool, r.GPUCount))
		printKV(os.Stdout, "dashboard", cyan(r.DashboardURL))
		if pod.Runtime != nil && pod.Runtime.Uptime > 0 {
			printKV(os.Stdout, "uptime", fmt.Sprintf("%dm%02ds", pod.Runtime.Uptime/60, pod.Runtime.Uptime%60))
		}
		if r.HourlyUSD > 0 {
			printKV(os.Stdout, "billing", fmt.Sprintf("~$%.2f/hr", r.HourlyUSD))
		}
		fmt.Fprintln(os.Stdout)
	}
	if *asJSON {
		return writeJSON(out)
	}
	return nil
}

func poolGPUName(g *runpod.PodGPU) string {
	if g == nil {
		return ""
	}
	if g.ID != "" {
		return g.ID
	}
	return ""
}
