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
		return cmdInit(rest)
	case "search":
		return cmdSearch(rest)
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
	case "index-setup":
		return cmdIndexSetup(rest)
	case "index-update":
		return cmdIndexUpdate(rest)
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
	case "use":
		return cmdUse(rest)
	case "url":
		return cmdURL(rest)
	case "proxy", "serve":
		return cmdProxy(rest)
	case "run":
		return cmdRun(rest)
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
		printUsage(os.Stdout)
		return nil
	default:
		return fmt.Errorf("unknown command %q\n\nRun `%s help` for usage", cmd, version.Name)
	}
}

func printUsage(w io.Writer) {
	printBanner(w)
	printTagline(w)
	fmt.Fprintln(w, "usage:")
	fmt.Fprintln(w, "  runhug <command> [flags]")
	fmt.Fprintln(w)
	fmt.Fprintln(w, bold("setup"))
	fmt.Fprintln(w, "  wizard             guided setup")
	fmt.Fprintln(w, "  init               search nlp + index packs")
	fmt.Fprintln(w, "  connect            save runpod api key")
	fmt.Fprintln(w, "  connect hf         save hugging face token")
	fmt.Fprintln(w, "  disconnect [hf]    forget stored key or token")
	fmt.Fprintln(w)
	fmt.Fprintln(w, bold("search & index"))
	fmt.Fprintln(w, "  search [query]     local index; --online for hub")
	fmt.Fprintln(w, "  recommend [query]  shortlist + optional advisor")
	fmt.Fprintln(w, "  recommend gpu <m>  gpu / vram for one model")
	fmt.Fprintln(w, "  inspect <model>    hub card + vram estimate")
	fmt.Fprintln(w, "  update             refresh index")
	fmt.Fprintln(w, "  upgrade            update this CLI")
	fmt.Fprintln(w)
	fmt.Fprintln(w, bold("deploy"))
	fmt.Fprintln(w, "  deploy <model>     serverless vllm (runpod, default)")
	fmt.Fprintln(w, "  deploy --provider gcp   Spot L4/T4 llama.cpp (or: gcp deploy)")
	fmt.Fprintln(w, "  gcp                GCP GPU provider (deploy|tunnel|status|…)")
	fmt.Fprintln(w, "  heretic make <m>   abliteration training pod + dashboard")
	fmt.Fprintln(w, "  list               local registry + providers")
	fmt.Fprintln(w, "  proxy              openai proxy :8080/v1")
	fmt.Fprintln(w)
	fmt.Fprintln(w, bold("chat & agents"))
	fmt.Fprintln(w, "  run [model]        chat repl")
	fmt.Fprintln(w, "  start claude       wire Claude Code through the bridge")
	fmt.Fprintln(w, "  start opencode     wire opencode to the proxy/deployment")
	fmt.Fprintln(w)
	fmt.Fprintln(w, bold("local"))
	fmt.Fprintln(w, "  local add|start|stop|run|setup   models on this machine")
	fmt.Fprintln(w)
	fmt.Fprintln(w, bold("gpu"))
	fmt.Fprintln(w, "  gpu list           hardware index + runpod/gcp/local")
	fmt.Fprintln(w, "  gpu set|clear|show preference for deploy / local")
	fmt.Fprintln(w, "  gpu update         refresh NVIDIA + GCP catalogs")
	fmt.Fprintln(w, "  gpus               alias for gpu list")
	fmt.Fprintln(w)
	fmt.Fprintln(w, bold("config"))
	fmt.Fprintln(w, "  config             config dir + settings")
	fmt.Fprintln(w, "  config get|set     no_color, update_limit, advisor_*")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "wizard")
}

func newFlagSet(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	return fs
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
