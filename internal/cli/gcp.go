package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/adamsiwiec1/runhug/internal/config"
	"github.com/adamsiwiec1/runhug/internal/gcp"
	"github.com/adamsiwiec1/runhug/internal/hf"
	"github.com/adamsiwiec1/runhug/internal/hparams"
	"github.com/adamsiwiec1/runhug/internal/store"
)

func cmdGCP(args []string) error {
	if len(args) == 0 {
		printGCPHelp(os.Stdout)
		return nil
	}
	switch args[0] {
	case "deploy", "run", "launch":
		return cmdGCPDeploy(args[1:])
	case "tunnel", "iap":
		return cmdGCPTunnel(args[1:])
	case "stop":
		return cmdGCPStop(args[1:])
	case "delete", "rm":
		return cmdGCPDelete(args[1:])
	case "status":
		return cmdGCPStatus(args[1:])
	case "dockerfile", "image":
		return cmdGCPDockerfile(args[1:])
	case "push":
		return cmdGCPPush(args[1:])
	case "opencode":
		return cmdGCPOpenCode(args[1:])
	default:
		if showCmdHelp(args, "runhug gcp", printGCPHelp, printGCPHelpFull) {
			return nil
		}
		printGCPHelp(os.Stderr)
		return fmt.Errorf("unknown gcp command %q\nRun `runhug gcp --help` or `runhug gcp --help-full`", args[0])
	}
}

func printGCPHelp(w io.Writer) {
	helpUsage(w, "runhug gcp <command>")
	fmt.Fprintln(w, dim("Spot L4/T4 · llama.cpp · stop-on-idle · SSH tunnel → 127.0.0.1:18080/v1"))
	fmt.Fprintln(w)

	helpSection(w, "commands")
	helpCmd(w, "deploy [model]", "create Spot VM (pushes image if missing)")
	helpCmd(w, "tunnel [name]", "SSH local-forward to /v1")
	helpCmd(w, "status|stop|delete", "lifecycle (defaults to last deploy)")
	helpCmd(w, "dockerfile", "view Dockerfile + entrypoint (pager)")
	helpCmd(w, "push", "local docker build + push to AR")
	helpCmd(w, "opencode", "merge .opencode/opencode.json")
	fmt.Fprintln(w)

	helpSection(w, "deploy flags")
	helpFlag(w, "--project --zone --gpu", "GCP project / zone / L4|T4")
	helpFlag(w, "--image <ref>", "AR/GCR tag (default: project llama-server)")
	helpFlag(w, "--idle-timeout --keep-up", "stop-on-idle (default 600s)")
	helpFlag(w, "--estimate -e", "full Spot cost block")
	helpFlag(w, "--verbose -v", "assumptions + extra detail")
	helpFlag(w, "--yes --dry-run --json", "confirm / plan-only / JSON")
	fmt.Fprintln(w)

	fmt.Fprintf(w, "%s %s\n", dim("also:"), cyan("runhug deploy --provider gcp <model>"))
}

// gcpHelpText is kept for tests that assert on the rendered help string.
func gcpHelpText() string {
	var b strings.Builder
	printGCPHelp(&b)
	return b.String()
}

