package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/adamsiwiec1/runhug/internal/config"
	"github.com/adamsiwiec1/runhug/internal/hf"
	"github.com/adamsiwiec1/runhug/internal/recommend"
	"github.com/adamsiwiec1/runhug/internal/runpod"
	"github.com/adamsiwiec1/runhug/internal/store"
)

func cmdWizard(args []string) error {
	fs := newFlagSet("wizard")
	yes := fs.Bool("yes", false, "print the guided checklist (non-interactive; never live-deploys)")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	if *yes || !canPrompt() {
		return wizardChecklist(os.Stdout)
	}
	return runWizard()
}

func wizardChecklist(w io.Writer) error {
	heading(w, "Wizard — guided setup (checklist)")
	fmt.Fprintln(w, dim("Non-interactive / --yes: follow these steps yourself. Never auto-creates a live Runpod endpoint."))
	fmt.Fprintln(w)

	env := config.Load()
	s := config.LoadSettings()
	steps := []struct {
		title string
		note  string
		cmds  []string
		done  bool
	}{
		{
			title: "1. Welcome",
			note:  "Find an HF model → deploy on Runpod → pennies/hour while a worker is up.",
		},
		{
			title: "2. Search index",
			note:  "Category packs (local SQLite search).",
			cmds:  []string{"runhug packs install"},
			done:  fileExists(indexFilePath()) || fileExists(bundledIndexPath()),
		},
		{
			title: "3. HF token",
			note:  "Optional — gated Hub cards + cloud embeddings.",
			cmds:  []string{"runhug connect hf"},
			done:  env.HFToken != "",
		},
		{
			title: "4. Runpod key",
			note:  "Required before live deploy.",
			cmds:  []string{"runhug connect"},
			done:  env.Connected(),
		},
		{
			title: "5. Advisor (optional)",
			note:  "OpenAI-compatible chat for recommend (default Ollama).",
			cmds: []string{
				"runhug config set advisor_base_url http://127.0.0.1:11434/v1",
				"runhug config set advisor_model <chat-model>",
			},
			done: s.AdvisorBaseURL != "" || s.AdvisorModel != "",
		},
		{
			title: "6. Find a model",
			note:  "Use-case → shortlist → pick org/model.",
			cmds: []string{
				`runhug recommend -q "your use case" --no-llm`,
				`runhug search -q "your use case" --limit 8`,
			},
		},
		{
			title: "7. GPU sizing",
			note:  "Interactive pool picker (e = estimate for highlighted GPU); use --estimate on recommend gpu / deploy --dry-run for full costs.",
			cmds:  []string{"runhug recommend gpu <org/model>", "runhug recommend gpu <org/model> --estimate", "runhug deploy <org/model> --dry-run --gpu <POOL>"},
		},
		{
			title: "8. Deploy dry-run",
			note:  "Always plan first — cost/GPU estimate, nothing created.",
			cmds:  []string{"runhug deploy <org/model> --dry-run", "runhug deploy <org/model> --dry-run --estimate"},
		},
		{
			title: "9. Live deploy?",
			note:  "Default No. Only with an explicit confirm + Runpod connected.",
			cmds:  []string{"runhug deploy <org/model>"},
		},
		{
			title: "10. Proxy",
			note:  "OpenAI-compatible local proxy after deploy.",
			cmds:  []string{"runhug proxy"},
		},
		{
			title: "11. Done",
			note:  "Useful follow-ups.",
			cmds: []string{
				"runhug list",
				"runhug status",
				"runhug search -q \"…\"",
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
	fmt.Fprintln(w, dim("Re-run without --yes in a terminal for the interactive walkthrough."))
	return nil
}

func runWizard() error {
	w := os.Stdout
	heading(w, "Wizard — guided setup")
	wizardStep(w, 1, "Welcome")
	fmt.Fprintln(w, dim("Find an HF model → deploy on Runpod → pennies while a worker is up."))
	fmt.Fprintln(w, dim("Short y/n prompts. Live deploy defaults to No."))
	fmt.Fprintln(w)

	if err := wizardInit(w); err != nil {
		return err
	}
	if err := wizardHF(w); err != nil {
		return err
	}
	if err := wizardRunpod(w); err != nil {
		return err
	}
	if err := wizardAdvisor(w); err != nil {
		return err
	}

	modelID, err := wizardFindModel(w)
	if err != nil {
		return err
	}
	if modelID == "" {
		fmt.Fprintln(w, yellow("No model selected — skipping deploy steps."))
		wizardDone(w, "", false)
		return nil
	}
	printKV(w, "model", bold(modelID))
	fmt.Fprintln(w)

	gpuPool, err := wizardGPU(w, modelID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s  %v\n", yellow("⚠"), err)
		fmt.Fprintln(w, dim("Continuing — you can still dry-run deploy."))
		fmt.Fprintln(w)
	}
	if gpuPool != "" {
		printKV(w, "gpu", bold(gpuPool))
		fmt.Fprintln(w)
	}

	if err := wizardDeployDryRun(w, modelID, gpuPool); err != nil {
		fmt.Fprintf(os.Stderr, "%s  dry-run failed: %v\n", yellow("⚠"), err)
		cmd := "runhug deploy " + modelID + " --dry-run"
		if gpuPool != "" {
			cmd += " --gpu " + gpuPool
		}
		fmt.Fprintln(w, dim("Fix connect / network, then: "+cmd))
		fmt.Fprintln(w)
		wizardDone(w, modelID, false)
		return nil
	}

	deployed, err := wizardLiveDeploy(w, modelID, gpuPool)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s  %v\n", yellow("⚠"), err)
		fmt.Fprintln(w)
	}

	if err := wizardProxy(w, deployed); err != nil {
		return err
	}
	wizardDone(w, modelID, deployed)
	return nil
}

func wizardStep(w io.Writer, n int, title string) {
	fmt.Fprintf(w, "%s  %s\n\n", cyan(fmt.Sprintf("%d)", n)), bold(title))
}

func wizardSkipNote(w io.Writer, msg string) {
	fmt.Fprintln(w, green("✓")+"  "+dim(msg))
	fmt.Fprintln(w)
}

func wizardInit(w io.Writer) error {
	wizardStep(w, 2, "Init search stack")
	hasIndex := fileExists(indexFilePath()) || fileExists(bundledIndexPath())
	if hasIndex {
		path := indexFilePath()
		if !fileExists(path) {
			path = bundledIndexPath()
		}
		wizardSkipNote(w, "index already present — "+path)
		ok, err := confirmPrefErr("Re-run init (embedder / packs) anyway?", false)
		if err != nil {
			return err
		}
		if !ok {
			return nil
		}
	}
	// Reuse existing init flow (prompts inside).
	return initSearchStack(false, "", true)
}

func wizardHF(w io.Writer) error {
	wizardStep(w, 3, "Hugging Face token")
	env := config.Load()
	if env.HFToken != "" {
		src := "env/stored"
		if config.HasStoredHFToken() && strings.TrimSpace(os.Getenv(config.EnvHFToken)) == "" {
			src = "stored"
		} else if strings.TrimSpace(os.Getenv(config.EnvHFToken)) != "" {
			src = "HF_TOKEN env"
		}
		wizardSkipNote(w, "HF token already set ("+src+") — value never printed")
		return nil
	}
	ok, err := confirmPrefErr("Connect Hugging Face now (connect hf)?", true)
	if err != nil {
		return err
	}
	if !ok {
		fmt.Fprintln(w, dim("Skipped — gated models / cloud embeddings need: runhug connect hf"))
		fmt.Fprintln(w)
		return nil
	}
	return cmdConnect([]string{"hf"})
}

func wizardRunpod(w io.Writer) error {
	wizardStep(w, 4, "Runpod API key")
	env := config.Load()
	if env.Connected() {
		src := "stored"
		if strings.TrimSpace(os.Getenv(config.EnvRunpodAPIKey)) != "" {
			src = "RUNPOD_API_KEY env"
		}
		wizardSkipNote(w, "Runpod already connected ("+src+") — key never printed")
		return nil
	}
	ok, err := confirmPrefErr("Connect Runpod now?", true)
	if err != nil {
		return err
	}
	if !ok {
		fmt.Fprintln(w, yellow("⚠")+"  "+dim("Live deploy needs Runpod — you can still dry-run later after connect."))
		fmt.Fprintln(w)
		return nil
	}
	return cmdConnect(nil)
}

func wizardAdvisor(w io.Writer) error {
	wizardStep(w, 5, "Advisor (optional)")
	s := config.LoadSettings()
	if s.AdvisorBaseURL != "" || s.AdvisorModel != "" {
		url := s.AdvisorBaseURL
		if url == "" {
			url = recommend.DefaultAdvisorBaseURL
		}
		wizardSkipNote(w, "advisor already configured — "+url+"  model="+dash(s.AdvisorModel))
		ok, err := confirmPrefErr("Reconfigure advisor?", false)
		if err != nil {
			return err
		}
		if !ok {
			return nil
		}
	}
	ok, err := confirmPrefErr("Configure OpenAI-compatible advisor for recommend?", false)
	if err != nil {
		return err
	}
	if !ok {
		fmt.Fprintln(w, dim("Skipped — recommend --no-llm still works; set later via config set advisor_*"))
		fmt.Fprintln(w)
		return nil
	}

	useOllama, err := confirmPrefErr("Use local Ollama default (http://127.0.0.1:11434/v1)?", true)
	if err != nil {
		return err
	}
	base := recommend.DefaultAdvisorBaseURL
	if !useOllama {
		line, err := readLine("Advisor base URL: ")
		if err != nil {
			return err
		}
		line = strings.TrimRight(strings.TrimSpace(line), "/")
		if line != "" {
			base = line
		}
	}
	if err := cmdConfigSet([]string{"advisor_base_url", base}); err != nil {
		return err
	}
	model, err := readLine("Advisor model id (blank to skip): ")
	if err != nil {
		return err
	}
	if strings.TrimSpace(model) != "" {
		if err := cmdConfigSet([]string{"advisor_model", strings.TrimSpace(model)}); err != nil {
			return err
		}
	}
	return nil
}

func wizardFindModel(w io.Writer) (string, error) {
	wizardStep(w, 6, "Find a model")
	query, err := readLine("Use case (e.g. coding assistant on 24GB): ")
	if err != nil {
		return "", err
	}
	query = strings.TrimSpace(query)
	if query == "" {
		line, err := readLine("Paste org/model instead (or blank to skip): ")
		if err != nil {
			return "", err
		}
		line = strings.TrimSpace(line)
		if strings.Contains(line, "/") {
			return line, nil
		}
		return "", nil
	}

	// Prefer recommend --no-llm shortlist; fall back to local search.
	ids, err := wizardShortlist(w, query)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s  shortlist: %v\n", yellow("⚠"), err)
		fmt.Fprintln(w, dim("Try paste org/model, or run: runhug search -q \""+query+"\""))
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

func wizardShortlist(w io.Writer, query string) ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// Lexical search on the raw user query, sorted by likes — same behavior as
	// `search -q … --sort likes --no-semantic`. Do not rewrite via ParseIntent
	// (defaults Query to "instruct") or re-rank with recommend.Score (known-
	// publisher bias), both of which bury niche cyber / domain models.
	models, meta, err := searchModels(ctx, searchRequest{
		Query:           query,
		Task:            "any",
		Sort:            "likes",
		Limit:           24,
		DisableSemantic: true,
		Online:          false,
	})
	if err != nil {
		return nil, err
	}
	if len(models) == 0 {
		return nil, fmt.Errorf("no candidates for %q — try broader terms or run update", query)
	}
	if len(models) > 8 {
		models = models[:8]
	}

	fmt.Fprintln(w, bold("Shortlist")+"  "+dim(meta.RankSource))
	ids := make([]string, 0, len(models))
	for i, m := range models {
		id := m.RepoID()
		ids = append(ids, id)
		hint := hfTaskHint(m)
		stats := fmt.Sprintf("♥ %s  ↓ %s", formatCount(int64(m.Likes)), formatCount(m.Downloads))
		if hint != "" {
			stats = stats + "  " + hint
		}
		fmt.Fprintf(w, "  %s  %s  %s\n",
			cyan(fmt.Sprintf("%d)", i+1)),
			bold(id),
			dim(stats),
		)
	}
	fmt.Fprintln(w)
	return ids, nil
}

func hfTaskHint(m hf.Model) string {
	if m.PipelineTag != "" {
		return m.PipelineTag
	}
	return ""
}

func wizardGPU(w io.Writer, modelID string) (string, error) {
	wizardStep(w, 7, "GPU sizing")

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	model, err := resolveWizardModel(ctx, modelID)
	if err != nil {
		return "", err
	}
	live := loadGPUCatalog(ctx)
	prefer := applySavedRunpodGPU("")
	adv, err := recommend.AdviseGPU(*model, live, prefer, 8192)
	if err != nil {
		return "", err
	}

	heading(w, "VRAM estimate")
	printKV(w, "model", bold(model.RepoID()))
	printKV(w, "params", adv.Params)
	if adv.WeightGB > 0 {
		printKV(w, "vram", fmt.Sprintf("%.1f GB weights → %.1f GB with overhead", adv.WeightGB, adv.RequiredGB))
	} else {
		printKV(w, "vram", fmt.Sprintf("~%.1f GB required (estimate)", adv.RequiredGB))
	}
	if adv.Offline {
		fmt.Fprintln(w, dim("Using offline GPU catalog (connect Runpod for live stock/prices)."))
	}
	fmt.Fprintln(w)

	catalog := live
	if len(catalog) == 0 {
		catalog = runpod.OfflineCatalog()
	}
	opts := runpod.FittingOptions(catalog, adv.RequiredGB, 5)
	if len(opts) == 0 {
		// Fall back to the single Suggest pick (may be multi-GPU).
		if adv.Choice.Pool.ID == "" {
			return "", fmt.Errorf("no fitting GPU pools for ~%.1f GB", adv.RequiredGB)
		}
		fmt.Fprintln(w, bold("Suggested")+"  "+dim(adv.Choice.Reason))
		fmt.Fprintf(w, "  %s  %s\n", cyan("1)"), bold(adv.Choice.Pool.ID)+"  "+dim(adv.Text))
		fmt.Fprintln(w)
		return adv.Choice.Pool.ID, nil
	}

	return pickGPUPool(w, opts, adv.RequiredGB, adv.WeightGB)
}

func resolveWizardModel(ctx context.Context, modelID string) (*hf.Model, error) {
	env := config.Load()
	model, err := hf.New(env.HFToken).Get(ctx, modelID)
	if err == nil {
		return model, nil
	}
	models, _, serr := searchModels(ctx, searchRequest{
		Query:           modelID,
		Task:            "any",
		Sort:            "likes",
		Limit:           5,
		DisableSemantic: true,
	})
	if serr != nil || len(models) == 0 {
		return nil, fmt.Errorf("model %q: %v", modelID, err)
	}
	found := models[0]
	for _, m := range models {
		if strings.EqualFold(m.RepoID(), modelID) {
			found = m
			break
		}
	}
	fmt.Fprintf(os.Stderr, "%s  Hub fetch failed; using local index metadata for %s\n", yellow("⚠"), found.RepoID())
	return &found, nil
}

func deployArgs(modelID string, gpuPool string, extra ...string) []string {
	args := []string{modelID}
	args = append(args, extra...)
	if strings.TrimSpace(gpuPool) != "" {
		args = append(args, "--gpu", strings.TrimSpace(gpuPool))
	}
	return args
}

func wizardDeployDryRun(w io.Writer, modelID, gpuPool string) error {
	wizardStep(w, 8, "Deploy dry-run")
	env := config.Load()
	if !env.Connected() {
		fmt.Fprintln(w, yellow("⚠")+"  "+dim("Runpod not connected — dry-run needs an API key for the GPU catalog."))
		ok, err := confirmPrefErr("Connect Runpod now so dry-run can size a pool?", true)
		if err != nil {
			return err
		}
		if ok {
			if err := cmdConnect(nil); err != nil {
				return err
			}
		} else {
			return fmt.Errorf("not connected — run `runhug connect` then deploy --dry-run")
		}
	}
	fmt.Fprintln(w, dim("Planning only — nothing will be created."))
	if gpuPool != "" {
		printKV(w, "gpu", bold(gpuPool))
	}
	fmt.Fprintln(w)
	return cmdDeploy(deployArgs(modelID, gpuPool, "--dry-run"))
}

func wizardLiveDeploy(w io.Writer, modelID, gpuPool string) (bool, error) {
	wizardStep(w, 9, "Live deploy?")
	env := config.Load()
	if !env.Connected() {
		fmt.Fprintln(w, yellow("⚠")+"  "+dim("Runpod not connected — skipping live create."))
		later := "runhug connect && runhug deploy " + modelID
		if gpuPool != "" {
			later += " --gpu " + gpuPool
		}
		fmt.Fprintln(w, dim("Later: "+later))
		fmt.Fprintln(w)
		return false, nil
	}
	fmt.Fprintln(w, dim("This creates a serverless endpoint and can bill while a worker is up."))
	if gpuPool != "" {
		printKV(w, "gpu", bold(gpuPool))
	}
	ok, err := confirmPrefErr("Create the live Runpod endpoint now?", false)
	if err != nil {
		return false, err
	}
	if !ok {
		fmt.Fprintln(w, dim("Skipped live deploy (default). Dry-run plan above still stands."))
		fmt.Fprintln(w)
		return false, nil
	}
	// Explicit confirm already collected — pass --yes to avoid a second prompt.
	if err := cmdDeploy(deployArgs(modelID, gpuPool, "--yes")); err != nil {
		return false, err
	}
	return true, nil
}

func wizardProxy(w io.Writer, deployed bool) error {
	wizardStep(w, 10, "Proxy")
	reg, _, err := store.Load()
	if err != nil {
		return err
	}
	hasEndpoint := false
	if reg != nil {
		for _, m := range reg.Models {
			if m.Kind() == store.BackendRunpod && m.EndpointID != "" {
				hasEndpoint = true
				break
			}
		}
	}
	if !deployed && !hasEndpoint {
		fmt.Fprintln(w, dim("No Runpod endpoint in the registry yet."))
		commands(w, "After deploy:",
			"runhug proxy",
			"curl http://127.0.0.1:8080/v1/models",
		)
		return nil
	}
	ok, err := confirmPrefErr("Start the OpenAI proxy now? (blocks until Ctrl-C)", false)
	if err != nil {
		return err
	}
	if !ok {
		commands(w, "When ready:",
			"runhug proxy",
			"curl http://127.0.0.1:8080/v1/models",
		)
		return nil
	}
	return cmdProxy(nil)
}

func wizardDone(w io.Writer, modelID string, deployed bool) {
	wizardStep(w, 11, "Done")
	fmt.Fprintln(w, green("You're set.")+"  "+dim("Next commands:"))
	fmt.Fprintln(w)
	next := []string{
		"runhug search -q \"…\"",
		"runhug recommend -q \"…\"",
		"runhug list",
		"runhug proxy",
	}
	if modelID != "" {
		next = append([]string{
			"runhug inspect " + modelID,
			"runhug recommend gpu " + modelID,
			"runhug deploy " + modelID + " --dry-run",
		}, next...)
		if !deployed {
			next = append([]string{"runhug deploy " + modelID}, next...)
		}
	}
	commands(w, "", next...)
	fmt.Fprintln(w, dim("Tip: runhug wizard --yes prints this flow as a checklist."))
}
