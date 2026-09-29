package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/adamsiwiec1/runhug/internal/config"
	"github.com/adamsiwiec1/runhug/internal/find"
	"github.com/adamsiwiec1/runhug/internal/hf"
	"github.com/adamsiwiec1/runhug/internal/local"
	"github.com/adamsiwiec1/runhug/internal/runtime"
	"github.com/adamsiwiec1/runhug/internal/store"
)

func cmdLocal(args []string) error {
	if len(args) == 0 {
		return cmdLocalAdd(nil)
	}
	switch args[0] {
	case "add":
		return cmdLocalAdd(args[1:])
	case "start":
		return cmdLocalStart(args[1:])
	case "stop":
		return cmdLocalStop(args[1:])
	case "run":
		return cmdLocalRun(args[1:])
	case "setup", "doctor":
		return cmdLocalSetup(args[1:])
	default:
		return fmt.Errorf("unknown local command %q (setup, add, start, stop, run)", args[0])
	}
}

func cmdLocalAdd(args []string) error {
	fs := newFlagSet("local add")
	gguf := fs.String("gguf", "", "path to a .gguf file")
	name := fs.String("name", "", "registry name (default: file or Hub id)")
	base := fs.String("url", "", "already-running OpenAI base (e.g. http://127.0.0.1:11434/v1)")
	download := fs.Bool("download", false, "if the arg is a Hub repo, download a Q4 GGUF")
	listOnly := fs.Bool("list", false, "only print the scan (default when you pass no name)")
	pick := fs.Int("pick", 0, "search the local index for this 1-based local model")
	all := fs.Bool("all", false, "register every scan hit")
	register := fs.Bool("register", false, "put the pick in the registry instead of searching the Hub")
	limit := fs.Int("limit", 15, "max search rows after a pick (1-100)")
	sortKey := fs.String("sort", "relevance", "relevance (default), likes, or downloads")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	reg, _, err := store.Load()
	if err != nil {
		return err
	}

	if *base == "" && *gguf == "" && fs.NArg() == 0 {
		return registerFound(reg, find.Scan(), *pick, *all, *listOnly, *register, "", *name, hubOpts{
			Sort:    *sortKey,
			Limit:   *limit,
			Command: localPickCmd(*pick),
		})
	}

	m := store.Model{Backend: store.BackendLocal, CreatedAt: time.Now().UTC()}

	switch {
	case *base != "":
		m.BaseURL = strings.TrimRight(*base, "/")
		m.HFRepo = *name
		if m.HFRepo == "" {
			m.HFRepo = "local/openai"
		}
	case *gguf != "":
		abs, err := filepath.Abs(*gguf)
		if err != nil {
			return err
		}
		if _, err := os.Stat(abs); err != nil {
			return err
		}
		m.GGUFPath = abs
		m.HFRepo = *name
		if m.HFRepo == "" {
			m.HFRepo = strings.TrimSuffix(filepath.Base(abs), ".gguf")
		}
		m.Runtime = runtime.LlamaCPP
	case fs.NArg() >= 1:
		arg := fs.Arg(0)
		if looksLikeFile(arg) {
			abs, err := filepath.Abs(arg)
			if err != nil {
				return err
			}
			m.GGUFPath = abs
			m.HFRepo = *name
			if m.HFRepo == "" {
				m.HFRepo = strings.TrimSuffix(filepath.Base(abs), ".gguf")
			}
			m.Runtime = runtime.LlamaCPP
			break
		}
		if !strings.Contains(arg, "/") {
			hits := find.Filter(find.Scan(), arg)
			opts := hubOpts{Sort: *sortKey, Limit: *limit, Command: localPickCmd(*pick)}
			if *register || *all {
				return registerFound(reg, hits, *pick, *all, *listOnly, true, arg, *name, opts)
			}
			if *pick > 0 && len(hits) > 0 {
				return registerFound(reg, hits, *pick, *all, *listOnly, false, arg, *name, opts)
			}
			if opts.Command == "" {
				opts.Command = quotedCmd("search", hubQueryFromName(arg))
			}
			return searchAndPrint(hubQueryFromName(arg), opts)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		client := hf.New(config.Load().HFToken)
		model, err := client.Get(ctx, arg)
		if err != nil {
			if *register || *all {
				hits := find.Filter(find.Scan(), arg)
				if len(hits) > 0 {
					return registerFound(reg, hits, *pick, *all, *listOnly, true, arg, *name, hubOpts{Sort: *sortKey, Limit: *limit})
				}
			}
			return searchAndPrint(hubQueryFromName(arg), hubOpts{Sort: *sortKey, Limit: *limit})
		}
		if !*download && !*register {
			printHubResults(os.Stdout, hubView{Query: model.RepoID(), Models: []hf.Model{*model}, Pick: 1})
			return nil
		}
		filename, quant, ok := hf.PickGGUF(*model)
		if !ok {
			return fmt.Errorf("%s has no GGUF weights", arg)
		}
		m.HFRepo = model.RepoID()
		if *name != "" {
			m.HFRepo = *name
		}
		if *download {
			dir, err := hf.CacheDir()
			if err != nil {
				return err
			}
			dest := filepath.Join(dir, strings.ReplaceAll(model.RepoID(), "/", "--"), filepath.Base(filename))
			fmt.Fprintf(os.Stderr, "downloading %s (%s) → %s\n", filename, quant, dest)
			if err := client.Download(ctx, model.RepoID(), filename, dest); err != nil {
				return err
			}
			m.GGUFPath = dest
			m.Runtime = runtime.LlamaCPP
		}
	}

	reg.Put(m)
	reg.Current = m.HFRepo
	if err := reg.Save(); err != nil {
		return err
	}
	printReady(m.HFRepo, m.Runtime, m.BaseURL)
	return nil
}

func localPickCmd(pick int) string {
	if pick < 1 {
		return ""
	}
	return fmt.Sprintf("runhug local add --pick %d", pick)
}

func registerFound(reg *store.Registry, hits []find.Found, pick int, all, listOnly, doRegister bool, query, name string, opts hubOpts) error {
	w := os.Stdout
	if len(hits) == 0 {
		heading(w, "No local models yet")
		fmt.Fprintln(w, "Looked in ~/models, ~/gguf, ~/.ollama/models,")
		fmt.Fprintln(w, "Hugging Face Hub ($HF_HOME, ~/.cache/huggingface/hub), LM Studio, $RVP_CACHE, and $RVP_MODELS.")
		fmt.Fprintln(w)
		if query != "" && !doRegister && !all {
			return searchAndPrint(hubQueryFromName(query), opts)
		}
		commands(w, "Search Hugging Face anyway:",
			`runhug search instruct --sort likes --limit 20`,
			`runhug search "coding assistant" --limit 20`,
		)
		return nil
	}

	heading(w, fmt.Sprintf("Local models  (%d)", len(hits)))
	printFoundTable(w, hits)

	if pick > 0 {
		if pick < 1 || pick > len(hits) {
			return fmt.Errorf("--pick must be 1..%d  (example: runhug local add --pick %d)", len(hits), examplePick(hits))
		}
		if !doRegister && !all {
			if opts.Command == "" {
				opts.Command = localPickCmd(pick)
			}
			// Lexical Hub/index search only — semantic embed would wake Ollama
			// and dump GIN / llama-server load logs into this TTY.
			opts.DisableSemantic = true
			return searchAndPrint(hubQueryFromFound(hits[pick-1]), opts)
		}
		hits = hits[pick-1 : pick]
	} else if query != "" && !doRegister && !all && !listOnly {
		return searchAndPrint(hubQueryFromName(query), opts)
	}

	shouldAdd := !listOnly && (all || doRegister)
	if !shouldAdd {
		printAddHelp(w, hits)
		return nil
	}
	var last store.Model
	for _, h := range hits {
		label := ""
		if name != "" && len(hits) == 1 {
			label = name
		}
		last = modelFromFound(h, label)
		reg.Put(last)
	}
	reg.Current = last.HFRepo
	if err := reg.Save(); err != nil {
		return err
	}
	printReady(last.HFRepo, last.Runtime, last.BaseURL)
	return nil
}

func modelFromFound(h find.Found, name string) store.Model {
	m := store.Model{Backend: store.BackendLocal, CreatedAt: time.Now().UTC()}
	switch h.Kind {
	case "ollama":
		m.Runtime = runtime.Ollama
		m.ServeName = h.Name
		m.BaseURL = runtime.OllamaURL
		m.HFRepo = h.Name
	default:
		m.Runtime = runtime.LlamaCPP
		m.GGUFPath = h.Path
		m.HFRepo = strings.TrimSuffix(filepath.Base(h.Path), ".gguf")
	}
	if name != "" {
		m.HFRepo = name
	}
	return m
}

func looksLikeFile(p string) bool {
	if strings.HasSuffix(strings.ToLower(p), ".gguf") {
		return true
	}
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

func formatSize(n int64) string {
	if n <= 0 {
		return ""
	}
	const mb = 1024 * 1024
	if n >= 1024*mb {
		return fmt.Sprintf("%.1fG", float64(n)/(1024*mb))
	}
	return fmt.Sprintf("%.0fM", float64(n)/mb)
}

func cmdLocalStart(args []string) error {
	fs := newFlagSet("local start")
	model := fs.String("model", "", "registry model (default: current)")
	port := fs.Int("port", 8081, "llama-server port")
	ctxSize := fs.Int("ctx", 4096, "context tokens (llama-server -c)")
	threads := fs.Int("threads", 0, "CPU threads (0 = all)")
	bin := fs.String("bin", "", "llama-server binary")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	reg, _, err := store.Load()
	if err != nil {
		return err
	}
	m, ok := reg.Lookup(*model)
	if !ok {
		return fmt.Errorf("unknown model; `local add` or `init` first")
	}
	switch m.Runtime {
	case runtime.Ollama:
		if err := runtime.EnsureOllama(""); err != nil {
			return err
		}
		if m.ServeName != "" {
			if err := runtime.PullOllama("", m.ServeName); err != nil {
				return err
			}
		}
		m.BaseURL = runtime.OllamaURL
	case runtime.MLX:
		name := m.ServeName
		if name == "" {
			name = runtime.MapMLX(m.HFRepo)
		}
		cmd, err := runtime.StartMLX(name, runtime.MLXPort)
		if err != nil {
			return err
		}
		m.LocalPID = cmd.Process.Pid
		m.BaseURL = runtime.MLXURL
		m.ServeName = name
	default:
		if m.GGUFPath == "" {
			return fmt.Errorf("%s has no GGUF path (use --url models as-is, or add --gguf)", m.HFRepo)
		}
		cmd, err := local.Start(*bin, m.GGUFPath, m.HFRepo, *port, *ctxSize, *threads)
		if err != nil {
			return err
		}
		m.LocalPID = cmd.Process.Pid
		m.BaseURL = local.DefaultURL(*port)
	}
	m.Backend = store.BackendLocal
	reg.Put(m)
	reg.Current = m.HFRepo
	if err := reg.Save(); err != nil {
		return err
	}
	fmt.Fprintln(os.Stdout)
	fmt.Fprintf(os.Stdout, "%s  %s\n", green("Ready"), bold(m.HFRepo))
	printKV(os.Stdout, "runtime", dash(m.Runtime))
	printKV(os.Stdout, "openai", cyan(m.BaseURL))
	fmt.Fprintln(os.Stdout)
	commands(os.Stdout, "Next:",
		"runhug proxy",
		"runhug search qwen --sort likes",
		"runhug connect",
	)
	return nil
}

func cmdLocalStop(args []string) error {
	fs := newFlagSet("local stop")
	model := fs.String("model", "", "registry model (default: current)")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	reg, _, err := store.Load()
	if err != nil {
		return err
	}
	m, ok := reg.Lookup(*model)
	if !ok || m.LocalPID == 0 {
		return fmt.Errorf("no running llama-server pid in the registry")
	}
	proc, err := os.FindProcess(m.LocalPID)
	if err == nil {
		_ = proc.Kill()
	}
	m.LocalPID = 0
	reg.Put(m)
	if err := reg.Save(); err != nil {
		return err
	}
	fmt.Printf("stopped pid for %s\n", m.HFRepo)
	return nil
}

// cmdLocalRun ensures the registry model is serving, then opens the chat REPL
// (same path as `runhug run`).
func cmdLocalRun(args []string) error {
	fs := newFlagSet("local run")
	model := fs.String("model", "", "registry model (default: current / positional)")
	port := fs.Int("port", 8081, "llama-server port when starting")
	ctxSize := fs.Int("ctx", 4096, "context tokens (llama-server -c)")
	threads := fs.Int("threads", 0, "CPU threads (0 = all)")
	bin := fs.String("bin", "", "llama-server binary")
	oneshot := fs.String("q", "", "one-shot prompt then exit")
	stream := fs.Bool("stream", true, "stream chat completions")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	key := strings.TrimSpace(*model)
	if key == "" && fs.NArg() > 0 {
		key = fs.Arg(0)
	}
	reg, _, err := store.Load()
	if err != nil {
		return err
	}
	m, ok := reg.Lookup(key)
	if !ok {
		return fmt.Errorf("unknown local model %q — `runhug local add` first", key)
	}
	needStart := m.BaseURL == "" || (m.Runtime != runtime.Ollama && m.Runtime != runtime.MLX && m.LocalPID == 0 && m.GGUFPath != "")
	if needStart {
		startArgs := []string{"--model", m.HFRepo, "--port", strconv.Itoa(*port), "--ctx", strconv.Itoa(*ctxSize)}
		if *threads > 0 {
			startArgs = append(startArgs, "--threads", strconv.Itoa(*threads))
		}
		if *bin != "" {
			startArgs = append(startArgs, "--bin", *bin)
		}
		if err := cmdLocalStart(startArgs); err != nil {
			return err
		}
	}
	runArgs := []string{"--yes", m.HFRepo}
	if q := strings.TrimSpace(*oneshot); q != "" {
		runArgs = append(runArgs, "-q", q)
	}
	if !*stream {
		runArgs = append(runArgs, "--stream=false")
	}
	return cmdRun(runArgs)
}
