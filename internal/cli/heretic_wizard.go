package cli

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/adamsiwiec1/runhug/internal/config"
)

func cmdHereticWizard(args []string) error {
	fs := newFlagSet("heretic wizard")
	yes := fs.Bool("yes", false, "print the guided checklist (non-interactive; never creates a live pod)")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	if *yes || !canPrompt() {
		return hereticWizardChecklist(os.Stdout)
	}
	return runHereticWizard()
}

func hereticWizardChecklist(w io.Writer) error {
	heading(w, "Heretic wizard — guided abliteration (checklist)")
	fmt.Fprintln(w, dim("Non-interactive / --yes: follow these steps yourself. Never auto-creates a live training pod."))
	fmt.Fprintln(w)

	env := config.Load()
	steps := []struct {
		title string
		note  string
		cmds  []string
		done  bool
	}{
		{
			title: "1. Welcome",
			note:  "Pick an instruct HF model → train an abliterated variant on RunPod → optional Hub upload.",
		},
		{
			title: "2. RunPod key",
			note:  "Required before creating a training pod.",
			cmds:  []string{"runhug connect"},
			done:  env.Connected(),
		},
		{
			title: "3. Hugging Face token",
			note:  "Needed for gated models and for uploading the result (skip uploads with --no-upload).",
			cmds:  []string{"runhug connect hf"},
			done:  env.HFToken != "",
		},
		{
			title: "4. Pick a model",
			note:  "Transformers / safetensors instruct checkpoint — not GGUF.",
			cmds: []string{
				`runhug search -q "instruct" --limit 8`,
				`runhug inspect <org/model>`,
			},
		},
		{
			title: "5. Dry-run make",
			note:  "Plan GPU, disk, trials, and upload target — nothing created.",
			cmds: []string{
				"runhug heretic make <org/model> --dry-run",
				"runhug heretic make <org/model> --dry-run --no-upload",
			},
		},
		{
			title: "6. Live training?",
			note:  "Default No. Creates a billed RunPod pod + live dashboard.",
			cmds:  []string{"runhug heretic make <org/model>"},
		},
		{
			title: "7. Follow-ups",
			note:  "Watch progress or tear down.",
			cmds: []string{
				"runhug heretic status <org/model>",
				"runhug heretic logs <org/model>",
				"runhug heretic stop <org/model>",
			},
		},
	}

	for _, st := range steps {
		status := ""
		if st.done {
			status = "  " + green("already done")
		}
		fmt.Fprintf(w, "%s%s\n", bold(st.title), status)
		if st.note != "" {
			fmt.Fprintln(w, "  "+dim(st.note))
		}
		for _, c := range st.cmds {
			fmt.Fprintln(w, "  "+cyan(c))
		}
		fmt.Fprintln(w)
	}
	fmt.Fprintln(w, dim("Re-run without --yes in a terminal for the interactive walkthrough:"))
	fmt.Fprintln(w, "  "+cyan("runhug heretic wizard"))
	return nil
}

func runHereticWizard() error {
	w := os.Stdout
	heading(w, "Heretic wizard — guided abliteration")
	wizardStep(w, 1, "Welcome")
	fmt.Fprintln(w, dim("Instruct HF model → RunPod training pod → optional Hub upload."))
	fmt.Fprintln(w, dim("Short y/n prompts. Live pod create defaults to No."))
	fmt.Fprintln(w)

	if err := hereticWizardRunpod(w); err != nil {
		return err
	}
	if err := hereticWizardHF(w); err != nil {
		return err
	}

	modelID, err := hereticWizardPickModel(w)
	if err != nil {
		return err
	}
	if modelID == "" {
		fmt.Fprintln(w, yellow("No model selected — done."))
		hereticWizardDone(w, "", hereticWizardOpts{}, false)
		return nil
	}
	printKV(w, "model", bold(modelID))
	fmt.Fprintln(w)

	opts, err := hereticWizardOptions(w)
	if err != nil {
		return err
	}

	if err := hereticWizardDryRun(w, modelID, opts); err != nil {
		fmt.Fprintf(os.Stderr, "%s  dry-run failed: %v\n", yellow("⚠"), err)
		fmt.Fprintln(w, dim("Fix connect / model id, then: "+hereticMakeCmd(modelID, opts, true)))
		fmt.Fprintln(w)
		hereticWizardDone(w, modelID, opts, false)
		return nil
	}

	created, err := hereticWizardLive(w, modelID, opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s  %v\n", yellow("⚠"), err)
		fmt.Fprintln(w)
	}
	hereticWizardDone(w, modelID, opts, created)
	return nil
}

