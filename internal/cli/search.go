package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/adamsiwiec1/runhug/internal/config"
	"github.com/adamsiwiec1/runhug/internal/family"
	"github.com/adamsiwiec1/runhug/internal/hf"
	"github.com/adamsiwiec1/runhug/internal/hparams"
	"github.com/adamsiwiec1/runhug/internal/recommend"
	"github.com/adamsiwiec1/runhug/internal/runtime"
)

type searchFlagVals struct {
	queryFlag               string
	author, task, library   *string
	typeFlag                *string
	filter, license, engine *string
	sort                    *string
	limit                   *int
	semanticOn, noSemantic  *bool
	keyword, online, hub    *bool
	verbose                 *bool
	wrap, wordWrap, ww      *int
}

func registerSearchFlags(fs *flag.FlagSet) *searchFlagVals {
	sf := &searchFlagVals{}
	fs.StringVar(&sf.queryFlag, "query", "", "search query (id, tags, card description; same as positional; wins if both set)")
	fs.StringVar(&sf.queryFlag, "q", "", "search query (same as --query)")
	sf.author = fs.String("author", "", "filter by Hugging Face org or user")
	sf.task = fs.String("task", "auto", "exact Hub pipeline_tag: auto (detect), any, text-generation, … (narrow; use --type for broad buckets)")
	sf.typeFlag = fs.String("type", "", "broad pack type: llm, vision, image, video, audio, embeddings, gguf, … (see: runhug packs list)")
	sf.library = fs.String("library", "", "library filter (transformers, …)")
	sf.filter = fs.String("filter", "", "extra tag filter (safetensors, gguf, …)")
	sf.license = fs.String("license", "", "license filter (apache-2.0, mit, gemma, other, …)")
	sf.engine = fs.String("engine", "", "engine filter (vllm, gguf, …)")
	sf.sort = fs.String("sort", "relevance", "relevance (default; embedding rerank when available), likes, or downloads — sorts the local index pool (or Hub pool with --online)")
	sf.limit = fs.Int("limit", 15, "rows to show (1-100)")
	sf.semanticOn = fs.Bool("semantic", true, "rerank with embeddings when HF_TOKEN is set (HF Inference); local Ollama needs RUNHUG_OLLAMA_EMBED=1")
	sf.noSemantic = fs.Bool("no-semantic", false, "disable embedding rerank (lexical only)")
	sf.keyword = fs.Bool("keyword", false, "alias for --no-semantic (lexical-only)")
	sf.online = fs.Bool("online", false, "live Hugging Face Hub search instead of the local SQLite index (rate-limited; set HF_TOKEN)")
	sf.hub = fs.Bool("hub", false, "alias for --online")
	sf.verbose = fs.Bool("verbose", false, "show rank notes, source line, and ACTIONS legend (or set RUNHUG_VERBOSE=1)")
	fs.BoolVar(sf.verbose, "v", false, "alias for --verbose")
	sf.wrap, sf.wordWrap, sf.ww = addWrapFlags(fs)
	return sf
}

func (sf *searchFlagVals) request(query string) searchRequest {
	return searchRequest{
		Query:           query,
		Author:          *sf.author,
		Task:            hf.ResolveTask(*sf.task, query),
		Type:            strings.TrimSpace(*sf.typeFlag),
		Library:         *sf.library,
		Filter:          *sf.filter,
		License:         *sf.license,
		Engine:          *sf.engine,
		Sort:            *sf.sort,
		Limit:           clampLimit(*sf.limit),
		DisableSemantic: !*sf.semanticOn || *sf.noSemantic || *sf.keyword,
		Online:          *sf.online || *sf.hub,
	}
}