func cmdGCPDeploy(args []string) error {
	fs := newFlagSet("gcp deploy")
	_ = fs.String("provider", "gcp", "ignored (gcp subcommand)")
	yes := fs.Bool("yes", false, "create without a prompt")
	dry := fs.Bool("dry-run", false, "print the plan only")
	asJSON := fs.Bool("json", false, "print JSON")
	project := fs.String("project", "", "GCP project id (no hardcoded default)")
	zone := fs.String("zone", "", "GCE zone")
	region := fs.String("region", "", "GCE region")
	gpu := fs.String("gpu", "", "L4 (default) or T4; default: gpu preference if gcp")
	idle := fs.Int("idle-timeout", 600, "seconds before stop-on-idle")
	keepUpFlag := fs.Bool("keep-up", false, "disable stop-on-idle (bills until gcp stop)")
	name := fs.String("name", "", "instance name")
	gguf := fs.String("gguf", "", "preferred GGUF filename")
	disk := fs.Int("disk", 0, "boot disk GB")
	writeImage := fs.String("write-image", "", "write Dockerfile+entrypoint to dir")
	doOpenCode := fs.Bool("opencode", false, "merge project .opencode/opencode.json (OPENAI_API_KEY env ref)")
	openCodeDir := fs.String("opencode-dir", ".", "project dir for .opencode/opencode.json")
	noFallback := fs.Bool("no-fallback", false, "do not fall back from L4 to T4")
	containerImage := fs.String("image", "", "prebuilt container image (Artifact Registry / GCR)")
	publicIP := fs.Bool("public-ip", false, "dogfood: ephemeral external IP (default no-address + Cloud NAT)")
	estimate := fs.Bool("estimate", false, "print full cost block")
	fs.BoolVar(estimate, "e", false, "alias for --estimate")
	verbose := fs.Bool("verbose", false, "include assumptions, gcloud argv, Dockerfile dump")
	fs.BoolVar(verbose, "v", false, "alias for --verbose")
	embeddings := fs.Bool("embeddings", false, "enable llama-server --embeddings (POST /v1/embeddings)")
	samplingMode := fs.String("sampling", "recommended", "recommended, none, or customize (with --set)")
	var setFlag stringsFlag
	fs.Var(&setFlag, "set", "sampling KEY=VALUE (repeatable)")
	if err := parseFlags(fs, args); err != nil {
		return err
	}

	modelArg := ""
	if fs.NArg() >= 1 {
		modelArg = fs.Arg(0)
	}

	client := gcp.NewClient()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	if err := client.RequireADC(ctx); err != nil && !*dry {
		return err
	}
	if *dry {
		// dry-run may still proceed without ADC so Dockerfile/plan can print
		_ = gcp.RequireGCloud()
	}

	proj := strings.TrimSpace(*project)
	if proj == "" {
		var err error
		proj, err = resolveGCPProject(ctx, client, !*dry && promptOK())
		if err != nil {
			return err
		}
	}
	if err := gcp.EnsureProject(proj); err != nil {
		return err
	}

	modelID := strings.TrimSpace(modelArg)
	ggufFile := strings.TrimSpace(*gguf)
	env := config.Load()

	if modelID == "" {
		if !*dry && promptOK() {
			line, err := readLine("HF GGUF model (org/name): ")
			if err != nil {
				return err
			}
			modelID = strings.TrimSpace(line)
		}
	}
	if modelID == "" {
		return fmt.Errorf("usage: runhug gcp deploy <org/model> — model is required (never hardcoded)")
	}
	resolved, err := resolveModelArg(modelID)
	if err != nil {
		return err
	}
	modelID = resolved

	// Resolve Hub card when possible (prefer GGUF).
	hctx, hcancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer hcancel()
	var weightGB float64
	var format hf.Format
	hfClient := hf.New(env.HFToken)
	model, err := hfClient.Get(hctx, modelID)
	if err != nil {
		if !*dry {
			return fmt.Errorf("huggingface model: %w", err)
		}
		fmt.Fprintf(os.Stderr, "%s could not fetch Hub card (%v); continuing dry-run with %s\n", yellow("warning:"), err, modelID)
	} else {
		modelID = model.RepoID()
		format = hf.DetectFormat(*model)
		if format.Engine != hf.EngineGGUF && format.Engine != hf.EngineUnknown && ggufFile == "" {
			return redirectNonGGUFToRunpod(modelID, format.Engine, *yes, *dry, *asJSON, *estimate, *verbose)
		}
		if ggufFile == "" {
			if file, q, ok := hf.PickGGUF(*model); ok {
				ggufFile = file
				if format.Quant == "" {
					format.Quant = strings.ToLower(q)
				}
			}
		} else if format.Quant == "" {
			format.Quant = hf.QuantFromFilename(ggufFile)
		}
		if model.IsGated() && env.HFToken == "" {
			return fmt.Errorf("%s is gated; set HF_TOKEN (passed as instance metadata, not baked into the image)", modelID)
		}
		weightGB = gcpWeightGB(*model, ggufFile)
	}

	keepUp := *keepUpFlag
	preferGPU := applySavedGCPGPU(*gpu)
	if preferGPU != "" && strings.TrimSpace(*gpu) == "" {
		fmt.Fprintf(os.Stderr, "%s using saved GPU preference %s\n", dim("note:"), preferGPU)
	}
	if !*dry && !*yes && promptOK() {
		_ = client.RefreshSpotQuotes(ctx)
		tgt, _ := gcp.PickTarget(preferGPU)
		rate := gcp.SpotHourlyUSD(tgt)
		warn := fmt.Sprintf("Keep Spot VM up with NO stop-on-idle? Bills ~%s until you run runhug gcp stop (GPU SKU; vCPU/RAM extra)", gcp.FormatKeepUp(rate))
		if *keepUpFlag {
			// Explicit --keep-up still requires an interactive billing ack unless --yes.
			if !confirmPref(warn+"?", false) {
				return fmt.Errorf("aborted: keep-up not confirmed (pass --yes to skip)")
			}
			keepUp = true
		} else if confirmPref(warn+"?", false) {
			keepUp = true
		}
	}

	bearer, err := gcp.GenerateBearer()
	if err != nil {
		return err
	}

	imgRef := strings.TrimSpace(*containerImage)
	if imgRef == "" {
		imgRef = strings.TrimSpace(os.Getenv("RUNHUG_GCP_IMAGE"))
	}
	if imgRef == "" {
		reg, _ := gcp.ZoneFromRegion(*region, *zone)
		imgRef = gcp.DefaultLlamaImage(proj, reg)
		if imgRef != "" {
			fmt.Fprintf(os.Stderr, "%s image %s\n", dim("note:"), imgRef)
		}
	}
	if imgRef == "" && !*dry {
		return fmt.Errorf("could not infer Artifact Registry image (pass --project)")
	}

	needPush := false
	if !*dry && gcp.CanAutoPush(imgRef) {
		exists, _ := client.ArtifactImageExists(ctx, imgRef)
		needPush = !exists
		if needPush {
			fmt.Fprintf(os.Stderr, "%s %s not in Artifact Registry yet — will docker build+push on confirm\n", dim("note:"), imgRef)
		}
	}

	usePublic := *publicIP
	if !usePublic {
		reg, _ := gcp.ZoneFromRegion(*region, *zone)
		if has, err := client.HasCloudNAT(ctx, proj, reg); err == nil && !has {
			usePublic = true
			fmt.Fprintf(os.Stderr, "%s no Cloud NAT in %s — ephemeral external IP so docker and the GGUF can download\n", dim("note:"), reg)
		}
	}

	plan, err := gcp.BuildPlan(gcp.DeployRequest{
		Project:        proj,
		Zone:           *zone,
		Region:         *region,
		Name:           *name,
		ModelID:        modelID,
		GGUFFile:       ggufFile,
		GPU:            preferGPU,
		IdleSeconds:    *idle,
		KeepUp:         keepUp,
		DiskGB:         *disk,
		Bearer:         bearer,
		HFToken:        env.HFToken,
		DryRun:         *dry,
		ContainerImage: imgRef,
		PublicIP:       usePublic,
		WeightGB:       weightGB,
		Embeddings:     *embeddings,
	})
	if err != nil {
		return err
	}

	var chosen *hparams.Sampling
	if model != nil {
		rec := loadRecommendSampling(hctx, hfClient, *model, format)
		chosen, err = chooseSampling(rec, *samplingMode, setFlag, *yes, *dry, *asJSON, *verbose)
		if err != nil {
			return err
		}
		plan.Image = plan.Image.WithSampling(chosen)
		plan.Image.Embeddings = *embeddings
		plan.Entrypoint = gcp.EntrypointScript(plan.Image)
		plan.Startup = gcp.StartupScript(plan.Image)
	}

	if dir := strings.TrimSpace(*writeImage); dir != "" {
		if err := gcp.WriteImageFiles(dir, plan.Image); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "wrote Dockerfile + entrypoint.sh → %s\n", dir)
	}

	printGCPPlan(plan, env.HFToken != "", *estimate || *dry || !*yes, *verbose)

	if *dry {
		if *asJSON {
			return writeJSON(map[string]any{
				"plan":       plan,
				"dockerfile": plan.Dockerfile,
				"entrypoint": plan.Entrypoint,
				"opencode":   gcp.OpenCodeConfig(gcp.OpenCodeSpec{BaseURL: plan.OpenAIHint, ModelID: modelID, Source: "gcp"}),
			})
		}
		if *verbose {
			spec := gcp.OpenCodeSpec{
				BaseURL: gcp.LocalOpenAIURL(gcp.LocalTunnelPort),
				ModelID: modelID,
				Source:  "gcp ssh tunnel",
			}
			cfg := gcp.OpenCodeConfig(spec)
			raw, _ := json.MarshalIndent(cfg, "", "  ")
			fmt.Fprintln(os.Stdout)
			heading(os.Stdout, "OpenCode (dry-run stdout, apiKey env ref only)")
			fmt.Println(string(raw))
			fmt.Fprintln(os.Stdout)
			body := plan.Dockerfile + "\n---\n" + plan.Entrypoint
			if err := pageText(body); err != nil {
				return err
			}
		} else {
			fmt.Fprintln(os.Stdout, dim("Image files: runhug gcp dockerfile --model "+modelID))
		}
		fmt.Println(green("✓") + " " + dim("dry-run: nothing created"))
		return nil
	}

	if !*yes {
		msg := fmt.Sprintf("Create Spot %s VM %s in %s/%s (~%s)?",
			plan.Target.Name, plan.Name, plan.Project, plan.Zone, gcp.FormatKeepUp(plan.Cost.HourlyUSD))
		if needPush {
			msg = fmt.Sprintf("Build+push %s and create Spot %s VM %s in %s/%s (~%s)?",
				imgRef, plan.Target.Name, plan.Name, plan.Project, plan.Zone, gcp.FormatKeepUp(plan.Cost.HourlyUSD))
		}
		if !confirmPref(msg, true) {
			return fmt.Errorf("aborted")
		}
	}

	if needPush {
		if err := ensureLlamaImage(client, imgRef, plan.Image, *verbose); err != nil {
			return err
		}
	}

	if err := client.EnsureIAPFirewall(ctx, plan.Project); err != nil {
		fmt.Fprintf(os.Stderr, "%s IAP firewall: %v (continuing)\n", yellow("warning:"), err)
	}

	createCtx, createCancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer createCancel()
	if err := client.Deploy(createCtx, plan, bearer, env.HFToken, !*noFallback); err != nil {
		return err
	}
	if err := gcp.SaveBearer(plan.Name, bearer); err != nil {
		fmt.Fprintf(os.Stderr, "%s saved VM but could not store bearer locally: %v\n", yellow("warning:"), err)
	}

	reg, _, err := store.Load()
	if err != nil {
		return err
	}
	reg.Put(store.Model{
		HFRepo:    modelID,
		Backend:   store.BackendGCP,
		PodID:     plan.Name,
		PodCloud:  "gcp",
		GPUPool:   plan.Target.Name,
		GPUCount:  1,
		BaseURL:   gcp.LocalOpenAIURL(gcp.LocalTunnelPort),
		ServeName: modelID,
		Image:     plan.ContainerImage,
		HourlyUSD: plan.Cost.HourlyUSD,
		Sampling:  chosen,
		CreatedAt: time.Now().UTC(),
		// Zone/project stashed in EndpointID / EndpointType for Phase 1 reuse.
		EndpointID:   plan.Project,
		EndpointType: plan.Zone,
	})
	reg.Current = modelID
	if err := reg.Save(); err != nil {
		fmt.Fprintf(os.Stderr, "%s registry write failed: %v\n", yellow("warning:"), err)
	}

	if *doOpenCode {
		path, err := gcp.WriteProjectOpenCode(*openCodeDir, gcp.OpenCodeSpec{
			BaseURL: gcp.LocalOpenAIURL(gcp.LocalTunnelPort),
			ModelID: modelID,
			Source:  "gcp ssh tunnel",
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s opencode: %v\n", yellow("warning:"), err)
		} else {
			printKV(os.Stdout, "opencode", path+` (apiKey={env:OPENAI_API_KEY})`)
		}
	}

	if *asJSON {
		return writeJSON(map[string]any{
			"provider":   "gcp",
			"project":    plan.Project,
			"zone":       plan.Zone,
			"instance":   plan.Name,
			"model":      modelID,
			"gpu":        plan.Target.Name,
			"openai":     plan.OpenAIHint,
			"tunnel":     plan.TunnelHint,
			"idle_sec":   plan.IdleSeconds,
			"keep_up":    plan.KeepUp,
			"hourly_usd": plan.Cost.HourlyUSD,
			"bearer_set": true,
		})
	}

	fmt.Fprintln(os.Stdout)
	fmt.Fprintf(os.Stdout, "%s  %s  (%s Spot ~%s)\n", green("Created"), cyan(plan.Name), plan.Target.Name, gcp.FormatKeepUp(plan.Cost.HourlyUSD))
	printKV(os.Stdout, "project", plan.Project)
	printKV(os.Stdout, "zone", plan.Zone)
	printKV(os.Stdout, "model", bold(modelID))
	printKV(os.Stdout, "openai", cyan(gcp.LocalOpenAIURL(gcp.LocalTunnelPort)))
	if plan.KeepUp {
		printKV(os.Stdout, "idle", yellow("keep-up — NO stop-on-idle; runhug gcp stop when done"))
	} else {
		printKV(os.Stdout, "idle", fmt.Sprintf("%ds stop-on-idle", plan.IdleSeconds))
	}
	fmt.Fprintln(os.Stdout)
	commands(os.Stdout, "Next:",
		"runhug run",
		"runhug gcp stop",
	)
	return nil
}

