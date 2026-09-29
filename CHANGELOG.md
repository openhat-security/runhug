# Changelog

All notable user-facing changes to runhug are documented here.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).
This project uses [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.3.0] - 2026-09-29

### Added
- `demos/runhug.tape` — [charmbracelet/vhs](https://github.com/charmbracelet/vhs) recording of the dry-run tour (`make demo-vhs` → `assets/screenshots/runhug-demo.{gif,mp4}`).

### Changed
- Root `help`: yellow section headers, cyan command names, dim descriptions (aligned columns); clearer tagline colors.
- Plan / cost / heretic / GCP dry-run output: yellow section titles, green `$` amounts, stock color (high/med/low).
- Cost **assumptions** and other detail (env dump, gcloud argv, Dockerfile on dry-run) only with `--verbose` / `-v`.
- `runhug gcp dockerfile` opens Dockerfile + entrypoint in a read-only viewer (`vim -R`, else `less`) when TTY; `--stdout` or `--out <dir>` for scripts.
- README: single install codeblock (commented per OS/channel); tips for `--verbose`, colorized help, `gcp dockerfile` pager; embed dry-run demo GIF.

### Removed
- `cmd/runhug-demo` (saschagrunert/demo interactive tour) — use `make demo-vhs` instead.

## [0.2.2] - 2026-09-29

### Fixed
- Homebrew cask: post-install `xattr` strip of `com.apple.quarantine` so Gatekeeper does not block unsigned darwin binaries from `brew install --cask` (until notarized).

## [0.2.1] - 2026-09-29

### Fixed
- macOS Gatekeeper: `scripts/install.sh` and npm postinstall strip `com.apple.quarantine` after download so unsigned darwin binaries are not blocked on first launch.

## [0.2.0] - 2026-09-29

### Added
- Multi-channel packaging via GoReleaser: Homebrew Cask (`openhat-security/homebrew-tap`), Scoop (`openhat-security/scoop-bucket`), `.deb`/`.rpm` (nfpm), apt + dnf repos on GitHub Pages (`openhat-security/packages`), AUR `runhug-bin` PKGBUILD, winget manifest generator, npm under `packaging/npm`.
- Deploy-time **sampling best-practice** step (`--sampling recommended|none`, `--set key=value`): family table + quant-aware nudge (GGUF/AWQ/GPTQ) + Hub `generation_config.json`. Applied server-side on GCP llama-server; request-time via `runhug run` / Claude bridge on RunPod.
- README / hero: deploy **any** model, including heretic/abliterated builds (`runhug heretic make`).
- `runhug gpu list|set|clear|show` — vendored NVIDIA GPU index (Jr23xd23/gpu-database), `--filter all|local|runpod|gcp`, `--sort best|cheapest|value|vram|name`; preference wired into deploy / recommend / GCP. `gpus` aliases `gpu list`.
- `runhug local run` — start local runtime if needed, then chat. Host GPU probe via `nvidia-smi` / Apple Silicon for `--filter local`.
- `runhug gpu update` — refresh shipped NVIDIA + AMD + GCP catalogs into `~/.config/runhug/gpudb/` (HF index, curated GCP list, optional `gcloud` merge / GitHub Release assets `gpudb-*.json`).

### Changed
- `assets/hero.svg`: Claude Code, OpenCode, RunPod, and GCP marks; any-model / heretic copy.

## [0.1.7] - 2026-09-27

### Added
- **GCP GPU provider (Phase 1):** `runhug gcp deploy|tunnel|status|stop|delete|dockerfile|push|opencode` and `runhug deploy --provider gcp`. Use-time GCP project + HF/GGUF model pick (never hardcoded). Thin-wraps `ghcr.io/ggml-org/llama.cpp:server-cuda` (Dockerfile/entrypoint; no ADC/SA/Bearer baked in); `runhug gcp push` does local `docker build --platform linux/amd64` + push (no Cloud Build). Spot L4/T4 DLVM runs `docker pull` + `docker run` (GCP discontinued `create-with-container`) + stop-on-idle; OpenAI `/v1` on `127.0.0.1` via SSH local-forward tunnel; CLI-managed Bearer; project `.opencode/opencode.json` merge with `{env:OPENAI_API_KEY}` only. Auth via gcloud Application Default Credentials.
- GCP Spot **cost projection** on deploy plan (`--estimate` / dry-run): $/hr, cold-start window, 8h keep-up estimate (approximate us-central1 Spot rates).
- **`--keep-up`**: disable stop-on-idle with an interactive y/N billing warning (skipped with `--yes`); guest entrypoint skips idle watcher when `IDLE_SECONDS=0`.
- **npm package** `runhug`: `npm i -g runhug` / `npx runhug` downloads the matching GitHub Release binary on postinstall.
- `scripts/build-release.sh` cross-compiles `runhug_<ver>_…` assets + `SHA256SUMS.txt`.

### Changed
- Default llama-server context size **32768** (OpenCode agent prompts exceed 8k).
- OpenCode / Next tunnel hints use local port **18080** to avoid colliding with a host `:8080`.

## [0.1.6] - 2026-09-18

### Added
- `runhug heretic make <org/model>` trains a "heretic" (abliteration) model on a RunPod GPU pod running `ghcr.io/adamsiwiec1/runhug-heretic`. The CLI sizes the GPU against the Hub repo, provisions the pod, and streams live trial progress (refusals / KL divergence) to the terminal via the pod dashboard; the decensored model is uploaded to `<hf-user>/heretic-<model>` when a HF token is configured. Also `heretic logs`, `heretic stop`, `heretic status`; pod-backed registry entries show up in `list` / `status` / `delete`.

### Changed
- README-only docs: removed VitePress `docs/` site, Pages workflow, and npm docs tooling; hero at `assets/hero.svg`. Install story is curl|bash / irm|iex (+ optional `go install`). CLI help Docs link points at the GitHub README.
- Product/repo rename: module `github.com/adamsiwiec1/runhug`, CLI binary `runhug` (`cmd/runhug`), config dir `~/.config/runhug` (migrates keys/settings from `~/.config/runhug-cli`). GitHub repo is now `openhat-security/runhug` (legacy `adamsiwiec1/runhug` redirects). Release asset prefix `runhug_<ver>_…` (install scripts still accept legacy `runhug-cli_` assets).

### Fixed
- Live `deploy` create payload matches Runpod v2: top-level `type` (default `QUEUE`, matching `worker-v1-vllm`), `workers.idleTimeout`, and `scaling` as `{type:QUEUE_DELAY,queueDelay}` for QUEUE or `{type:REQUEST_COUNT,requestCount}` for LOAD_BALANCER (no `value`/`idleTimeout` in scaling). Optional `--endpoint-type LOAD_BALANCER` uses FastAPI LB URL `https://{id}.api.runpod.ai/v1` (not `/openai/v1`). QUEUE OpenAI URL remains `https://api.runpod.ai/v2/{id}/openai/v1`.

## [0.1.6-beta.1] - 2026-09-17

### Fixed
- SQLite driver swapped from cgo-based `mattn/go-sqlite3` to pure-Go `modernc.org/sqlite`. Release binaries built with `CGO_ENABLED=0` previously hit a stub and failed to open the local index (`open local index: Binary was compiled with 'CGO_ENABLED=0'…`). Local `search` / `recommend` now work in statically-linked, cgo-free builds, and cross-OS releases no longer need a C toolchain.

## [0.1.5] - 2026-09-15

### Changed
- Release asset names are now `runhug_<ver>_…` (install scripts prefer these; legacy `runhug-cli_` still accepted).
- Version bump for the README-only docs cutover.

## [0.1.4-beta.3] - 2026-09-14

### Fixed
- Wizard GPU picker TTY path no longer staircases under `term.MakeRaw`: all multi-line output uses `\r\n`; no cursor-up frame redraw. Static table + help once; only the single selection line is `\r`+`\033[K` rewritten. `e` appends a one-shot compact estimate then reprints the selection line (no erase/toggle clear).

## [0.1.4-beta.2] - 2026-09-14

### Fixed
- Wizard GPU picker no longer full-table redraws on cycle (row wrap / reverse-video width / estimate height broke cursor math). Table prints once; a fixed status frame under it is the only redrawn region. Soft cyan marker on the status line (no reverse-video); `e` toggles a compact estimate (`CompactLine` + short daily line).

## [0.1.4-beta.1] - 2026-09-14

This project is in **beta**. Pre-releases use hyphen SemVer tags (`v0.1.4-beta.1`). `go install …@latest` ignores pre-releases and stays on the last stable (`v0.1.3`).

### Changed
- Wizard GPU step is a **TTY interactive picker** (colored table, ↑/↓ / j/k / n/p, 1–9 jump, Enter select, Esc/q = recommended). Full cost scenarios are **opt-in** via `e` on the highlighted row only — not dumped for every pool.
- `recommend gpu` and `deploy --dry-run` no longer auto-print huge cost blocks; pass `--estimate` / `-e` for the full cold/warm/daily table. Compact `$X.XX/hr` one-liners remain.

### Added
- `internal/cli/gpu_picker.go` (table formatting + key handling, unit-tested).

## [0.1.3] - 2026-09-14

### Added
- Approximate serverless **cost estimate** block on `wizard` GPU options, `recommend gpu`, and `deploy --dry-run`: $/hr while up, cold-start range from weight size, $/cold vs $/warm request, and daily scenarios (10/100/1000 req, all-warm / 10% cold / all-cold). Labeled as estimates only.
- `wizard` GPU step lists fitting serverless pools (recommended cheapest fit + larger/safer options with VRAM, example GPU, $/hr, stock); user picks by number and that pool is passed to dry-run / live `deploy --gpu`. New `runpod.ListFitting` / `FittingOptions` helpers.

### Fixed
- `wizard` Find-a-model shortlist searches the **raw user query** with `--sort likes` and `--no-semantic` (no `ParseIntent` → `"instruct"` rewrite, no `recommend.Score` known-publisher re-rank). Prints likes/downloads like `search`. `recommend --no-llm` uses the same likes-ordered lexical pool.

## [0.1.2] - 2026-09-14

### Added
- `wizard` (aliases: `guide`, `guided`, `setup`): interactive guided setup from search stack → connect → find model → dry-run deploy; `--yes` / non-TTY prints a checklist and never live-deploys.
- Configurable Hub update limit: `update --limit N`, `RUNHUG_UPDATE_LIMIT`, `config set update_limit` (default 2000; 0=unlimited). New Hub models require likes≥3 and downloads≥100; existing ids always refresh.
- `recommend` command: local shortlist + optional OpenAI-compatible advisor; Suggested GPU per candidate; `recommend gpu <model>`.
- Settings: `advisor_base_url`, `advisor_model`. Documented `models.id` as the unique HF repo id primary key.

## [0.1.1] - 2026-09-14

### Changed
- Index pack builds no longer default-cap at 5000 rows; `--limit 0` / unset `RUNHUG_INDEX_LIMIT` means unlimited.
- Pack builder filters Hub models to `likes >= 3` and `downloads >= 100` (configurable via `--min-likes` / `--min-downloads`), with early-stop when paging by downloads.

### Added
- `ListOpts.MinLikes` / `MinDownloads` with downloads-desc early exit when a whole page is below the download floor.

## [0.1.0] - 2026-09-14

Initial public surface (consolidated from earlier unversioned notes).

### Added
- Category SQLite **index packs** on GitHub Releases (`index-manifest.json`, `index-<category>.db`); `init` multi-select install; `update` watermark deltas / `update --packs`; `cmd/build-index-packs` + release workflow.
- Default `search` uses the local/bundled SQLite index only and never calls the Hub when an index exists. Live Hub search is opt-in via `--online` / `--hub` (rate-limited; set `HF_TOKEN`). If no index exists, search tells you to run `update` or `init` instead of hitting the Hub.
- Restored search table **ACTIONS** column (🔗 OSC-8 Hub link, 📋 plain) and footer hint for `copy N` / `search --copy N`.
- Search is NLP/embeddings-only: removed local chat re-rank (`localllm` / `recommend.Rerank` chat path).
- `init` sets up nomic-embed-text / `connect hf` + optional index (no Qwen chat starter).
- Default Hub task is `auto`/`any` with image/audio intent detection; `--keyword` aliases `--no-semantic`.
- `search -q` / `--query` is the same as a positional query (`--query` wins if both are set). Search matches model card descriptions as well as repo id/tags, and expands a small alias map (`hacking` → `pentest`, …) as extra Hub `search=` calls before local scoring.
- `search --wrap N` / `--word-wrap N` / `-ww N` wraps MODEL names at N runes across multiple lines (clamped 12–80; 0 or omitted = ellipsis truncate at 48).
- `search --copy N` copies the MODEL id for row N to the clipboard.
- Semantic rerank when an embedder is available: local Ollama `nomic-embed-text` (or similar), else Hugging Face Inference `sentence-transformers/all-MiniLM-L6-v2` if `HF_TOKEN` is set. `--semantic` / `--no-semantic`.
- `connect` / `disconnect` persist a Runpod API key in the user config dir (`runpod.key`, mode 0600). Key verified with `GET /v2/serverless` before save. `config.Load()` reads `RUNPOD_API_KEY` first, then the stored key.
- `proxy` — OpenAI proxy on `127.0.0.1:8080/v1`. `serve` remains an alias.
- `list` includes account Serverless endpoints when connected, marked **ours** (registry) vs other account endpoints. `deployments` is an alias.
- ANSI colors with a `NO_COLOR` / non-TTY fallback.

### Changed
- Intermediate rename to **runhug-cli** (later superseded by `runhug` in 0.1.6): module `github.com/adamsiwiec1/runhug-cli`, config `~/.config/runhug-cli`, `RUNHUG_CONFIG` / legacy `RVP_CONFIG`.
- `search --sort` is `relevance` (default), `likes`, or `downloads`.
- `search` accepts `--license` and `--engine`.
- `list` fetches remote endpoints automatically when a key is available (`--local` skips that).

### Fixed
- `connect` Authorization header sanitization strips BOM/non-ASCII clipboard junk.

### Removed
- `chat` (REPL and one-shot).
- `quickstart` (use `search`).
