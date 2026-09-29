package cli

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/adamsiwiec1/runhug/internal/config"
	"github.com/adamsiwiec1/runhug/internal/family"
	"github.com/adamsiwiec1/runhug/internal/hf"
	"github.com/adamsiwiec1/runhug/internal/jobs"
	"github.com/adamsiwiec1/runhug/internal/runpod"
	"github.com/adamsiwiec1/runhug/internal/sizing"
	"github.com/adamsiwiec1/runhug/internal/store"
)

func cmdDeploy(args []string) error {
	// Peek --provider before RunPod flag parse so GCP-only flags (--project, …) work.
	if p, ok := peekProvider(args); ok {
		switch strings.ToLower(p) {
		case "gcp", "google", "gce":
			return cmdGCPDeploy(stripProviderFlag(args))
		case "", "runpod":
			// continue
		default:
			return fmt.Errorf("unknown --provider %q (want runpod or gcp)", p)
		}
	}
	fs := newFlagSet("deploy")
	provider := fs.String("provider", "runpod", "runpod (default) or gcp")
	yes := fs.Bool("yes", false, "create without a prompt")
	dry := fs.Bool("dry-run", false, "print the plan only")
	estimate := fs.Bool("estimate", false, "print full approximate cost block on the plan")
	fs.BoolVar(estimate, "e", false, "alias for --estimate")
	gpu := fs.String("gpu", "", "serverless GPU pool (ADA_24, AMPERE_80, …)")
	gpuCount := fs.Int("gpu-count", 0, "GPUs per worker (default: sized)")
	maxLen := fs.Int("max-len", 8192, "MAX_MODEL_LEN")
	minWorkers := fs.Int("min-workers", 0, "minimum workers (nonzero bills around the clock)")
	maxWorkers := fs.Int("max-workers", 3, "maximum workers")
	idle := fs.Int("idle-timeout", 5, "seconds before an idle worker scales down")
	scalerValue := fs.Int("scaler-value", 1, "REQUEST_COUNT concurrency target (or QUEUE_DELAY seconds when --endpoint-type=QUEUE)")
	endpointType := fs.String("endpoint-type", runpod.EndpointTypeQueue, "QUEUE (default, matches worker-v1-vllm) or LOAD_BALANCER")
	flashboot := fs.String("flashboot", "FLASHBOOT", "OFF, FLASHBOOT, or PRIORITY_FLASHBOOT")
	image := fs.String("image", runpod.DefaultImage, "worker image")
	disk := fs.Int("disk", 0, "container disk GB (0 = sized from the repo)")
	name := fs.String("name", "", "endpoint name")
	quant := fs.String("quant", "", "QUANTIZATION override (awq, gptq, …)")
	noFamily := fs.Bool("no-family", false, "skip Qwen/Mistral/Llama env defaults")
	trust := fs.Bool("trust-remote-code", false, "set TRUST_REMOTE_CODE=true")
	force := fs.Bool("force", false, "deploy gated models without HF_TOKEN, or non-vLLM formats")
	smoke := fs.Bool("smoke", false, "run a short completion after create (bills a cold start)")
	asJSON := fs.Bool("json", false, "print JSON")
	samplingMode := fs.String("sampling", "recommended", "recommended, none, or customize (with --set)")
	var extra stringsFlag
	fs.Var(&extra, "env", "extra KEY=VALUE (repeatable)")
	var setFlag stringsFlag
	fs.Var(&setFlag, "set", "sampling KEY=VALUE (repeatable: temp, top_p, top_k, min_p, rep, max_tokens)")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	_ = provider // already handled via peek; kept for --help
	if fs.NArg() < 1 {
		return fmt.Errorf("usage: runhug deploy <org/model>")
	}
	modelID := fs.Arg(0)
	env := config.Load()
	if err := env.RequireRunpod(); err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	hfClient := hf.New(env.HFToken)
	model, err := hfClient.Get(ctx, modelID)
	if err != nil {
		return err
	}
	modelID = model.RepoID()
	format := hf.DetectFormat(*model)
	if format.Engine == hf.EngineGGUF && !*force {
		return fmt.Errorf("%s is GGUF; vLLM will not load it. Use a llama.cpp worker, or pass --force", modelID)
	}
	if model.IsGated() && env.HFToken == "" && !*force {
		return fmt.Errorf("%s is gated; set HF_TOKEN or pass --force (the worker will 403 on download)", modelID)
	}

	est := inspectEstimate(*model, format, *maxLen)
	rp := runpod.New(env.RunpodAPIKey)
	gpus, err := rp.ListGPUs(ctx)
	if err != nil {
		return err
	}
	preferGPU := applySavedRunpodGPU(*gpu)
	if preferGPU != "" && strings.TrimSpace(*gpu) == "" {
		fmt.Fprintf(os.Stderr, "%s using saved GPU preference %s\n", dim("note:"), preferGPU)
	}
	choice, err := runpod.Pick(gpus, est.RequiredGB, preferGPU, *gpuCount)
	if err != nil {
		return err
	}

	diskGB := *disk
	if diskGB <= 0 {
		diskGB = est.DiskGB
	}
	endpointName := *name
	if endpointName == "" {
		endpointName = slug(modelID)
	}

	workerEnv := map[string]string{
		"MODEL_NAME":                        modelID,
		"RAW_OPENAI_OUTPUT":                 "1",
		"GPU_MEMORY_UTILIZATION":            "0.90",
		"OPENAI_SERVED_MODEL_NAME_OVERRIDE": modelID,
		"MAX_MODEL_LEN":                     fmt.Sprintf("%d", *maxLen),
	}
	if env.HFToken != "" {
		workerEnv["HF_TOKEN"] = env.HFToken
	}
	if !*noFamily {
		// Match family from repo id plus Hub base_model / tags / arch so fine-tunes
		// like Qwythos (Qwen3.5 base, no "qwen" in the name) still get tool env.
		workerEnv = family.Apply(modelID, workerEnv, model.FamilyHints()...)
	}
	q := *quant
	if q == "" {
		q = format.Quant
	}
	if q != "" {
		workerEnv["QUANTIZATION"] = q
	}
	if *trust {
		workerEnv["TRUST_REMOTE_CODE"] = "true"
	}
	if choice.GPUCount > 1 {
		workerEnv["TENSOR_PARALLEL_SIZE"] = fmt.Sprintf("%d", choice.GPUCount)
	}
	userEnv, err := parseKV(extra)
	if err != nil {
		return err
	}
	for k, v := range userEnv {
		workerEnv[k] = v
	}

	epType := strings.ToUpper(strings.TrimSpace(*endpointType))
	if epType == "" {
		epType = runpod.EndpointTypeQueue
	}
	var scaling *runpod.Scaling
	switch epType {
	case runpod.EndpointTypeQueue:
		// QUEUE may use QUEUE_DELAY (seconds) or REQUEST_COUNT; default delay from --scaler-value.
		delay := float64(*scalerValue)
		if delay < 0.5 {
			delay = 4
		}
		scaling = runpod.QueueDelayScaling(delay)
	case runpod.EndpointTypeLoadBalancer:
		scaling = runpod.RequestCountScaling(*scalerValue)
	default:
		return fmt.Errorf("--endpoint-type must be LOAD_BALANCER or QUEUE, got %q", *endpointType)
	}

	req := runpod.CreateEndpointRequest{
		Name:  endpointName,
		Type:  epType,
		Image: *image,
		Disk:  diskGB,
		Env:   workerEnv,
		GPU: runpod.GPUConfig{
			Pools: []string{choice.Pool.ID},
			Count: choice.GPUCount,
		},
		Workers: &runpod.Workers{
			Min:         *minWorkers,
			Max:         *maxWorkers,
			IdleTimeout: *idle,
		},
		Scaling:   scaling,
		Timeout:   600000,
		Flashboot: strings.ToUpper(*flashboot),
	}

	printPlan(modelID, format, est, choice, req, env.HFToken != "", *estimate, *dry)

	rec := loadRecommendSampling(ctx, hfClient, *model, format)
	chosen, err := chooseSampling(rec, *samplingMode, setFlag, *yes, *dry, *asJSON)
	if err != nil {
		return err
	}

	if *dry {
		if *asJSON {
			return writeJSON(map[string]any{"plan": req, "choice": choice, "estimate": est, "sampling": chosen})
		}
		fmt.Println(dim("dry-run: nothing created"))
		return nil
	}
	if !*yes {
		if !confirm(fmt.Sprintf("Create this endpoint? Serverless GPU ~$%.2f/hr while a worker is up", choice.HourlyUSD)) {
			return fmt.Errorf("aborted")
		}
	}

	created, err := rp.CreateEndpoint(ctx, req)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "409") || strings.Contains(strings.ToLower(err.Error()), "collision") {
			req.Name = endpointName + "-" + time.Now().UTC().Format("150405")
			created, err = rp.CreateEndpoint(ctx, req)
		}
		if err != nil {
			return err
		}
	}

	reg, _, err := store.Load()
	if err != nil {
		return err
	}
	reg.Put(store.Model{
		HFRepo:       modelID,
		EndpointID:   created.ID,
		EndpointType: req.Type,
		GPUPool:      choice.Pool.ID,
		GPUCount:     choice.GPUCount,
		Image:        req.Image,
		HourlyUSD:    choice.HourlyUSD,
		Sampling:     chosen,
		CreatedAt:    time.Now().UTC(),
	})
	reg.Current = modelID
	if err := reg.Save(); err != nil {
		fmt.Fprintf(os.Stderr, "%s saved endpoint %s but registry write failed: %v\n", yellow("warning:"), created.ID, err)
	}

	openai := runpod.OpenAIURLFor(req.Type, created.ID)
	result := map[string]any{
		"endpoint_id": created.ID,
		"name":        created.Name,
		"model":       modelID,
		"gpu_pool":    choice.Pool.ID,
		"gpu_count":   choice.GPUCount,
		"hourly_usd":  choice.HourlyUSD,
		"openai_url":  openai,
	}

	if *smoke {
		fmt.Fprintln(os.Stderr, "smoke: short completion (first job is a multi-minute cold start)…")
		text, err := jobs.Chat(env.RunpodAPIKey, created.ID, "Say hello in one short sentence.", 32)
		if err != nil {
			result["smoke_error"] = err.Error()
			fmt.Fprintf(os.Stderr, "smoke failed: %v\n", err)
		} else {
			result["smoke"] = text
			fmt.Fprintf(os.Stderr, "smoke: %s\n", text)
		}
	}

	if *asJSON {
		return writeJSON(result)
	}
	fmt.Fprintln(os.Stdout)
	fmt.Fprintf(os.Stdout, "%s  %s  (%s)\n", green("Created"), cyan(created.ID), created.Name)
	printKV(os.Stdout, "model", bold(modelID))
	printKV(os.Stdout, "hub", cyan(hubLink(modelID)))
	printKV(os.Stdout, "openai", cyan(openai))
	printKV(os.Stdout, "billing", fmt.Sprintf("scale-to-zero (min workers %d). A running worker is ~$%.2f/hr on %s.", *minWorkers, choice.HourlyUSD, choice.Pool.ID))
	fmt.Fprintln(os.Stdout)
	commands(os.Stdout, "Next:",
		"runhug proxy",
		"curl http://127.0.0.1:8080/v1/models",
		"runhug delete "+modelID,
	)
	return nil
}