func gcpWeightGB(m hf.Model, _ string) float64 {
	est := inspectEstimate(m, hf.DetectFormat(m), 8192)
	return est.WeightGB
}

func redirectNonGGUFToRunpod(modelID string, engine hf.Engine, yes, dry, asJSON, estimate, verbose bool) error {
	fmt.Fprintf(os.Stderr, "%s %s is %s (GCP llama.cpp needs GGUF)\n", dim("note:"), modelID, engine)
	if !dry && !yes {
		if !promptOK() {
			return fmt.Errorf("%s is not GGUF — pass --yes to deploy on RunPod", modelID)
		}
		if !confirmPref(fmt.Sprintf("Deploy %s on RunPod serverless instead?", modelID), true) {
			return fmt.Errorf("aborted")
		}
	}
	fmt.Fprintf(os.Stderr, "%s switching to RunPod (vLLM)…\n", dim("note:"))
	return cmdDeploy(runpodArgsFromGCP(modelID, yes, dry, asJSON, estimate, verbose))
}

func runpodArgsFromGCP(modelID string, yes, dry, asJSON, estimate, verbose bool) []string {
	args := []string{modelID}
	if yes {
		args = append(args, "--yes")
	}
	if dry {
		args = append(args, "--dry-run")
	}
	if asJSON {
		args = append(args, "--json")
	}
	if estimate {
		args = append(args, "--estimate")
	}
	if verbose {
		args = append(args, "--verbose")
	}
	return args
}

