package cli

import (
	"fmt"
	"os"

	"github.com/adamsiwiec1/runhug/internal/hostgpu"
	"github.com/adamsiwiec1/runhug/internal/runtime"
)

func cmdLocalSetup(args []string) error {
	fs := newFlagSet("local setup")
	want := fs.String("runtime", "", "ollama, llamacpp, or mlx")
	yes := fs.Bool("yes", false, "install the recommended runtime without a prompt")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	eng, err := resolveEngine(*want, *yes, true)
	if err != nil {
		runtime.PrintConfig(os.Stderr, runtime.Detect())
		printLocalGPUs(os.Stderr)
		return err
	}
	fmt.Printf("%s  %s  %s\n", dim("using"), eng.Kind, dim(dash(eng.Binary)))
	if eng.Running {
		fmt.Printf("%s  %s  %s\n", dim("openai"), cyan(eng.BaseURL), green("(already up)"))
	} else {
		fmt.Printf("%s  %s  %s\n", dim("openai"), cyan(eng.BaseURL), dim("(will start on first use)"))
	}
	fmt.Fprintln(os.Stderr)
	runtime.PrintConfig(os.Stderr, runtime.Detect())
	printLocalGPUs(os.Stderr)
	return nil
}

func printLocalGPUs(w *os.File) {
	devs, err := hostgpu.Detect()
	if err != nil || len(devs) == 0 {
		fmt.Fprintln(w, dim("gpu        (none detected — runhug gpu list --filter local)"))
		return
	}
	for _, d := range devs {
		fmt.Fprintf(w, "%s  %s  ~%.0f GB (%s)\n", dim("gpu"), d.Name, d.MemoryGB, d.Source)
	}
}

func resolveEngine(want string, yes, printMissing bool) (runtime.Engine, error) {
	want = runtime.Normalize(want)
	snap := runtime.Detect()
	if e := snap.Preferred(want); e != nil {
		return *e, nil
	}
	kind := want
	if kind == "" {
		kind = runtime.RecommendedKind()
	}
	plan := runtime.InstallPlan(kind)
	if printMissing {
		fmt.Fprintf(os.Stderr, "no %s runtime on PATH.\n\n", plan.Title)
		for _, line := range plan.Manual {
			fmt.Fprintf(os.Stderr, "  %s\n", line)
		}
		fmt.Fprintln(os.Stderr)
	}
	if !yes {
		if !canPrompt() {
			return runtime.Engine{}, fmt.Errorf("pass --yes to install %s, or install it yourself and re-run", plan.Title)
		}
		if !confirm("Install " + plan.Title + " now?") {
			return runtime.Engine{}, fmt.Errorf("aborted — install %s or point at a running server with --url", plan.Title)
		}
	}
	if err := runtime.Install(kind); err != nil {
		return runtime.Engine{}, err
	}
	snap = runtime.Detect()
	if e := snap.Preferred(kind); e != nil {
		return *e, nil
	}
	return runtime.Engine{}, fmt.Errorf("%s installed but still not on PATH; open a new terminal or set RVP_RUNTIME", plan.Title)
}

func canPrompt() bool {
	return stdinIsTTY()
}
