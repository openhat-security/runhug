package cli

import (
	"fmt"
	"io"

	"github.com/adamsiwiec1/runhug/internal/packs"
)

// Full help pages for leaf / parent commands that already have short print*Help.

func printSearchHelpFull(w io.Writer) {
	helpUsage(w, "runhug search [query]")
	fmt.Fprintln(w, dim("Local SQLite index by default. Short list:"), cyan("runhug search --help"))
	fmt.Fprintln(w)
	helpFullSection(w, "overview",
		helpFullEntry{
			Cmd:  "search [query]",
			What: "Find models by text query against your local index (or live Hub with --online).",
			When: "Everyday discovery before inspect / deploy / local add.",
			More: "--type = broad pack bucket (llm, vision, image, …)\n  --task = exact Hub pipeline_tag\n  --online/--hub for live Hub (rate-limited; set HF_TOKEN)",
		},
	)
	helpSection(w, "flags")
	helpFlag(w, "--query -q", "search query (same as positional)")
	helpFlag(w, "--online --hub", "live Hugging Face Hub search")
	helpFlag(w, "--type", "llm, vision, image, video, audio, embeddings, gguf, …")
	helpFlag(w, "--task", "exact pipeline_tag (auto/any/text-generation/…)")
	helpFlag(w, "--sort", "relevance (default), likes, or downloads")
	helpFlag(w, "--limit", "rows to show (1-100)")
	helpFlag(w, "--author --engine --license", "filters")
	helpFlag(w, "--verbose -v --json --copy N", "detail / JSON / clipboard")
	fmt.Fprintln(w)
	fmt.Fprintf(w, "%s %s\n", dim("example:"), cyan(`runhug search aero --type llm`))
}

func printDeployHelpFull(w io.Writer) {
	helpUsage(w, "runhug deploy <org/model>")
	fmt.Fprintln(w, dim("Serverless vLLM on RunPod (default) or GCP Spot via --provider gcp."))
	fmt.Fprintln(w, dim("Short list:"), cyan("runhug deploy --help"))
	fmt.Fprintln(w)
	helpFullSection(w, "overview",
		helpFullEntry{
			Cmd:  "deploy <model>",
			What: "Create a RunPod serverless endpoint running worker-vLLM for the Hub repo.",
			When: "You need an OpenAI-compatible HTTPS URL quickly.",
			More: "Requires runhug connect. Use --dry-run --estimate before spending.\n  GGUF needs --provider gcp or a llama.cpp worker (--force to override).",
		},
		helpFullEntry{
			Cmd:  "--provider gcp",
			What: "Hand off to GCP Spot deploy (same as runhug gcp deploy).",
			When: "Cheaper Spot L4/T4 llama.cpp instead of RunPod serverless.",
		},
	)
	helpSection(w, "flags")
	helpFlag(w, "--provider", "runpod (default) or gcp")
	helpFlag(w, "--gpu --gpu-count", "pool / GPUs per worker")
	helpFlag(w, "--max-len --disk", "context length / container disk")
	helpFlag(w, "--estimate -e", "full cost block on the plan")
	helpFlag(w, "--yes --dry-run --json", "confirm / plan-only / JSON")
	helpFlag(w, "--verbose -v", "assumptions + env dump")
	helpFlag(w, "--force", "gated without HF_TOKEN, or non-vLLM formats")
	fmt.Fprintln(w)
	fmt.Fprintf(w, "%s %s\n", dim("example:"), cyan("runhug deploy Qwen/Qwen2.5-7B-Instruct --dry-run --estimate"))
}

func printRecommendHelpFull(w io.Writer) {
	helpUsage(w, "runhug recommend <query>")
	fmt.Fprintln(w, dim("Shortlist models for a use-case. Short list:"), cyan("runhug recommend --help"))
	fmt.Fprintln(w)
	helpFullSection(w, "commands",
		helpFullEntry{
			Cmd:  "recommend <query>",
			What: "Score a shortlist from the local index; optional local LLM advisor when configured.",
			When: "You want ranked picks for a use case, not raw search rows.",
			More: "--no-llm skips advisor. --online allows Hub fill-in. config set advisor_* for Ollama/etc.",
		},
		helpFullEntry{
			Cmd:  "recommend gpu <model>",
			What: "VRAM estimate + suggested serverless GPU pool for one checkpoint.",
			When: "Before deploy, to size cost and pool.",
			More: "--estimate for cold/warm/daily cost block.",
		},
	)
}

