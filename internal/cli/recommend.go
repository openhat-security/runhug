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
	"github.com/adamsiwiec1/runhug/internal/sizing"
)

func cmdRecommend(args []string) error {
	if len(args) == 0 || isHelpArg(args[0]) {
		printRecommendHelp(os.Stdout)
		return nil
	}
	if strings.EqualFold(args[0], "gpu") {
		return cmdRecommendGPU(args[1:])
	}

	fs := newFlagSet("recommend")
	queryFlag := ""
	fs.StringVar(&queryFlag, "query", "", "use-case query (same as positional; wins if both set)")
	fs.StringVar(&queryFlag, "q", "", "use-case query (same as --query)")
	candidates := fs.Int("candidates", 8, "local shortlist size to score / send to advisor (1-20)")
	baseURL := fs.String("base-url", "", "OpenAI-compatible base URL (default Ollama http://127.0.0.1:11434/v1)")
	model := fs.String("model", "", "advisor chat model id")
	apiKeyEnv := fs.String("api-key-env", "", "env var name holding the advisor API key (value never printed)")
	noLLM := fs.Bool("no-llm", false, "print scored shortlist + GPU hints only (no chat completions)")
	online := fs.Bool("online", false, "allow live Hub search for shortlist (default: local index only)")
	hub := fs.Bool("hub", false, "alias for --online")
	gpuPool := fs.String("gpu", "", "force this Runpod GPU pool in suggestions")
	asJSON := fs.Bool("json", false, "print JSON")
	if err := parseFlags(fs, args); err != nil {
		return err
	}

	query := resolveSearchQuery(queryFlag, strings.Join(fs.Args(), " "))
	if strings.TrimSpace(query) == "" {
		printRecommendHelp(os.Stderr)
		return fmt.Errorf("query required")
	}

	n := *candidates
	if n < 1 {
		n = 1
	}
	if n > 20 {
		n = 20
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	ramGB := recommend.RAMGB()
	intent := recommend.ParseIntent(query, ramGB)

	// Search the raw user query (not intent.Query, which defaults to "instruct"
	// for unmatched niches like "hacking"). Lexical likes sort matches
	// `search -q … --sort likes --no-semantic`.
	models, meta, err := searchModels(ctx, searchRequest{
		Query:           query,
		Task:            "any",
		Sort:            "likes",
		Limit:           max(n*3, 24),
		DisableSemantic: true,
		Online:          *online || *hub,
	})
	if err != nil {
		return err
	}
	var scored []recommend.Scored
	if *noLLM {
		// Keep likes order for --no-llm; do not bury niche publishers via Score.
		for _, m := range models {
			if len(scored) >= n {
				break
			}
			scored = append(scored, recommend.Scored{Model: m, Score: 0, Why: []string{"likes"}})
		}
	} else {
		scored = recommend.Score(models, intent)
	}
	if len(scored) == 0 {
		return fmt.Errorf("no candidates in local index for %q — try a broader query, run update, or pass --online", query)
	}
	if len(scored) > n {
		scored = scored[:n]
	}

	liveGPUs := loadGPUCatalog(ctx)
	gpuHints := make([]string, len(scored))
	gpuAdvices := make([]recommend.GPUAdvice, len(scored))
	for i, s := range scored {
		adv, gerr := recommend.AdviseGPU(s.Model, liveGPUs, *gpuPool, 8192)
		if gerr != nil {
			gpuHints[i] = fmt.Sprintf("VRAM ~%.1f GB (pool pick failed: %v)", adv.RequiredGB, gerr)
			continue
		}
		gpuAdvices[i] = adv
		gpuHints[i] = adv.Text
	}

	settings := config.LoadSettings()
	advisorURL := strings.TrimSpace(*baseURL)
	if advisorURL == "" {
		advisorURL = strings.TrimSpace(settings.AdvisorBaseURL)
	}
	if advisorURL == "" {
		advisorURL = recommend.DefaultAdvisorBaseURL
	}
	advisorModel := strings.TrimSpace(*model)
	if advisorModel == "" {
		advisorModel = strings.TrimSpace(settings.AdvisorModel)
	}

	var apiKey string
	if envName := strings.TrimSpace(*apiKeyEnv); envName != "" {
		apiKey = config.SanitizeAPIKey(os.Getenv(envName))
		if apiKey == "" {
			return fmt.Errorf("--api-key-env %s is empty or unset (value never printed)", envName)
		}
	}

	var advice string
	if !*noLLM {
		advice, err = recommend.Advise(ctx, recommend.AdvisorOpts{
			BaseURL: advisorURL,
			Model:   advisorModel,
			APIKey:  apiKey,
		}, query, scored, gpuHints)
		if err != nil {
			return err
		}
	}

	if *asJSON {
		out := map[string]any{
			"query":       query,
			"intent":      intent,
			"rank_source": meta.RankSource,
			"candidates":  scored,
			"gpu":         gpuAdvices,
			"no_llm":      *noLLM,
		}
		if advice != "" {
			out["advice"] = advice
		}
		out["advisor"] = map[string]any{
			"base_url": advisorURL,
			"model":    advisorModel,
			// never include api key
		}
		return writeJSON(out)
	}

	heading(os.Stdout, "Recommend")
	printKV(os.Stdout, "query", query)
	printKV(os.Stdout, "intent", fmt.Sprintf("%s  max≈%.1fB  target≈%.1fB  RAM≈%.0fGB", intent.Task, intent.MaxParamsB, intent.TargetParamsB, ramGB))
	printKV(os.Stdout, "index", meta.RankSource)
	fmt.Fprintln(os.Stdout)
	fmt.Fprintln(os.Stdout, bold("Shortlist"))
	for i, s := range scored {
		detail := fmt.Sprintf("score=%.3f  %s", s.Score, strings.Join(s.Why, " · "))
		if *noLLM {
			detail = fmt.Sprintf("♥ %s  ↓ %s", formatCount(int64(s.Model.Likes)), formatCount(s.Model.Downloads))
			if why := strings.Join(s.Why, " · "); why != "" {
				detail = detail + "  " + why
			}
		}
		fmt.Fprintf(os.Stdout, "  %s  %s  %s\n",
			cyan(fmt.Sprintf("%d)", i+1)),
			bold(s.Model.RepoID()),
			dim(detail),
		)
		if i < len(gpuHints) && gpuHints[i] != "" {
			printKV(os.Stdout, "gpu", gpuHints[i])
		}
	}
	fmt.Fprintln(os.Stdout)
	if *noLLM {
		fmt.Fprintln(os.Stdout, dim("(--no-llm: skipped OpenAI-compatible advisor)"))
	} else {
		heading(os.Stdout, "Advisor")
		printKV(os.Stdout, "endpoint", advisorURL+"  model="+dash(advisorModel))
		fmt.Fprintln(os.Stdout)
		fmt.Fprintln(os.Stdout, advice)
		fmt.Fprintln(os.Stdout)
	}
	if len(scored) > 0 {
		commands(os.Stdout, "Next:",
			"runhug inspect "+scored[0].Model.RepoID(),
			"runhug recommend gpu "+scored[0].Model.RepoID(),
			"runhug deploy "+scored[0].Model.RepoID()+" --dry-run",
		)
	}
	return nil
}

func cmdRecommendGPU(args []string) error {
	if len(args) == 0 || isHelpArg(args[0]) {
		printRecommendGPUHelp(os.Stdout)
		return nil
	}
	fs := newFlagSet("recommend-gpu")
	gpuPool := fs.String("gpu", "", "force this Runpod GPU pool")
	maxLen := fs.Int("max-len", 8192, "context length for VRAM estimate")
	estimate := fs.Bool("estimate", false, "print full approximate cost block (cold/warm/daily scenarios)")
	fs.BoolVar(estimate, "e", false, "alias for --estimate")
	verbose := fs.Bool("verbose", false, "include cost assumptions and extra detail")
	fs.BoolVar(verbose, "v", false, "alias for --verbose")
	asJSON := fs.Bool("json", false, "print JSON")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	if fs.NArg() < 1 {
		printRecommendGPUHelp(os.Stderr)
		return fmt.Errorf("model required")
	}
	modelID := fs.Arg(0)

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	env := config.Load()
	var model *hf.Model
	var err error
	// Prefer Hub card when token/network available; fall back to local index row.
	model, err = hf.New(env.HFToken).Get(ctx, modelID)
	if err != nil {
		models, _, serr := searchModels(ctx, searchRequest{
			Query: modelID,
			Task:  "any",
			Sort:  "relevance",
			Limit: 5,
		})
		if serr != nil || len(models) == 0 {
			return fmt.Errorf("model %q: %v", modelID, err)
		}
		// exact id match preferred
		found := models[0]
		for _, m := range models {
			if strings.EqualFold(m.RepoID(), modelID) {
				found = m
				break
			}
		}
		model = &found
		fmt.Fprintf(os.Stderr, "%s  Hub fetch failed; using local index metadata for %s\n", yellow("⚠"), model.RepoID())
	}

	live := loadGPUCatalog(ctx)
	prefer := applySavedRunpodGPU(*gpuPool)
	if prefer != "" && strings.TrimSpace(*gpuPool) == "" {
		fmt.Fprintf(os.Stderr, "%s using saved GPU preference %s\n", dim("note:"), prefer)
	}
	adv, err := recommend.AdviseGPU(*model, live, prefer, *maxLen)
	if err != nil {
		return err
	}
	if *asJSON {
		return writeJSON(map[string]any{
			"model":  model.RepoID(),
			"gpu":    adv,
			"params": adv.Params,
		})
	}
	heading(os.Stdout, "Suggested GPU")
	printKV(os.Stdout, "model", bold(model.RepoID()))
	printKV(os.Stdout, "params", adv.Params)
	if adv.WeightGB > 0 {
		printKV(os.Stdout, "vram", fmt.Sprintf("%.1f GB weights → %.1f GB with overhead", adv.WeightGB, adv.RequiredGB))
	} else {
		printKV(os.Stdout, "vram", fmt.Sprintf("~%.1f GB required (estimate)", adv.RequiredGB))
	}
	printKV(os.Stdout, "gpu", adv.Text)
	if *verbose {
		printKV(os.Stdout, "why", adv.Choice.Reason)
	}
	fmt.Fprintln(os.Stdout)
	if *estimate {
		cost := sizing.EstimateServerlessCost(adv.Choice.HourlyUSD, adv.WeightGB, adv.Choice.GPUCount, 5, true)
		printCostBlock(os.Stdout, cost.FormatBlock(adv.Choice.Pool.ID, *verbose))
		fmt.Fprintln(os.Stdout)
	}
	// Also list a few larger/safer alternatives when live/offline catalog is available.
	catalog := live
	if len(catalog) == 0 {
		catalog = runpod.OfflineCatalog()
	}
	opts := runpod.FittingOptions(catalog, adv.RequiredGB, 5)
	if len(opts) > 1 {
		title := bold("Other fitting pools")
		if *verbose {
			title += "  " + dim("pass --estimate / -e for full cost scenarios")
		}
		fmt.Fprintln(os.Stdout, title)
		for i, pool := range opts {
			tag := ""
			if i == 0 {
				tag = "  recommended"
			} else if pool.MemoryGB > opts[0].MemoryGB+0.01 {
				tag = "  larger / safer"
			}
			fmt.Fprintf(os.Stdout, "  %s  %s  %s%s\n",
				cyan(fmt.Sprintf("%d)", i+1)),
				bold(pool.ID),
				dim(fmt.Sprintf("%.0f GB · $%.2f/hr · %s", pool.MemoryGB, pool.PricePerHour, poolStockLabel(pool))),
				dim(tag),
			)
		}
		fmt.Fprintln(os.Stdout)
	}
	commands(os.Stdout, "Next:",
		"runhug inspect "+model.RepoID(),
		"runhug recommend gpu "+model.RepoID()+" --estimate",
		"runhug deploy "+model.RepoID()+" --dry-run",
		"runhug deploy "+model.RepoID()+" --dry-run --estimate",
		"runhug gpus --min-vram "+fmt.Sprintf("%.0f", adv.RequiredGB),
	)
	return nil
}

func printRecommendHelp(w io.Writer) {
	helpUsage(w, "runhug recommend <query>")
	fmt.Fprintln(w, dim("Shortlist models for a use-case · optional local LLM advisor"))
	fmt.Fprintln(w)

	helpSection(w, "commands")
	helpCmd(w, "recommend <query>", "score a shortlist (+ advisor unless --no-llm)")
	helpCmd(w, "recommend gpu <m>", "VRAM / GPU pool for one model")
	fmt.Fprintln(w)

	helpSection(w, "flags")
	helpFlag(w, "--query -q", "use-case query (same as positional)")
	helpFlag(w, "--candidates", "shortlist size (1-20, default 8)")
	helpFlag(w, "--no-llm", "scored shortlist + GPU hints only")
	helpFlag(w, "--online --hub", "allow live Hub search for shortlist")
	helpFlag(w, "--json", "print JSON")
	fmt.Fprintln(w)

	fmt.Fprintf(w, "%s %s\n", dim("example:"), cyan(`runhug recommend "best model for RAG on a 16GB laptop"`))
}

func printRecommendGPUHelp(w io.Writer) {
	helpUsage(w, "runhug recommend gpu <org/model>")
	fmt.Fprintln(w, dim("VRAM estimate + suggested serverless GPU pool"))
	fmt.Fprintln(w)

	helpSection(w, "flags")
	helpFlag(w, "--gpu", "force this Runpod GPU pool")
	helpFlag(w, "--max-len", "context length for VRAM estimate")
	helpFlag(w, "--estimate -e", "full cost block (cold/warm/daily)")
	helpFlag(w, "--verbose -v", "cost assumptions + extra detail")
	helpFlag(w, "--json", "print JSON")
	fmt.Fprintln(w)

	fmt.Fprintf(w, "%s %s\n", dim("example:"), cyan("runhug recommend gpu Qwen/Qwen2.5-7B-Instruct --estimate"))
}

func loadGPUCatalog(ctx context.Context) []runpod.GPU {
	env := config.Load()
	if env.RunpodAPIKey == "" {
		return nil
	}
	gpus, err := runpod.New(env.RunpodAPIKey).ListGPUs(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s  live GPU catalog unavailable; using offline estimates\n", dim("·"))
		return nil
	}
	return gpus
}
