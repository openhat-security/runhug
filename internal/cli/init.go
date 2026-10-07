package cli

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/adamsiwiec1/runhug/internal/config"
	"github.com/adamsiwiec1/runhug/internal/runtime"
	"github.com/adamsiwiec1/runhug/internal/semantic"
)

const defaultEmbedModel = "nomic-embed-text"

func cmdInit(args []string) error {
	fs := newFlagSet("init")
	yes := fs.Bool("yes", false, "pull the embedder / refresh index without prompts when possible")
	wantRuntime := fs.String("runtime", "", "ollama (default when installing a local embedder)")
	model := fs.String("model", "", "optional: Hub id or Ollama tag to install as a local serve model (not the search default)")
	search := fs.String("search", "", "optional: Hub search then install a local serve model")
	pick := fs.Int("pick", 0, "with --search, 1-based row (default: ask, or 1 with --yes)")
	limit := fs.Int("limit", 8, "Hub rows when choosing a local serve model (1-100)")
	skipIndex := fs.Bool("skip-index", false, "do not offer to refresh the local search index")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	*limit = clampLimit(*limit)
	if *model == "" && fs.NArg() > 0 {
		*model = strings.TrimSpace(fs.Arg(0))
	}

	heading(os.Stdout, "Init — search NLP setup")
	fmt.Fprintln(os.Stdout, dim("Search uses the local SQLite index; update refreshes it from the Hub."))
	fmt.Fprintln(os.Stdout, dim("No local chat model is required. Deploy to Runpod stays separate."))
	fmt.Fprintln(os.Stdout)

	// Optional escape hatch: install a local serve model (not the product default).
	if *model != "" || *search != "" {
		eng, err := resolveEngine(*wantRuntime, *yes, true)
		if err != nil {
			runtime.PrintConfig(os.Stderr, runtime.Detect())
			return err
		}
		printKV(os.Stdout, "runtime", eng.Kind+"  "+dash(eng.Binary))
		fmt.Fprintln(os.Stdout)
		if *model != "" {
			return activateLocal(eng, specFromOverride(*model, eng.Kind))
		}
		return initFromSearch(eng, *search, *pick, *limit, *yes)
	}

	if err := initSearchStack(*yes, *wantRuntime, !*skipIndex); err != nil {
		return err
	}
	return nil
}

func initSearchStack(yes bool, wantRuntime string, offerIndex bool) error {
	env := config.Load()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	ollamaUp := runtime.PortOpen(runtime.OllamaPort)
	embedder := semantic.Discover(ctx, env.HFToken)
	printKV(os.Stdout, "ollama", ollamaStatus(ollamaUp))
	if embedder != nil {
		printKV(os.Stdout, "embedder", green(embedder.Label()))
	} else if local := semantic.LocalOllamaEmbed(); local != "" {
		printKV(os.Stdout, "embedder", dim(local+" (ollama) — search uses HF_TOKEN or lexical; set RUNHUG_OLLAMA_EMBED=1 to opt in"))
	} else {
		printKV(os.Stdout, "embedder", yellow("none — lexical search; connect hf for cloud semantic rank"))
	}
	if env.HFToken != "" {
		printKV(os.Stdout, "hf token", green("set (HF Inference embeddings available)"))
	} else {
		printKV(os.Stdout, "hf token", dim("not set — runhug connect hf for cloud embeddings"))
	}
	fmt.Fprintln(os.Stdout)

	if embedder == nil {
		if err := initEnsureEmbedder(yes, wantRuntime, ollamaUp); err != nil {
			return err
		}
	} else {
		fmt.Fprintln(os.Stdout, green("Search embeddings ready."))
		fmt.Fprintln(os.Stdout)
	}

	if offerIndex {
		if err := initOfferIndex(yes); err != nil {
			return err
		}
	}

	commands(os.Stdout, "Next:",
		`runhug search -q "top penetration testing models" --limit 5`,
		`runhug search -q "animated cartoon generation models" --limit 5`,
		"runhug update",
		"runhug connect",
		"runhug deploy <org/model>",
	)
	return nil
}

func ollamaStatus(up bool) string {
	if up {
		return green("up") + "  " + cyan(runtime.OllamaURL)
	}
	snap := runtime.Detect()
	for _, e := range snap.Engines {
		if e.Kind == runtime.Ollama && e.Present {
			return yellow("installed, not running") + "  try: ollama serve"
		}
	}
	return dim("not detected — install Ollama or use connect hf for embeddings")
}