type hereticWizardOpts struct {
	Trials   int
	NoUpload bool
	Private  bool
	Cloud    string
	GPU      string
}

func hereticWizardRunpod(w io.Writer) error {
	wizardStep(w, 2, "RunPod API key")
	env := config.Load()
	if env.Connected() {
		src := "stored"
		if strings.TrimSpace(os.Getenv(config.EnvRunpodAPIKey)) != "" {
			src = "RUNPOD_API_KEY env"
		}
		wizardSkipNote(w, "RunPod already connected ("+src+") — key never printed")
		return nil
	}
	ok, err := confirmPrefErr("Connect RunPod now?", true)
	if err != nil {
		return err
	}
	if !ok {
		fmt.Fprintln(w, yellow("⚠")+"  "+dim("Live training needs RunPod — you can still dry-run later after connect."))
		fmt.Fprintln(w)
		return nil
	}
	return cmdConnect(nil)
}

func hereticWizardHF(w io.Writer) error {
	wizardStep(w, 3, "Hugging Face token")
	env := config.Load()
	if env.HFToken != "" {
		wizardSkipNote(w, "HF token already set — value never printed")
		return nil
	}
	ok, err := confirmPrefErr("Connect Hugging Face now (needed for gated models + uploads)?", true)
	if err != nil {
		return err
	}
	if !ok {
		fmt.Fprintln(w, dim("Skipped — gated downloads / Hub upload need: runhug connect hf (or use --no-upload)"))
		fmt.Fprintln(w)
		return nil
	}
	return cmdConnect([]string{"hf"})
}

func hereticWizardPickModel(w io.Writer) (string, error) {
	wizardStep(w, 4, "Pick a model")
	fmt.Fprintln(w, dim("Transformers instruct checkpoint (not GGUF). Paste org/model or search."))
	fmt.Fprintln(w)

	query, err := readLine("Search query (blank to paste org/model): ")
	if err != nil {
		return "", err
	}
	query = strings.TrimSpace(query)
	if query == "" {
		line, err := readLine("Model id (org/model): ")
		if err != nil {
			return "", err
		}
		line = strings.TrimSpace(line)
		if strings.Contains(line, "/") {
			return line, nil
		}
		return "", nil
	}

	ids, err := wizardShortlist(w, query)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s  shortlist: %v\n", yellow("⚠"), err)
		fmt.Fprintln(w, dim("Paste org/model instead, or: runhug search -q \""+query+"\""))
	}

	for {
		hint := "Pick row #, paste org/model, or another query"
		if len(ids) > 0 {
			hint = fmt.Sprintf("Pick 1-%d, paste org/model, or another query", len(ids))
		}
		line, err := readLine(hint + ": ")
		if err != nil {
			return "", err
		}
		pick, q, repo := parseChoice(line, len(ids))
		switch {
		case repo != "":
			return repo, nil
		case pick > 0 && pick <= len(ids):
			return ids[pick-1], nil
		case q != "":
			ids, err = wizardShortlist(w, q)
			if err != nil {
				fmt.Fprintf(os.Stderr, "%s  %v\n", yellow("⚠"), err)
			}
			continue
		case line == "":
			return "", nil
		default:
			fmt.Fprintln(w, dim("Expected a row number, org/model, or a new query."))
		}
	}
}

func hereticWizardOptions(w io.Writer) (hereticWizardOpts, error) {
	wizardStep(w, 5, "Training options")
	opts := hereticWizardOpts{
		Trials: hereticDefaultTrials,
		Cloud:  hereticDefaultCloud,
	}

	line, err := readLine(fmt.Sprintf("Trials [%d]: ", hereticDefaultTrials))
	if err != nil {
		return opts, err
	}
	if n, err := strconv.Atoi(strings.TrimSpace(line)); err == nil && n > 0 {
		opts.Trials = n
	}

	upload, err := confirmPrefErr("Upload abliterated weights to Hugging Face when done?", true)
	if err != nil {
		return opts, err
	}
	opts.NoUpload = !upload
	if upload {
		priv, err := confirmPrefErr("Create the upload repo as private?", false)
		if err != nil {
			return opts, err
		}
		opts.Private = priv
	}

	secure, err := confirmPrefErr("Use SECURE cloud (vs COMMUNITY)?", true)
	if err != nil {
		return opts, err
	}
	if !secure {
		opts.Cloud = "COMMUNITY"
	}

	gpuLine, err := readLine("GPU pool (blank = auto-size): ")
	if err != nil {
		return opts, err
	}
	opts.GPU = strings.TrimSpace(gpuLine)

	printKV(w, "trials", fmt.Sprintf("%d", opts.Trials))
	if opts.NoUpload {
		printKV(w, "upload", dim("no — keep on pod"))
	} else if opts.Private {
		printKV(w, "upload", "private Hub repo")
	} else {
		printKV(w, "upload", "public Hub repo")
	}
	printKV(w, "cloud", opts.Cloud)
	if opts.GPU != "" {
		printKV(w, "gpu", opts.GPU)
	} else {
		printKV(w, "gpu", dim("auto"))
	}
	fmt.Fprintln(w)
	return opts, nil
}

