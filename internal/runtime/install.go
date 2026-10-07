package runtime

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	goruntime "runtime"
	"strings"
)

type Plan struct {
	Kind     string
	Title    string
	Commands [][]string
	Manual   []string
}

func InstallPlan(kind string) Plan {
	kind = Normalize(kind)
	if kind == "" {
		kind = RecommendedKind()
	}
	switch kind {
	case Ollama:
		p := Plan{
			Kind:  Ollama,
			Title: "Ollama",
			Manual: []string{
				"macOS/Linux (Homebrew):  brew install ollama && ollama serve",
				"Linux script:            curl -fsSL https://ollama.com/install.sh | sh",
				"Windows / GUI:           https://ollama.com/download",
				"Then:                    ollama serve   # OpenAI API at " + OllamaURL,
			},
		}
		if hasBrew() {
			p.Commands = [][]string{{"brew", "install", "ollama"}}
		}
		return p
	case LlamaCPP:
		p := Plan{
			Kind:  LlamaCPP,
			Title: "llama.cpp (llama-server)",
			Manual: []string{
				"macOS/Linux (Homebrew):  brew install llama.cpp",
				"Or build:                https://github.com/ggml-org/llama.cpp",
				"Then set:                export LLAMA_SERVER=$(command -v llama-server)",
			},
		}
		if hasBrew() {
			p.Commands = [][]string{{"brew", "install", "llama.cpp"}}
		}
		return p
	case MLX:
		p := Plan{
			Kind:  MLX,
			Title: "MLX (Apple Silicon)",
			Manual: []string{
				"Apple Silicon only:      python3 -m pip install mlx-lm",
				"Or:                      uv pip install mlx-lm",
				"Then:                    mlx_lm.server --model mlx-community/Qwen2.5-1.5B-Instruct-4bit --port 8082",
				"OpenAI API:              " + MLXURL,
			},
		}
		if AppleSilicon() {
			p.Commands = [][]string{{"python3", "-m", "pip", "install", "--user", "mlx-lm"}}
		}
		return p
	default:
		return Plan{Kind: kind, Title: kind, Manual: []string{"unknown runtime " + kind}}
	}
}

func Install(kind string) error {
	p := InstallPlan(kind)
	if len(p.Commands) == 0 {
		return fmt.Errorf("no automatic installer for %s on this OS — use:\n%s", p.Title, strings.Join(p.Manual, "\n"))
	}
	for _, argv := range p.Commands {
		fmt.Fprintf(os.Stderr, "running  %s\n", strings.Join(argv, " "))
		cmd := exec.Command(argv[0], argv[1:]...)
		cmd.Stdout = os.Stderr
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("%s: %w", strings.Join(argv, " "), err)
		}
	}
	return nil
}

func PrintConfig(w io.Writer, s Snapshot) {
	fmt.Fprintln(w, "local runtimes")
	for _, e := range s.Engines {
		state := "not installed"
		if e.Present && e.Running {
			state = "up"
		} else if e.Present {
			state = "installed"
		}
		fmt.Fprintf(w, "  %-10s %-12s %s\n", e.Kind, state, dash(e.Binary))
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "search index")
	fmt.Fprintln(w, "  runhug packs install")
	fmt.Fprintln(w, "  runhug connect hf")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "search Hugging Face")
	fmt.Fprintln(w, "  runhug search qwen --sort likes")
	fmt.Fprintln(w, "  runhug inspect Qwen/Qwen2.5-7B-Instruct")
	fmt.Fprintln(w, "  runhug local add --pick 1")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Runpod")
	fmt.Fprintln(w, "  runhug connect")
	fmt.Fprintln(w, "  runhug deploy Qwen/Qwen2.5-7B-Instruct")
	fmt.Fprintln(w, "  runhug list")
	fmt.Fprintln(w, "  runhug proxy")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "already running a server")
	fmt.Fprintln(w, "  runhug local add --name ollama --url http://127.0.0.1:11434/v1")
	fmt.Fprintln(w, "  runhug local add --name mlx    --url http://127.0.0.1:8082/v1")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "override")
	fmt.Fprintln(w, "  export RVP_RUNTIME=ollama|llamacpp|mlx")
	fmt.Fprintln(w, "  export OLLAMA_BIN=/path/to/ollama")
	fmt.Fprintln(w, "  export LLAMA_SERVER=/path/to/llama-server")
	fmt.Fprintln(w, "  export MLX_SERVER=/path/to/mlx_lm.server")
	if goruntime.GOOS == "linux" {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "linux without Homebrew")
		fmt.Fprintln(w, "  curl -fsSL https://ollama.com/install.sh | sh")
	}
}

func ManualHelp() string {
	p := InstallPlan(RecommendedKind())
	return strings.Join(p.Manual, "\n")
}

func hasBrew() bool {
	_, err := exec.LookPath("brew")
	return err == nil
}

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