func ensureLlamaImage(client *gcp.Client, imgRef string, cfg gcp.ImageConfig, verbose bool) error {
	parent, cancel := context.WithTimeout(context.Background(), 45*time.Minute)
	defer cancel()
	ctx, cancelJob := context.WithCancel(parent)
	defer cancelJob()
	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc, os.Interrupt)
	defer signal.Stop(sigc)
	go func() {
		select {
		case <-sigc:
			cancelJob()
		case <-ctx.Done():
		}
	}()

	printEnsureStep(os.Stderr, 1, 4, "registry", "create Artifact Registry repo if missing")
	if err := client.EnsureDockerRepo(ctx, imgRef); err != nil {
		return err
	}
	printEnsureStep(os.Stderr, 2, 4, "auth", "gcloud auth configure-docker")
	if err := client.ConfigureDockerAuth(ctx, imgRef); err != nil {
		return err
	}

	quiet := promptOK() && !verbose
	var out io.Writer
	if quiet {
		f, err := os.CreateTemp("", "runhug-docker-*.log")
		if err != nil {
			return err
		}
		defer func() { _ = f.Close(); _ = os.Remove(f.Name()) }()
		log := &quietLog{file: f}
		out = log
		restore := startJobKeys(ctx, cancelJob, log)
		defer restore()
	}

	started := time.Now()
	curN, curLabel := 3, "build"
	var stepMu sync.Mutex
	printEnsureStep(os.Stderr, 3, 4, "build", "llama.cpp cuda image  "+jobKeysHint())
	stopTick := make(chan struct{})
	if quiet {
		go func() {
			t := time.NewTicker(8 * time.Second)
			defer t.Stop()
			for {
				select {
				case <-stopTick:
					return
				case <-ctx.Done():
					return
				case <-t.C:
					stepMu.Lock()
					n, lab := curN, curLabel
					stepMu.Unlock()
					printJobElapsed(started, n, 4, lab, "working")
				}
			}
		}()
		defer close(stopTick)
	}

	res, err := gcp.Push(ctx, gcp.PushRequest{
		Image:  imgRef,
		Config: cfg,
		Out:    out,
		Quiet:  quiet,
		OnStep: func(name string) {
			if name != "push" {
				return
			}
			stepMu.Lock()
			curN, curLabel = 4, "push"
			stepMu.Unlock()
			printEnsureStep(os.Stderr, 4, 4, "push", imgRef+"  "+jobKeysHint())
		},
	})
	if err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("cancelled")
		}
		return err
	}
	printEnsureStep(os.Stderr, 4, 4, "ready", green("pushed "+res.Image))
	return nil
}

