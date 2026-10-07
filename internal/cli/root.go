package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/adamsiwiec1/runhug/internal/version"
)

func Run(args []string) error {
	if len(args) == 0 {
		printUsage(os.Stdout)
		return flag.ErrHelp
	}
	cmd, rest := args[0], args[1:]
	switch cmd {
	case "wizard", "guide", "guided", "setup":
		return cmdWizard(rest)
	case "init":
		return fmt.Errorf("removed — use `runhug packs install` for the search index, `runhug local add` for a local model, or `runhug deploy` / `runhug recommend gpu`")
	case "search":
		return cmdSearch(rest)
	case "packs":
		return cmdPacks(rest)
	case "recommend":
		return cmdRecommend(rest)
	case "inspect":
		return cmdInspect(rest)
	case "connect":
		return cmdConnect(rest)
	case "disconnect":
		return cmdDisconnect(rest)
	case "login":
		return cmdLogin(rest)
	case "hf":
		return cmdHF(rest)
	case "config":
		return cmdConfig(rest)
	case "update":
		return cmdUpdate(rest)
	case "upgrade":
		return cmdUpgrade(rest)
	case "index-setup", "index-update":
		fmt.Fprintln(os.Stderr, dim("Hub crawl moved to hfpacks. Install packs with:"))
		fmt.Fprintln(os.Stderr, "  "+cyan("runhug packs install"))
		fmt.Fprintln(os.Stderr, "  "+cyan("runhug update --packs"))
		fmt.Fprintln(os.Stderr, dim("Producer: https://github.com/openhat-security/hfpacks"))
		return fmt.Errorf("%s is retired — use runhug packs install / update --packs", cmd)
	case "index-info", "index":
		return cmdIndexInfo(rest)
	case "deploy":
		return cmdDeploy(rest)
	case "gcp":
		return cmdGCP(rest)
	case "heretic":
		return cmdHeretic(rest)
	case "local":
		return cmdLocal(rest)
	case "list", "deployments":
		return cmdList(rest)
	case "cost", "costs", "billing":
		return cmdCost(rest)
	case "use":
		return cmdUse(rest)
	case "url":
		return cmdURL(rest)
	case "proxy", "serve":
		return cmdProxy(rest)
	case "run":
		return cmdRun(rest)
	case "metrics":
		return cmdMetrics(rest)
	case "start":
		return cmdStart(rest)
	case "delete":
		return cmdDelete(rest)
	case "status":
		return cmdStatus(rest)
	case "gpu":
		return cmdGPU(rest)
	case "gpus":
		return cmdGPUList(rest)
	case "import":
		return cmdImport(rest)
	case "version", "-v", "--version":
		fmt.Printf("%s %s\n", version.Name, version.Version)
		return nil
	case "help", "-h", "--help":
		if wantsFullHelp(rest) {
			printUsageFull(os.Stdout)
			return nil
		}
		printUsage(os.Stdout, true)
		return nil
	case "help-full", "--help-full":
		printUsageFull(os.Stdout)
		return nil
	default:
		return fmt.Errorf("unknown command %q\n\nRun `%s --help`", cmd, version.Name)
	}
}

func wantsFullHelp(args []string) bool {
	for _, a := range args {
		switch strings.ToLower(strings.TrimSpace(a)) {
		case "full", "--full", "-full", "--help-full", "help-full":
			return true
		}
	}
	return false
}

func printUsage(w io.Writer, withDetails ...bool) {
	printBanner(w)
	printTagline(w)

	helpUsage(w, "runhug <command> [flags]")

	helpSection(w, "setup")
	helpCmd(w, "wizard", "guided first-run setup")
	helpCmd(w, "packs", "install / list / update index packs")
	helpCmd(w, "gpu", "find + set gpu catalog & preference")
	helpCmd(w, "config", "settings · connect / disconnect credentials")
	fmt.Fprintln(w)

	helpSection(w, "search")
	helpCmd(w, "search [query]", "local index (add --online for Hub)")
	helpCmd(w, "recommend [query]", "shortlist models")
	helpCmd(w, "recommend gpu <model>", "GPU / VRAM for a model")
	helpCmd(w, "inspect <model>", "card, VRAM, cheaper options")
	helpCmd(w, "update", "refresh local model index")
	helpCmd(w, "upgrade", "upgrade this CLI")
	fmt.Fprintln(w)

	helpSection(w, "deploy")
	helpCmd(w, "deploy <model>", "RunPod serverless (vLLM)")
	helpCmd(w, "gcp", "GCP Spot GPUs")
	helpCmd(w, "list", "show deployments")
	helpCmd(w, "cost", "instance up/down + GPU $/hr totals")
	fmt.Fprintln(w)

	helpSection(w, "heretic")
	helpCmd(w, "heretic wizard", "guided abliteration")
	helpCmd(w, "heretic make <model>", "abliteration training")
	fmt.Fprintln(w)

	helpSection(w, "run")
	helpCmd(w, "run [model]", "chat REPL")
	helpCmd(w, "metrics [model]", "live GPU / CPU / RAM dashboard")
	helpCmd(w, "start claude|opencode", "point an agent at a deployment")
	helpCmd(w, "local", "run models on this machine")
	helpCmd(w, "proxy", "OpenAI proxy on :8080")
	fmt.Fprintln(w)

	fmt.Fprintf(w, "%s %s\n", dim("new here?"), cyan("runhug wizard"))
	if len(withDetails) > 0 && withDetails[0] {
		printHelpDetailsHint(w, "runhug")
	}
}

func newFlagSet(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	return fs
}

func isHelpArg(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "-h", "--help", "help":
		return true
	default:
		return false
	}
}

func parseFlags(fs *flag.FlagSet, args []string) error {
	bools := map[string]bool{}
	fs.VisitAll(func(f *flag.Flag) {
		type boolFlag interface{ IsBoolFlag() bool }
		if v, ok := f.Value.(boolFlag); ok && v.IsBoolFlag() {
			bools[f.Name] = true
		}
	})
	var flags, pos []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			pos = append(pos, args[i+1:]...)
			break
		}
		if strings.HasPrefix(a, "-") && a != "-" {
			flags = append(flags, a)
			name := strings.TrimLeft(a, "-")
			if strings.Contains(name, "=") || bools[name] {
				continue
			}
			if i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				i++
				flags = append(flags, args[i])
			}
			continue
		}
		pos = append(pos, a)
	}
	return fs.Parse(append(flags, pos...))
}

func writeJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func confirm(prompt string) bool {
	fmt.Fprint(os.Stderr, prompt+" [y/N] ")
	var s string
	_, _ = fmt.Fscanln(os.Stdin, &s)
	s = strings.TrimSpace(strings.ToLower(s))
	return s == "y" || s == "yes"
}

func parseKV(pairs []string) (map[string]string, error) {
	out := map[string]string{}
	for _, p := range pairs {
		k, v, ok := strings.Cut(p, "=")
		if !ok || strings.TrimSpace(k) == "" {
			return nil, fmt.Errorf("expected KEY=VALUE, got %q", p)
		}
		out[strings.TrimSpace(k)] = v
	}
	return out, nil
}

type stringsFlag []string

func (s *stringsFlag) String() string { return strings.Join(*s, ",") }
func (s *stringsFlag) Set(v string) error {
	*s = append(*s, v)
	return nil
}