func printRecommendGPUHelpFull(w io.Writer) {
	helpUsage(w, "runhug recommend gpu <org/model>")
	fmt.Fprintln(w, dim("VRAM + GPU pool for one model. Short list:"), cyan("runhug recommend gpu --help"))
	fmt.Fprintln(w)
	helpFullSection(w, "overview",
		helpFullEntry{
			Cmd:  "recommend gpu <model>",
			What: "Estimate required VRAM and suggest a RunPod serverless pool (live catalog when connected).",
			When: "Picking --gpu for deploy or checking laptop fit.",
			More: "--estimate -e full cost · --max-len context · --gpu force pool · --json",
		},
	)
}

func printInspectHelpFull(w io.Writer) {
	helpUsage(w, "runhug inspect <org/model>")
	fmt.Fprintln(w, dim("Hub card · VRAM · cheaper alternatives. Short list:"), cyan("runhug inspect --help"))
	fmt.Fprintln(w)
	helpFullSection(w, "shows",
		helpFullEntry{
			Cmd:  "format / params",
			What: "Engine, precision, weight size, and VRAM estimate for a context length.",
			When: "Before deploy or local download.",
		},
		helpFullEntry{
			Cmd:  "gpu",
			What: "Suggested serverless pool (needs runhug connect for live prices).",
			When: "Cost sanity-check.",
		},
		helpFullEntry{
			Cmd:  "alternatives",
			What: "Up to 3 smaller / cheaper fits (GGUF quants + related Hub).",
			When: "The first pick is too large or expensive.",
		},
	)
}

func printUpgradeHelpFull(w io.Writer) {
	helpUsage(w, "runhug upgrade")
	fmt.Fprintln(w, dim("Upgrade the CLI via the same channel you installed. Short list:"), cyan("runhug upgrade --help"))
	fmt.Fprintln(w)
	helpFullSection(w, "overview",
		helpFullEntry{
			Cmd:  "upgrade",
			What: "Detect brew/scoop/winget/npm/apt/dnf/aur/go/github install and upgrade that way.",
			When: "You want a newer CLI without refreshing the model index.",
			More: "--check report only · --force reinstall even if latest\n  Aliases: runhug update --cli · runhug update self",
		},
	)
}

func printPacksHelpFull(w io.Writer) {
	helpUsage(w, "runhug packs <command>")
	fmt.Fprintln(w, dim("Install category packs from hfpacks Releases. Short list:"), cyan("runhug packs --help"))
	fmt.Fprintln(w)
	helpFullSection(w, "commands",
		helpFullEntry{
			Cmd:  "list",
			What: "Show installed packs, hfpacks Release assets, and full category catalog (MATCH column).",
			When: "See what you have vs what the latest Release offers; discover ids for search --type.",
			More: "--local only installed · --remote only release assets\n  REL ✓ = on latest Release. Alias: status · categories/types → list",
		},
		helpFullEntry{
			Cmd:  "install [ids…]",
			What: "Download SQLite packs from github.com/" + packs.ReleaseRepo() + " and merge into models.db.",
			When: "First-time setup, or to add missing categories.",
			More: "No ids → interactive numbered picker (default: missing).\n  --yes installs all. Override repo: RUNHUG_PACKS_REPO=owner/name",
		},
		helpFullEntry{
			Cmd:  "update",
			What: "Re-download and merge packs you already installed from the latest hfpacks Release.",
			When: "After a new hfpacks Release; same as runhug update --packs.",
			More: "--force full pack replace instead of delta when available.",
		},
		helpFullEntry{
			Cmd:  "remove [ids…]",
			What: "Delete local pack DB copies and installed.json entries.",
			When: "Freeing disk or dropping a category.",
			More: "No ids → interactive picker among installed. --yes removes all.",
		},
	)
	fmt.Fprintln(w)
	fmt.Fprintln(w, dim("Produce packs (crawl Hub, csv/parquet export):"))
	fmt.Fprintln(w, "  "+cyan("https://github.com/openhat-security/hfpacks"))
	fmt.Fprintln(w, dim("Packs are never published on openhat-security/runhug Releases."))
}