func resolveGCPProject(ctx context.Context, client *gcp.Client, interactive bool) (string, error) {
	if cur := client.CurrentProject(ctx); cur != "" {
		if !interactive {
			return cur, nil
		}
		ok := confirmPref(fmt.Sprintf("Use gcloud project %s?", cur), true)
		if ok {
			return cur, nil
		}
	}
	if !interactive {
		return "", fmt.Errorf("pass --project (or set gcloud config project)")
	}
	projects, err := client.ListProjects(ctx)
	if err != nil {
		return "", fmt.Errorf("list projects: %w (pass --project)", err)
	}
	if len(projects) == 0 {
		return "", fmt.Errorf("no GCP projects visible — pass --project")
	}
	fmt.Fprintln(os.Stderr, bold("GCP projects"))
	max := len(projects)
	if max > 20 {
		max = 20
	}
	for i := 0; i < max; i++ {
		p := projects[i]
		label := p.ProjectID
		if p.Name != "" && p.Name != p.ProjectID {
			label = fmt.Sprintf("%s (%s)", p.ProjectID, p.Name)
		}
		fmt.Fprintf(os.Stderr, "  %2d  %s\n", i+1, label)
	}
	line, err := readLine(fmt.Sprintf("Pick project [1-%d] or type project id: ", max))
	if err != nil {
		return "", err
	}
	line = strings.TrimSpace(line)
	if n, err := strconv.Atoi(line); err == nil && n >= 1 && n <= max {
		return projects[n-1].ProjectID, nil
	}
	if line == "" {
		return "", fmt.Errorf("project required")
	}
	return line, nil
}