func cmdSearch(args []string) error {
	if showCmdHelp(args, "runhug search", printSearchHelp, printSearchHelpFull) {
		return nil
	}
	fs := newFlagSet("search")
	sf := registerSearchFlags(fs)
	asJSON := fs.Bool("json", false, "print JSON")
	copyIdx := fs.Int("copy", 0, "copy MODEL id for this 1-based row to the clipboard")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	query := resolveSearchQuery(sf.queryFlag, strings.Join(fs.Args(), " "))
	if strings.TrimSpace(query) == "" && len(args) == 0 {
		printSearchHelp(os.Stdout)
		return nil
	}
	req := sf.request(query)

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	models, meta, err := searchModels(ctx, req)
	if err != nil {
		return err
	}
	if *asJSON {
		return writeJSON(models)
	}
	printHubResults(os.Stdout, hubView{
		Query:      query,
		Models:     models,
		Sort:       req.Sort,
		Limit:      req.Limit,
		Command:    quotedCmd("search", query),
		RankSource: meta.RankSource,
		Queries:    meta.Queries,
		WrapWidth:  resolveWrapWidth(*sf.wrap, *sf.wordWrap, *sf.ww),
		Verbose:    *sf.verbose || runtime.Verbose(),
	})
	if *copyIdx > 0 {
		if *copyIdx > len(models) {
			return fmt.Errorf("--copy %d out of range (1-%d)", *copyIdx, len(models))
		}
		id := models[*copyIdx-1].RepoID()
		if err := copyToClipboard(id); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "%s  %s\n", green("copied"), id)
	}
	return nil
}

