package cli

import (
	"fmt"
	"io"
	"strings"
)

// printUsageFull prints the long-form command reference.
func printUsageFull(w io.Writer) {
	printBanner(w)
	printTagline(w)

	fmt.Fprintln(w, dim("Full command reference. Short list:"), cyan("runhug --help"))
	fmt.Fprintln(w)
	helpUsage(w, "runhug <command> [flags]")

	helpFullSection(w, "setup",
		helpFullEntry{
			Cmd:  "wizard",
			What: "Interactive first-run checklist: credentials, index packs, optional first search.",
			When: "New install, or when you want guided setup instead of memorizing flags.",
		},
		helpFullEntry{
			Cmd:  "init",
			What: "Install curated category packs into your local search index (~/.config/runhug/models.db).",
			When: "First time, or after deleting your index. Prefer update for later refreshes.",
			More: "Flags: --yes skips prompts. Seeds from the bundled index when you have none yet.",
		},
		helpFullEntry{
			Cmd:  "connect [hf]",
			What: "Save a RunPod API key (default) or Hugging Face token (connect hf).",
			When: "Before deploy / heretic (RunPod) or gated Hub downloads / uploads (HF).",
			More: "Tokens are stored under the runhug config dir; env vars also work if set.",
		},
		helpFullEntry{
			Cmd:  "disconnect [hf]",
			What: "Remove the saved RunPod key or HF token from disk.",
			When: "Rotating credentials or clearing a machine.",
		},
	)

	helpFullSection(w, "search",
		helpFullEntry{
			Cmd:  "search [query]",
			What: "Search the local SQLite index for models. Default never hits the Hub.",
			When: "Everyday model discovery. Add --online / --hub for live Hub results.",
			More: "Useful flags: -q/--query, --type (llm, vision, image, …), --engine, --license,\n  --sort likes|downloads, --limit.",
		},
		helpFullEntry{
			Cmd:  "packs",
			What: "Install, list, or remove category index packs from hfpacks Releases.",
			When: "Growing or refreshing the local index (producer: openhat-security/hfpacks).",
			More: "See: runhug packs install · runhug packs --help",
		},
		helpFullEntry{
			Cmd:  "recommend [query]",
			What: "Shortlist models for a use case; optional local LLM advisor when configured.",
			When: "You want a ranked shortlist instead of raw search rows.",
			More: "recommend gpu <model> estimates VRAM / suggests GPUs for one checkpoint.",
		},
		helpFullEntry{
			Cmd:  "inspect <model>",
			What: "Show Hub card highlights, format/engine, VRAM estimate, and cheaper alternatives.",
			When: "Before deploy — sanity-check license, weights, and cost.",
		},
		helpFullEntry{
			Cmd:  "index",
			What: "Show which search index is active and breakdowns (library, pipeline, license, packs).",
			When: "Debugging empty search results or checking index freshness.",
			More: "Uses your local models.db when present; bundled index only until you create one.",
		},
		helpFullEntry{
			Cmd:  "update",
			What: "Refresh the local model index (Hub delta and/or pack refresh).",
			When: "Index is stale, or search misses models you expect on the Hub.",
			More: "update --cli / update self upgrades the CLI binary (same as upgrade).",
		},
		helpFullEntry{
			Cmd:  "upgrade",
			What: "Upgrade the runhug CLI via brew, scoop, npm, apt, or the install script.",
			When: "You want a newer CLI without touching the model index.",
		},
	)

	helpFullSection(w, "deploy",
		helpFullEntry{
			Cmd:  "deploy <model>",
			What: "Deploy a model as RunPod serverless vLLM (default provider).",
			When: "You need an OpenAI-compatible HTTPS endpoint quickly.",
			More: "Useful flags: --dry-run, --estimate, --provider gcp, --force (gated / non-vLLM).\n  Sampling: recommended / none / customize prompts during deploy.",
		},
		helpFullEntry{
			Cmd:  "gcp …",
			What: "GCP Spot GPU provider: deploy, tunnel, status, push image, dockerfile, …",
			When: "Cheaper Spot L4/T4 llama.cpp workloads, or custom CUDA images.",
			More: "Also: deploy --provider gcp <gguf-model>. See runhug gcp --help.",
		},
		helpFullEntry{
			Cmd:  "heretic wizard | make <model>",
			What: "Guided abliteration walkthrough, or create a training pod + dashboard (+ optional Hub upload).",
			When: "You want an abliterated variant of an instruct model.",
			More: "Needs RunPod (+ HF token if uploading). Try heretic wizard or make --dry-run --no-upload first.",
		},
		helpFullEntry{
			Cmd:  "list",
			What: "Show local deployment registry and provider status.",
			When: "Finding endpoint IDs, URLs, or what is still running.",
		},
		helpFullEntry{
			Cmd:  "proxy",
			What: "Local OpenAI-compatible proxy (default 127.0.0.1:8080/v1) in front of a deployment.",
			When: "Tools expect a local base URL, or you want one stable address across endpoints.",
		},
		helpFullEntry{
			Cmd:  "use / url / status / delete / import",
			What: "Select active deployment, print URL, check health, tear down, or import an endpoint.",
			When: "Day-2 ops after deploy.",
		},
	)

	helpFullSection(w, "chat",
		helpFullEntry{
			Cmd:  "run [model]",
			What: "Interactive chat REPL against the active (or named) deployment.",
			When: "Quick manual testing without an external client.",
		},
		helpFullEntry{
			Cmd:  "start claude|opencode",
			What: "Wire Claude Code or OpenCode to your proxy / deployment via env + bridge hints.",
			When: "Using coding agents against a runhug endpoint.",
		},
	)

	helpFullSection(w, "local",
		helpFullEntry{
			Cmd:  "local add|start|stop|run|setup",
			What: "Download and run models on this machine (llama.cpp / mlx / ollama paths).",
			When: "Offline or free local inference instead of cloud deploy.",
			More: "See runhug local --help for subcommands.",
		},
	)

	helpFullSection(w, "gpu",
		helpFullEntry{
			Cmd:  "gpu list",
			What: "Browse the hardware catalog joined with RunPod / GCP / local detection.",
			When: "Picking a pool for deploy or comparing VRAM / price.",
			More: "Filters: --filter local|amd|nvidia|runpod|gcp · --query · --sort",
		},
		helpFullEntry{
			Cmd:  "gpu set|clear|show",
			What: "Save (or forget) a GPU preference used by deploy / local flows.",
			When: "You always want the same pool without retyping it.",
		},
		helpFullEntry{
			Cmd:  "gpu update",
			What: "Refresh NVIDIA, AMD, and GCP catalogs into ~/.config/runhug/gpudb/.",
			When: "Catalog looks stale, or after new SKUs land upstream.",
			More: "Reports how many GPUs were added (or “no new GPUs”).",
		},
	)

	helpFullSection(w, "config",
		helpFullEntry{
			Cmd:  "config [get|set]",
			What: "Show config dir / settings, or get/set keys.",
			When: "Toggling color, Hub update limits, or the recommend advisor endpoint.",
			More: "Keys: no_color, update_limit, advisor_base_url, advisor_model",
		},
	)

	helpSection(w, "typical flow")
	fmt.Fprintln(w, "  "+cyan("runhug wizard"))
	fmt.Fprintln(w, "  "+cyan("runhug search -q \"small instruct\"")+"  →  "+cyan("runhug inspect <model>"))
	fmt.Fprintln(w, "  "+cyan("runhug deploy <model> --dry-run --estimate")+"  →  "+cyan("runhug deploy <model>"))
	fmt.Fprintln(w, "  "+cyan("runhug proxy")+"  ·  "+cyan("runhug run")+"  ·  "+cyan("runhug start claude"))
	fmt.Fprintln(w)

	helpSection(w, "environment")
	helpEnv(w, "RUNPOD_API_KEY", "RunPod API key (else: runhug connect)")
	helpEnv(w, "HF_TOKEN", "Hugging Face token for gated models / uploads")
	helpEnv(w, "RUNHUG_CONFIG", "override config/registry path")
	helpEnv(w, "NO_COLOR", "disable ANSI color / banner")
	helpEnv(w, "GITHUB_TOKEN", "optional; pack / gpudb release downloads")
	fmt.Fprintln(w)

	fmt.Fprintf(w, "%s %s · %s %s\n",
		dim("short help:"), cyan("runhug --help"),
		dim("this page:"), cyan("runhug --help-full"),
	)
}

type helpFullEntry struct {
	Cmd  string
	What string
	When string
	More string
}

func helpFullSection(w io.Writer, title string, entries ...helpFullEntry) {
	helpSection(w, title)
	for i, e := range entries {
		if i > 0 {
			fmt.Fprintln(w)
		}
		fmt.Fprintf(w, "  %s\n", cyan(e.Cmd))
		helpFullLine(w, "what", e.What)
		helpFullLine(w, "when", e.When)
		if strings.TrimSpace(e.More) != "" {
			helpFullLine(w, "more", e.More)
		}
	}
	fmt.Fprintln(w)
}

func helpFullLine(w io.Writer, label, text string) {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		if i == 0 {
			fmt.Fprintf(w, "    %s  %s\n", dim(padRight(label, 4)), line)
			continue
		}
		fmt.Fprintf(w, "    %s  %s\n", padRight("", 4), line)
	}
}

func helpEnv(w io.Writer, name, desc string) {
	fmt.Fprintf(w, "  %s  %s\n", cyan(padRight(name, 16)), dim(desc))
}