func printGCPPlan(plan *gcp.DeployPlan, hasHF bool, showCost, verbose bool) {
	planHeading(os.Stdout, "Plan (GCP)")
	printKV(os.Stdout, "provider", cyan("gcp"))
	printKV(os.Stdout, "project", bold(plan.Project))
	printKV(os.Stdout, "zone", plan.Zone)
	printKV(os.Stdout, "instance", cyan(plan.Name))
	printKV(os.Stdout, "model", bold(plan.ModelID))
	if plan.GGUFFile != "" {
		printKV(os.Stdout, "gguf", cyan(plan.GGUFFile))
	}
	gpuLine := fmt.Sprintf("%s (%s)", cyan(plan.Target.Name), plan.Target.MachineType)
	if verbose && plan.Target.Reason != "" {
		gpuLine = fmt.Sprintf("%s — %s", gpuLine, dim(plan.Target.Reason))
	}
	printKV(os.Stdout, "gpu", gpuLine)
	printKV(os.Stdout, "cost", highlightUSD(plan.Cost.CompactLine()))
	if plan.KeepUp {
		printKV(os.Stdout, "idle", yellow("keep-up — NO stop-on-idle"))
	} else {
		printKV(os.Stdout, "idle", yellow(fmt.Sprintf("%ds stop-on-idle", plan.IdleSeconds)))
	}
	printKV(os.Stdout, "bind", "SSH tunnel → "+gcp.LocalOpenAIURL(gcp.LocalTunnelPort))
	if verbose {
		printKV(os.Stdout, "container", dim(plan.ContainerImage))
		net := "no-address (need Cloud NAT)"
		if plan.PublicIP {
			net = yellow("ephemeral external IP (dogfood)")
		}
		printKV(os.Stdout, "network", net)
		printKV(os.Stdout, "auth", dim("gcloud ADC + CLI Bearer (not in image)"))
	}
	if hasHF {
		printKV(os.Stdout, "hf_token", green("set (metadata-from-file, not printed)"))
	}
	if verbose {
		printKV(os.Stdout, "gcloud", dim("gcloud "+strings.Join(plan.CreateArgs, " ")))
	}
	if showCost {
		fmt.Fprintln(os.Stdout)
		printCostBlock(os.Stdout, plan.Cost.FormatBlock(verbose))
	} else if verbose {
		fmt.Fprintln(os.Stdout, dim("Tip: pass --estimate / -e for the full Spot cost block."))
	}
	fmt.Fprintln(os.Stdout)
}

func cmdGCPTunnel(args []string) error {
	fs := newFlagSet("gcp tunnel")
	project := fs.String("project", "", "GCP project")
	zone := fs.String("zone", "", "GCE zone")
	local := fs.Int("local-port", gcp.LocalTunnelPort, "local listen port")
	remote := fs.Int("port", gcp.ServerPort, "remote llama-server port")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	name, proj, z, err := resolveGCPTarget(fs.Arg(0), *project, *zone)
	if err != nil {
		return err
	}
	client := gcp.NewClient()
	ctx := context.Background()
	if err := client.RequireADC(ctx); err != nil {
		return err
	}
	if _, err := gcp.LoadBearer(name); err == nil {
		fmt.Fprintf(os.Stderr, "%s tunnel %s → %s  then %s\n", dim("→"), name, gcp.LocalOpenAIURL(*local), cyan("runhug run"))
	} else {
		fmt.Fprintf(os.Stderr, "%s no stored bearer for %s — %s still works after connect\n", yellow("warning:"), name, cyan("runhug run"))
	}
	return client.StartTunnel(ctx, gcp.TunnelOpts{
		Project:    proj,
		Zone:       z,
		Instance:   name,
		RemotePort: *remote,
		LocalPort:  *local,
	})
}