func searchAndPrint(query string, opts hubOpts) error {
	query = strings.TrimSpace(query)
	if query == "" {
		return fmt.Errorf("usage: runhug search <query>")
	}
	if opts.Sort == "" {
		opts.Sort = "relevance"
	}
	opts.Limit = clampLimit(opts.Limit)
	if opts.Command == "" {
		opts.Command = quotedCmd("search", query)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	models, meta, err := searchModels(ctx, searchRequest{
		Query:           query,
		Task:            "any",
		Sort:            opts.Sort,
		Limit:           opts.Limit,
		DisableSemantic: opts.DisableSemantic,
	})
	if err != nil {
		return err
	}
	printHubResults(os.Stdout, hubView{
		Query:      query,
		Models:     models,
		Sort:       opts.Sort,
		Limit:      opts.Limit,
		Command:    opts.Command,
		RankSource: meta.RankSource,
		Queries:    meta.Queries,
		WrapWidth:  opts.WrapWidth,
		Verbose:    opts.Verbose || runtime.Verbose(),
	})
	return nil
}

func addWrapFlags(fs *flag.FlagSet) (wrap, wordWrap, ww *int) {
	wrap = fs.Int("wrap", 0, "wrap MODEL names at N runes (0=ellipsis truncate; try 28)")
	wordWrap = fs.Int("word-wrap", 0, "alias for --wrap")
	ww = fs.Int("ww", 0, "alias for --wrap (e.g. -ww 28)")
	return wrap, wordWrap, ww
}

func searchHub(query, sort, task, filter string, limit int) ([]hf.Model, error) {
	if sort == "" {
		sort = "relevance"
	}
	if task == "" {
		task = "any"
	}
	if limit <= 0 {
		limit = 10
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	models, _, err := searchModels(ctx, searchRequest{
		Query:  query,
		Task:   task,
		Filter: filter,
		Sort:   sort,
		Limit:  limit,
	})
	return models, err
}

// resolveSearchQuery prefers --query/-q when set, otherwise the positional words.
func resolveSearchQuery(flagQuery, positional string) string {
	flagQuery = strings.TrimSpace(flagQuery)
	positional = strings.TrimSpace(positional)
	if flagQuery != "" {
		return flagQuery
	}
	return positional
}

func formatCount(n int64) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	case n >= 1_000:
		return fmt.Sprintf("%.1fK", float64(n)/1_000)
	default:
		return fmt.Sprintf("%d", n)
	}
}

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func yn(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

func cmdInspect(args []string) error {
	if len(args) == 0 {
		printInspectHelp(os.Stdout)
		return nil
	}
	if showCmdHelp(args, "runhug inspect", printInspectHelp, printInspectHelpFull) {
		return nil
	}
	fs := newFlagSet("inspect")
	maxLen := fs.Int("max-len", 8192, "context length used for the VRAM estimate")
	gpu := fs.String("gpu", "", "force this GPU pool in the recommendation")
	asJSON := fs.Bool("json", false, "print JSON")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	if fs.NArg() < 1 {
		printInspectHelp(os.Stderr)
		return fmt.Errorf("model required")
	}
	modelID := fs.Arg(0)

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	env := config.Load()
	client := hf.New(env.HFToken)
	model, err := client.Get(ctx, modelID)
	if err != nil {
		return err
	}
	format := hf.DetectFormat(*model)
	est := inspectEstimate(*model, format, *maxLen)
	alts := recommend.SuggestAlternatives(ctx, client, *model, format, est, 3)

	var plan any
	var recText string
	if env.RunpodAPIKey != "" {
		recText, plan, err = inspectGPU(ctx, env.RunpodAPIKey, est.RequiredGB, *gpu)
		if err != nil {
			recText = "GPU catalog: " + err.Error()
		}
	} else {
		recText = "run `runhug connect` to pick a live serverless GPU pool"
	}

	if *asJSON {
		return writeJSON(map[string]any{
			"model":        model,
			"format":       format,
			"estimate":     est,
			"gpu":          plan,
			"alternatives": alts,
		})
	}

	heading(os.Stdout, "Inspect")
	fmt.Fprintf(os.Stdout, "%s  %s\n", dim("Model"), bold(model.RepoID()))
	printKV(os.Stdout, "url", cyan(hubLink(model.RepoID())))
	printKV(os.Stdout, "task", dash(model.PipelineTag))
	printKV(os.Stdout, "library", dash(model.LibraryName))
	printKV(os.Stdout, "license", dash(model.License()))
	printKV(os.Stdout, "gated", yn(model.IsGated()))
	printKV(os.Stdout, "downloads", fmt.Sprintf("%s    likes %d", formatCount(model.Downloads), model.Likes))
	fmt.Fprintf(os.Stdout, "  %s  %s", dim(padRight("format", 9)), format.Engine)
	if format.Quant != "" {
		fmt.Fprintf(os.Stdout, "  quantization=%s", format.Quant)
	}
	fmt.Println()
	printKV(os.Stdout, "params", fmt.Sprintf("%s  (%s)", est.ParamsLabel(), dash(est.ParamsSource)))
	printKV(os.Stdout, "precision", fmt.Sprintf("%s  (%.2g bytes/param)", est.Precision, est.BytesPerParam))
	if est.WeightGB > 0 {
		printKV(os.Stdout, "weights", fmt.Sprintf("%.1f GB", est.WeightGB))
		printKV(os.Stdout, "vram est", fmt.Sprintf("%.1f GB  (×%.2f overhead, max_len=%d)", est.RequiredGB, est.OverheadFactor, *maxLen))
	} else {
		printKV(os.Stdout, "vram est", "unknown")
	}
	printKV(os.Stdout, "disk", fmt.Sprintf("%d GB container (ephemeral)", est.DiskGB))
	if format.Engine == hf.EngineGGUF {
		printKV(os.Stdout, "engine", yellow("GGUF — do not deploy on worker-vllm; use a llama.cpp worker"))
	} else {
		printKV(os.Stdout, "engine", green("vLLM"))
	}
	printKV(os.Stdout, "gpu", recText)
	if extras := family.EnvFor(model.RepoID()); len(extras) > 0 {
		var bits []string
		for k, v := range extras {
			bits = append(bits, k+"="+v)
		}
		printKV(os.Stdout, "family", strings.Join(bits, "  "))
	}
	rec := hparams.Recommend(*model, format, nil)
	if !rec.Empty() {
		printKV(os.Stdout, "sampling", rec.FormatBlock())
	}
	for _, n := range est.Notes {
		fmt.Fprintf(os.Stdout, "  %s  %s\n", yellow(padRight("note", 9)), n)
	}
	printInspectAlternatives(os.Stdout, alts)
	fmt.Fprintln(os.Stdout)
	next := []string{
		"runhug deploy " + model.RepoID(),
		"runhug connect",
	}
	if format.Engine == hf.EngineGGUF {
		next = []string{
			"runhug init --model " + model.RepoID(),
			"runhug search " + model.RepoID() + " --sort likes",
		}
	}
	if len(alts) > 0 {
		top := alts[0].RepoID
		next = append([]string{
			"runhug inspect " + top,
			"runhug deploy " + top,
		}, next...)
	}
	commands(os.Stdout, "Next:", next...)
	return nil
}

func printInspectAlternatives(w io.Writer, alts []recommend.Alternative) {
	if len(alts) == 0 {
		return
	}
	fmt.Fprintln(w)
	fmt.Fprintf(w, "%s  %s\n", boldYellow("Alternatives"), dim("(smaller / cheaper fits)"))
	for i, a := range alts {
		quant := a.Quant
		if quant == "" {
			quant = "—"
		} else {
			quant = strings.ToUpper(quant)
		}
		est := "—"
		if a.RequiredGB > 0 {
			est = fmt.Sprintf("~%.0f GB est", a.RequiredGB)
		}
		why := a.Why
		if a.GGUFFile != "" {
			why = "file=" + a.GGUFFile
		}
		fmt.Fprintf(w, "  %s  %s  %s  %s  %s\n",
			cyan(fmt.Sprintf("%d", i+1)),
			bold(padRight(a.RepoID, 32)),
			yellow(padRight(quant, 8)),
			dim(padRight(est, 12)),
			dim(a.Engine+"  "+why),
		)
	}
}

func printInspectHelp(w io.Writer) {
	helpUsage(w, "runhug inspect <org/model>")
	fmt.Fprintln(w, dim("Hub card · VRAM estimate · cheaper / lower-quant alternatives"))
	fmt.Fprintln(w)

	helpSection(w, "shows")
	helpCmd(w, "format / params", "engine, precision, weight + VRAM estimate")
	helpCmd(w, "gpu", "suggested serverless pool (needs runhug connect)")
	helpCmd(w, "alternatives", "up to 3 smaller / cheaper fits (GGUF quants + related Hub)")
	fmt.Fprintln(w)

	helpSection(w, "flags")
	helpFlag(w, "--max-len", "context length for the VRAM estimate (default 8192)")
	helpFlag(w, "--gpu", "force this GPU pool in the recommendation")
	helpFlag(w, "--json", "print JSON (includes alternatives)")
	fmt.Fprintln(w)

	fmt.Fprintf(w, "%s %s\n", dim("example:"), cyan("runhug inspect Qwen/Qwen3.8-27B"))
}

func printSearchHelp(w io.Writer) {
	helpUsage(w, "runhug search [query]")
	fmt.Fprintln(w, dim("Local index by default · --online for live Hub"))
	fmt.Fprintln(w, dim("--task = exact Hub pipeline_tag · --type = broad pack bucket (llm, vision, …)"))
	fmt.Fprintln(w)

	helpSection(w, "flags")
	helpFlag(w, "--query -q", "search query (same as positional)")
	helpFlag(w, "--online --hub", "live Hugging Face Hub search")
	helpFlag(w, "--type", "broad type: llm, vision, image, video, audio, embeddings, gguf, …")
	helpFlag(w, "--task", "exact pipeline_tag (auto/any/text-generation/…)")
	helpFlag(w, "--sort", "relevance (default), likes, or downloads")
	helpFlag(w, "--limit", "rows to show (1-100)")
	helpFlag(w, "--author --engine", "filter by author / engine")
	helpFlag(w, "--verbose -v", "rank notes, source, ACTIONS legend")
	helpFlag(w, "--json --copy N", "JSON output / copy row N to clipboard")
	fmt.Fprintln(w)

	fmt.Fprintf(w, "%s %s\n", dim("example:"), cyan(`runhug search aero --type llm`))
	fmt.Fprintf(w, "%s %s\n", dim("example:"), cyan(`runhug search qwen --online --engine vllm`))
}