func hereticMakeArgs(modelID string, opts hereticWizardOpts, dry, yes bool) []string {
	args := []string{modelID}
	if dry {
		args = append(args, "--dry-run")
	}
	if yes {
		args = append(args, "--yes")
	}
	args = append(args, "--trials", strconv.Itoa(opts.Trials))
	args = append(args, "--cloud", opts.Cloud)
	if opts.NoUpload {
		args = append(args, "--no-upload")
	}
	if opts.Private && !opts.NoUpload {
		args = append(args, "--private")
	}
	if opts.GPU != "" {
		args = append(args, "--gpu", opts.GPU)
	}
	return args
}

func hereticMakeCmd(modelID string, opts hereticWizardOpts, dry bool) string {
	parts := []string{"runhug", "heretic", "make", modelID}
	if dry {
		parts = append(parts, "--dry-run")
	}
	parts = append(parts, "--trials", strconv.Itoa(opts.Trials), "--cloud", opts.Cloud)
	if opts.NoUpload {
		parts = append(parts, "--no-upload")
	}
	if opts.Private && !opts.NoUpload {
		parts = append(parts, "--private")
	}
	if opts.GPU != "" {
		parts = append(parts, "--gpu", opts.GPU)
	}
	return strings.Join(parts, " ")
}

func hereticWizardDryRun(w io.Writer, modelID string, opts hereticWizardOpts) error {
	wizardStep(w, 6, "Dry-run make")
	fmt.Fprintln(w, dim("Plan only — GPU, disk, upload target. Nothing created."))
	fmt.Fprintln(w)
	return cmdHereticMake(hereticMakeArgs(modelID, opts, true, false))
}

func hereticWizardLive(w io.Writer, modelID string, opts hereticWizardOpts) (bool, error) {
	wizardStep(w, 7, "Live training?")
	env := config.Load()
	if !env.Connected() {
		fmt.Fprintln(w, yellow("⚠")+"  "+dim("RunPod not connected — skipping live create."))
		fmt.Fprintln(w, dim("Later: "+hereticMakeCmd(modelID, opts, false)))
		fmt.Fprintln(w)
		return false, nil
	}
	ok, err := confirmPrefErr("Create the live heretic training pod now? (bills GPU time)", false)
	if err != nil {
		return false, err
	}
	if !ok {
		fmt.Fprintln(w, dim("Skipped — when ready: "+hereticMakeCmd(modelID, opts, false)))
		fmt.Fprintln(w)
		return false, nil
	}
	// Explicit confirm already collected — pass --yes to avoid a second prompt.
	if err := cmdHereticMake(hereticMakeArgs(modelID, opts, false, true)); err != nil {
		return false, err
	}
	return true, nil
}

func hereticWizardDone(w io.Writer, modelID string, opts hereticWizardOpts, created bool) {
	wizardStep(w, 8, "Done")
	if created && modelID != "" {
		fmt.Fprintln(w, green("✓")+"  "+dim("pod created — dashboard link printed above"))
		fmt.Fprintln(w)
		commands(w, "Next:",
			"runhug heretic status "+modelID,
			"runhug heretic logs "+modelID,
			"runhug heretic stop "+modelID,
		)
		return
	}
	if modelID != "" {
		commands(w, "When ready:",
			hereticMakeCmd(modelID, opts, true),
			hereticMakeCmd(modelID, opts, false),
			"runhug heretic status "+modelID,
		)
		return
	}
	commands(w, "Start over:",
		"runhug heretic wizard",
		"runhug heretic make <org/model> --dry-run",
	)
}