func cmdGCPStop(args []string) error {
	fs := newFlagSet("gcp stop")
	project := fs.String("project", "", "GCP project")
	zone := fs.String("zone", "", "GCE zone")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	name, proj, z, err := resolveGCPTarget(fs.Arg(0), *project, *zone)
	if err != nil {
		return err
	}
	client := gcp.NewClient()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := client.RequireADC(ctx); err != nil {
		return err
	}
	return client.StopInstance(ctx, proj, z, name)
}

func cmdGCPDelete(args []string) error {
	fs := newFlagSet("gcp delete")
	project := fs.String("project", "", "GCP project")
	zone := fs.String("zone", "", "GCE zone")
	yes := fs.Bool("yes", false, "skip confirm")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	name, proj, z, err := resolveGCPTarget(fs.Arg(0), *project, *zone)
	if err != nil {
		return err
	}
	if !*yes && !confirm(fmt.Sprintf("Delete GCP instance %s?", name)) {
		return fmt.Errorf("aborted")
	}
	client := gcp.NewClient()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	if err := client.RequireADC(ctx); err != nil {
		return err
	}
	if err := client.DeleteInstance(ctx, proj, z, name); err != nil {
		return err
	}
	reg, _, err := store.Load()
	if err == nil {
		for id, m := range reg.Models {
			if m.Backend == store.BackendGCP && (m.PodID == name || id == name) {
				reg.Remove(id)
				_ = reg.Save()
				break
			}
		}
	}
	fmt.Fprintf(os.Stdout, "%s deleted %s\n", green("OK"), name)
	return nil
}

func cmdGCPStatus(args []string) error {
	fs := newFlagSet("gcp status")
	project := fs.String("project", "", "GCP project")
	zone := fs.String("zone", "", "GCE zone")
	asJSON := fs.Bool("json", false, "print JSON")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	name, proj, z, err := resolveGCPTarget(fs.Arg(0), *project, *zone)
	if err != nil {
		return err
	}
	client := gcp.NewClient()
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Minute)
	defer cancel()
	if err := client.RequireADC(ctx); err != nil {
		return err
	}
	st, err := client.DescribeInstance(ctx, proj, z, name)
	if err != nil {
		return err
	}
	if *asJSON {
		return writeJSON(st)
	}
	printKV(os.Stdout, "instance", st.Name)
	printKV(os.Stdout, "status", st.Status)
	printKV(os.Stdout, "zone", st.Zone)
	printKV(os.Stdout, "project", proj)
	return nil
}

func cmdGCPDockerfile(args []string) error {
	fs := newFlagSet("gcp dockerfile")
	model := fs.String("model", "", "optional MODEL_ID hint")
	gguf := fs.String("gguf", "", "optional GGUF filename")
	idle := fs.Int("idle-timeout", 600, "idle seconds")
	outDir := fs.String("out", "", "write files to directory instead of paging")
	forceStdout := fs.Bool("stdout", false, "print to stdout instead of man-style pager")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	if fs.NArg() >= 1 && *model == "" {
		*model = fs.Arg(0)
	}
	cfg := gcp.ImageConfig{ModelID: *model, GGUFFile: *gguf, IdleSeconds: *idle}
	if dir := strings.TrimSpace(*outDir); dir != "" {
		if err := gcp.WriteImageFiles(dir, cfg); err != nil {
			return err
		}
		fmt.Fprintf(os.Stdout, "wrote %s\n", filepath.Join(dir, "Dockerfile"))
		fmt.Fprintf(os.Stdout, "wrote %s\n", filepath.Join(dir, "entrypoint.sh"))
		return nil
	}
	body := gcp.Dockerfile(cfg) + "\n---\n" + gcp.EntrypointScript(cfg)
	if *forceStdout {
		fmt.Println(body)
		return nil
	}
	return pageText(body)
}