func printGCPHelpFull(w io.Writer) {
	helpUsage(w, "runhug gcp <command>")
	fmt.Fprintln(w, dim("Spot L4/T4 · llama.cpp · stop-on-idle · tunnel → 127.0.0.1:8080/v1"))
	fmt.Fprintln(w, dim("Short list:"), cyan("runhug gcp --help"))
	fmt.Fprintln(w)
	helpFullSection(w, "commands",
		helpFullEntry{
			Cmd:  "deploy [model]",
			What: "Create a Spot VM with a prebuilt image (AR/GCR) serving llama.cpp OpenAI API.",
			When: "Cheaper than RunPod serverless for GGUF / long Spot runs.",
			More: "Needs --project, --image (live). --dry-run --estimate first.\n  Also: runhug deploy --provider gcp <model>",
		},
		helpFullEntry{
			Cmd:  "tunnel [name]",
			What: "SSH/IAP local-forward so 127.0.0.1:8080/v1 hits the VM.",
			When: "After deploy, before proxy/run/agents.",
		},
		helpFullEntry{
			Cmd:  "status|stop|delete",
			What: "Instance lifecycle (stop keeps disk; delete tears down).",
			When: "Day-2 ops / stopping spend.",
		},
		helpFullEntry{
			Cmd:  "dockerfile | push",
			What: "View the image recipe, or docker build+push to Artifact Registry.",
			When: "Customizing the serving image.",
		},
		helpFullEntry{
			Cmd:  "opencode",
			What: "Merge .opencode/opencode.json for agent wiring.",
			When: "Using OpenCode against the GCP endpoint.",
		},
	)
}

func printHereticHelpFull(w io.Writer) {
	helpUsage(w, "runhug heretic <command>")
	fmt.Fprintln(w, dim("Abliteration training on RunPod. Short list:"), cyan("runhug heretic --help"))
	fmt.Fprintln(w)
	helpFullSection(w, "commands",
		helpFullEntry{
			Cmd:  "wizard",
			What: "Interactive walkthrough: credentials, model pick, options, dry-run, optional live make.",
			When: "First heretic run, or when you want prompts instead of flags.",
			More: "Non-interactive checklist: runhug heretic wizard --yes (never creates a live pod).",
		},
		helpFullEntry{
			Cmd:  "make <model>",
			What: "Create a training pod, follow progress, optional Hub upload of the abliterated weights.",
			When: "You want an abliterated variant of an instruct model.",
			More: "Needs RunPod (+ HF token if uploading). Try --dry-run --no-upload first.\n  Pass-through heretic flags after -- : runhug heretic make <m> -- --seed 1337",
		},
		helpFullEntry{
			Cmd:  "logs|status|stop",
			What: "Tail training log, show dashboard link, or terminate the pod.",
			When: "Monitoring or ending a make job.",
		},
	)
}

func printLocalHelp(w io.Writer) {
	helpUsage(w, "runhug local <command>")
	helpSection(w, "commands")
	helpCmd(w, "setup", "detect local engines (llama.cpp / mlx / ollama)")
	helpCmd(w, "add", "register a GGUF, Hub download, or running URL")
	helpCmd(w, "start|stop", "serve / stop a registry model")
	helpCmd(w, "run", "start if needed, then chat REPL")
	fmt.Fprintln(w)
}

func printLocalHelpFull(w io.Writer) {
	helpUsage(w, "runhug local <command>")
	fmt.Fprintln(w, dim("Run models on this machine. Short list:"), cyan("runhug local --help"))
	fmt.Fprintln(w)
	helpFullSection(w, "commands",
		helpFullEntry{
			Cmd:  "setup",
			What: "Detect installed local engines and print what is missing.",
			When: "First local run, or after installing llama.cpp / mlx / ollama.",
		},
		helpFullEntry{
			Cmd:  "add",
			What: "Register a .gguf path, download a Hub GGUF, or point at an already-running OpenAI base URL.",
			When: "Getting a model into the local registry.",
		},
		helpFullEntry{
			Cmd:  "start|stop|run",
			What: "Serve a registry model, stop it, or start+chat.",
			When: "Offline / free inference instead of cloud deploy.",
		},
	)
}