func inspectEstimate(m hf.Model, format hf.Format, maxLen int) sizing.Estimate {
	return sizing.EstimateModel(m, format, maxLen)
}

func inspectGPU(ctx context.Context, apiKey string, required float64, pool string) (string, any, error) {
	gpus, err := runpod.New(apiKey).ListGPUs(ctx)
	if err != nil {
		return "", nil, err
	}
	c, err := runpod.Pick(gpus, required, pool, 0)
	if err != nil {
		return "", nil, err
	}
	text := fmt.Sprintf("%s (%s, %.0f GB)  $%.2f/hr serverless  stock %s  ×%d GPU",
		c.Pool.ID, c.Pool.ExampleGPU, c.Pool.MemoryGB, c.HourlyUSD, c.Pool.Availability, c.GPUCount)
	if c.Next != nil {
		text += fmt.Sprintf("  | next %s $%.2f/hr", c.Next.ID, c.Next.PricePerHour)
	}
	return text, c, nil
}

func printPlan(modelID string, format hf.Format, est sizing.Estimate, c runpod.Choice, req runpod.CreateEndpointRequest, hasHF bool, showEstimate, dryRun bool) {
	heading(os.Stdout, "Plan")
	printKV(os.Stdout, "model", bold(modelID))
	fmt.Fprintf(os.Stdout, "  %s  %s", dim(padRight("format", 9)), format.Engine)
	if format.Quant != "" {
		fmt.Fprintf(os.Stdout, " (%s)", format.Quant)
	}
	fmt.Println()
	printKV(os.Stdout, "params", fmt.Sprintf("%s (%s)", est.ParamsLabel(), dash(est.ParamsSource)))
	if est.WeightGB > 0 {
		printKV(os.Stdout, "vram", fmt.Sprintf("%.1f GB weights → %.1f GB with overhead", est.WeightGB, est.RequiredGB))
	}
	printKV(os.Stdout, "gpu", fmt.Sprintf("%s ×%d  %s  $%.2f/hr  stock %s",
		c.Pool.ID, c.GPUCount, c.Pool.ExampleGPU, c.HourlyUSD, c.Pool.Availability))
	printKV(os.Stdout, "why", c.Reason)
	printKV(os.Stdout, "image", req.Image)
	printKV(os.Stdout, "disk", fmt.Sprintf("%d GB", req.Disk))
	printKV(os.Stdout, "type", req.Type)
	wMin, wMax := 0, 3
	idleSec := 5
	if req.Workers != nil {
		wMin, wMax = req.Workers.Min, req.Workers.Max
		if req.Workers.IdleTimeout > 0 {
			idleSec = req.Workers.IdleTimeout
		}
	}
	printKV(os.Stdout, "workers", fmt.Sprintf("min=%d max=%d flashboot=%s", wMin, wMax, req.Flashboot))
	scalingDesc := "n/a"
	if req.Scaling != nil {
		switch {
		case req.Scaling.RequestCount != nil:
			scalingDesc = fmt.Sprintf("%s requestCount=%d", req.Scaling.Type, *req.Scaling.RequestCount)
		case req.Scaling.QueueDelay != nil:
			scalingDesc = fmt.Sprintf("%s queueDelay=%.1f", req.Scaling.Type, *req.Scaling.QueueDelay)
		default:
			scalingDesc = req.Scaling.Type
		}
	}
	printKV(os.Stdout, "scaling", scalingDesc)
	printKV(os.Stdout, "idle", fmt.Sprintf("%ds before scale-down (workers.idleTimeout)", idleSec))
	keys := make([]string, 0, len(req.Env))
	for k := range req.Env {
		if k == "HF_TOKEN" {
			continue
		}
		keys = append(keys, k+"="+req.Env[k])
	}
	printKV(os.Stdout, "env", strings.Join(keys, "  "))
	if hasHF {
		printKV(os.Stdout, "hf_token", "set (not printed)")
	}
	fmt.Fprintln(os.Stdout)
	if showEstimate {
		fb := strings.EqualFold(req.Flashboot, "FLASHBOOT") || strings.EqualFold(req.Flashboot, "PRIORITY_FLASHBOOT")
		cost := sizing.EstimateServerlessCost(c.HourlyUSD, est.WeightGB, c.GPUCount, idleSec, fb)
		fmt.Fprintln(os.Stdout, bold(cost.FormatBlock(c.Pool.ID)))
	} else if dryRun {
		fmt.Fprintln(os.Stdout, dim("Tip: pass --estimate / -e for cold/warm/daily cost scenarios."))
	}
}

var slugRe = regexp.MustCompile(`[^a-z0-9-]+`)

func slug(modelID string) string {
	s := strings.ToLower(modelID)
	s = strings.ReplaceAll(s, "/", "-")
	s = slugRe.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if s == "" {
		s = "vllm-model"
	}
	s = "vllm-" + s
	if len(s) > 80 {
		s = s[:80]
	}
	return s
}

// peekProvider returns --provider value when present.
func peekProvider(args []string) (string, bool) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--provider" || a == "-provider":
			if i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				return args[i+1], true
			}
			return "", true
		case strings.HasPrefix(a, "--provider="):
			return strings.TrimPrefix(a, "--provider="), true
		case strings.HasPrefix(a, "-provider="):
			return strings.TrimPrefix(a, "-provider="), true
		}
	}
	return "", false
}

// stripProviderFlag removes --provider / --provider=X so nested gcp deploy can re-parse.
func stripProviderFlag(args []string) []string {
	out := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--provider" || a == "-provider":
			if i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				i++
			}
			continue
		case strings.HasPrefix(a, "--provider=") || strings.HasPrefix(a, "-provider="):
			continue
		default:
			out = append(out, a)
		}
	}
	return out
}