func initEnsureEmbedder(yes bool, wantRuntime string, ollamaUp bool) error {
	if ollamaUp || wantRuntime == runtime.Ollama || wantRuntime == "" {
		snap := runtime.Detect()
		var eng *runtime.Engine
		for i := range snap.Engines {
			if snap.Engines[i].Kind == runtime.Ollama && snap.Engines[i].Present {
				eng = &snap.Engines[i]
				break
			}
		}
		if eng != nil || ollamaUp {
			bin := ""
			if eng != nil {
				bin = eng.Binary
			}
			ok := yes
			if !ok && canPrompt() {
				var err error
				ok, err = confirmPrefErr(fmt.Sprintf("Pull Ollama embedder %s for local semantic search?", defaultEmbedModel), true)
				if err != nil {
					return err
				}
			}
			if ok {
				if err := runtime.PullOllama(bin, defaultEmbedModel); err != nil {
					fmt.Fprintf(os.Stderr, "%s\n", yellow(err.Error()))
					fmt.Fprintln(os.Stdout, dim("Falling back: runhug connect hf  (HF Inference embeddings)"))
					return nil
				}
				fmt.Fprintf(os.Stdout, "%s  %s\n\n", green("Ready"), bold(defaultEmbedModel+" (ollama)"))
				return nil
			}
		}
	}

	fmt.Fprintln(os.Stdout, bold("No local embedder yet."))
	commands(os.Stdout, "Pick one:",
		"ollama pull "+defaultEmbedModel,
		"runhug connect hf",
		"runhug search -q \"…\" --keyword   # lexical-only",
	)
	if yes {
		return nil
	}
	if canPrompt() {
		ok, err := confirmPrefErr("Open Hugging Face token setup now (connect hf)?", false)
		if err != nil {
			return err
		}
		if ok {
			return cmdConnect([]string{"hf"})
		}
	}
	return nil
}

func initOfferIndex(yes bool) error {
	indexPath := indexFilePath()
	bundled := bundledIndexPath()
	hasLocal := fileExists(indexPath)
	hasBundled := fileExists(bundled)
	switch {
	case hasLocal:
		printKV(os.Stdout, "index", green("local")+"  "+dim(indexPath))
	case hasBundled:
		printKV(os.Stdout, "index", cyan("bundled")+"  "+dim(bundled))
	default:
		printKV(os.Stdout, "index", yellow("missing — install packs or run update"))
	}
	fmt.Fprintln(os.Stdout)

	ok := yes
	if !ok && canPrompt() {
		prompt := "Install category index packs from GitHub Releases?"
		def := !hasLocal
		var err error
		ok, err = confirmPrefErr(prompt, def)
		if err != nil {
			return err
		}
	}
	if !ok {
		fmt.Fprintln(os.Stdout, dim("Skip packs — run: runhug packs install  (or update)"))
		fmt.Fprintln(os.Stdout)
		return nil
	}

	ids, err := promptPackCategories(yes)
	if err != nil {
		return err
	}
	if len(ids) == 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	if err := installPackCategories(ctx, ids); err != nil {
		fmt.Fprintf(os.Stderr, "%s  Pack install failed: %v\n", yellow("⚠"), err)
		fmt.Fprintln(os.Stdout, dim("Falling back to Hub index refresh…"))
		return cmdUpdate(nil)
	}
	return nil
}

func fileExists(path string) bool {
	if path == "" {
		return false
	}
	_, err := os.Stat(path)
	return err == nil
}

func initFromSearch(eng runtime.Engine, query string, pick, limit int, yes bool) error {
	models, err := searchHub(query, "likes", "any", "", limit)
	if err != nil {
		return err
	}
	printHubResults(os.Stdout, hubView{
		Query:      query,
		Models:     models,
		Sort:       "likes",
		Limit:      limit,
		SkipFooter: true,
	})
	if len(models) == 0 {
		return fmt.Errorf("no Hub models matched %q", query)
	}
	n := pick
	if n == 0 && yes {
		n = 1
	}
	if n == 0 {
		if !canPrompt() {
			return fmt.Errorf("pass --pick 1..%d (example: --pick 1)", len(models))
		}
		line, err := readLine(fmt.Sprintf("Which row? [1-%d]  (or another query) ", len(models)))
		if err != nil {
			return err
		}
		p, q, repo := parseChoice(line, len(models))
		switch {
		case repo != "":
			return activateLocal(eng, specFromOverride(repo, eng.Kind))
		case q != "":
			return initFromSearch(eng, q, 0, limit, false)
		case p > 0:
			n = p
		default:
			return fmt.Errorf("expected 1..%d, a Hub id, or a search query", len(models))
		}
	}
	if n < 1 || n > len(models) {
		return fmt.Errorf("--pick must be 1..%d", len(models))
	}
	return activateLocal(eng, specFromHub(models[n-1], eng.Kind))
}