func cmdGCPPush(args []string) error {
	fs := newFlagSet("gcp push")
	image := fs.String("image", "", "destination image tag (Artifact Registry / GCR)")
	model := fs.String("model", "", "optional MODEL_ID hint")
	gguf := fs.String("gguf", "", "optional GGUF filename")
	idle := fs.Int("idle-timeout", 600, "idle seconds baked into image defaults")
	dir := fs.String("dir", "", "build context directory (default: temp)")
	platform := fs.String("platform", gcp.DefaultImagePlatform, "docker build --platform")
	dry := fs.Bool("dry-run", false, "print docker commands only")
	verbose := fs.Bool("verbose", false, "stream docker build/push output")
	fs.BoolVar(verbose, "v", false, "alias for --verbose")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	imgRef := strings.TrimSpace(*image)
	if imgRef == "" {
		imgRef = strings.TrimSpace(os.Getenv("RUNHUG_GCP_IMAGE"))
	}
	client := gcp.NewClient()
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Minute)
	defer cancel()
	if imgRef == "" {
		proj := client.CurrentProject(ctx)
		imgRef = gcp.DefaultLlamaImage(proj, gcp.DefaultRegion)
		if imgRef == "" {
			return fmt.Errorf("pass --image or set gcloud config project / RUNHUG_GCP_IMAGE")
		}
		fmt.Fprintf(os.Stderr, "%s image %s\n", dim("note:"), imgRef)
	}
	if fs.NArg() >= 1 && strings.TrimSpace(*model) == "" {
		*model = fs.Arg(0)
	}
	cfg := gcp.ImageConfig{
		ModelID:     strings.TrimSpace(*model),
		GGUFFile:    strings.TrimSpace(*gguf),
		IdleSeconds: *idle,
	}
	if *dry {
		res, err := gcp.Push(ctx, gcp.PushRequest{
			Image:    imgRef,
			Dir:      strings.TrimSpace(*dir),
			Platform: strings.TrimSpace(*platform),
			DryRun:   true,
			Config:   cfg,
		})
		if err != nil {
			return err
		}
		heading(os.Stdout, "gcp push (dry-run — local docker, no Cloud Build)")
		printKV(os.Stdout, "dir", res.Dir)
		printKV(os.Stdout, "image", res.Image)
		printKV(os.Stdout, "platform", res.Platform)
		fmt.Fprintln(os.Stdout)
		fmt.Fprintln(os.Stdout, strings.Join(res.BuildCmd, " "))
		fmt.Fprintln(os.Stdout, strings.Join(res.PushCmd, " "))
		return nil
	}
	return ensureLlamaImage(client, imgRef, cfg, *verbose)
}

func cmdGCPOpenCode(args []string) error {
	fs := newFlagSet("gcp opencode")
	dir := fs.String("dir", ".", "project directory")
	base := fs.String("base-url", gcp.LocalOpenAIURL(gcp.ServerPort), "OpenAI base URL")
	model := fs.String("model", "", "model id")
	dry := fs.Bool("dry-run", false, "print JSON to stdout only")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	if fs.NArg() >= 1 && *model == "" {
		*model = fs.Arg(0)
	}
	if strings.TrimSpace(*model) == "" {
		reg, _, err := store.Load()
		if err == nil {
			if m, ok := reg.Lookup(""); ok && m.Backend == store.BackendGCP {
				*model = m.HFRepo
			}
		}
	}
	if strings.TrimSpace(*model) == "" {
		return fmt.Errorf("pass a model id: runhug gcp opencode <org/model>")
	}
	spec := gcp.OpenCodeSpec{BaseURL: *base, ModelID: *model, Source: "gcp ssh tunnel"}
	cfg := gcp.OpenCodeConfig(spec)
	if *dry {
		raw, err := json.MarshalIndent(cfg, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(raw))
		return nil
	}
	path, err := gcp.WriteProjectOpenCode(*dir, spec)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stdout, "%s %s (no apiKey field)\n", green("wrote"), path)
	return nil
}

// resolveGCPTarget resolves instance/project/zone from args + registry.
func resolveGCPTarget(nameArg, projectFlag, zoneFlag string) (name, project, zone string, err error) {
	name = strings.TrimSpace(nameArg)
	project = strings.TrimSpace(projectFlag)
	zone = strings.TrimSpace(zoneFlag)

	reg, _, loadErr := store.Load()
	if loadErr == nil {
		if name == "" {
			if m, ok := reg.Lookup(""); ok && m.Backend == store.BackendGCP {
				name = m.PodID
				if project == "" {
					project = m.EndpointID
				}
				if zone == "" {
					zone = m.EndpointType
				}
			}
		} else if m, ok := reg.Lookup(name); ok && m.Backend == store.BackendGCP {
			if name == m.HFRepo {
				name = m.PodID
			}
			if project == "" {
				project = m.EndpointID
			}
			if zone == "" {
				zone = m.EndpointType
			}
		} else {
			// try pod id match
			for _, m := range reg.Models {
				if m.Backend == store.BackendGCP && m.PodID == name {
					if project == "" {
						project = m.EndpointID
					}
					if zone == "" {
						zone = m.EndpointType
					}
					break
				}
			}
		}
	}

	if name == "" {
		return "", "", "", fmt.Errorf("instance name required (or deploy first)")
	}
	if project == "" {
		if cur := gcp.NewClient().CurrentProject(context.Background()); cur != "" {
			project = cur
		}
	}
	if project == "" {
		return "", "", "", fmt.Errorf("pass --project")
	}
	if zone == "" {
		_, zone = gcp.ZoneFromRegion("", "")
	}
	return name, project, zone, nil
}
