# Changelog

All notable user-facing changes to runhug are documented here.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).
This project uses [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Changed
- `runhug run` starts in fullscreen by default. Use `/mini` to restore scrollback; `/full` returns to the framed view.

## [0.4.4] - 2026-10-07

### Added
- VHS demos: `assets/run.tape` launches Claude Code + OpenCode for real; `assets/chat.tape` shows interactive `runhug run` (wiki Demo-Chat / Demo-Run).
- `make install` runs `go install` and stamps `{semver}-localN`, incrementing N from the last GOBIN binary for that semver.
- `runhug run` chat: thinking indicator before first token; slash commands `/help`, `/status`, `/metrics`, `/logs`, `/model`, `/tools`, `/cwd`, `/clear`, `/exit`.
- `/metrics` detailed colored GPU/CPU/RAM dashboard; `/metrics on|off` pins a compact strip; `/metrics full` live fullscreen (q to leave). `runhug metrics` is the same live view for a second terminal.
- `/tools` explains each agent tool (`list_dir`, `read_file`, `write_file`, `edit_file`, `glob`, `bash`) and toggles them.
- Interactive prompt: `/` suggestions, Tab complete, ↑/↓ input history, drag-select copies, Ctrl-V paste; Ctrl-C clears the line while typing and cancels an in-flight reply (does not quit); Ctrl-D or `/exit` quits.
- Chat sessions persist under `~/.config/runhug/sessions` and resume on the next `runhug run` with the transcript on screen and the same messages sent to the model (`/sessions`, `/resume`, `/new`, `--session`, `--new`).
- Local agent tools (default on): `list_dir`, `read_file`, `write_file`, `edit_file`, `glob`, `bash` via OpenAI tool calls; `/tools off` for plain streaming chat. Tool calls print a short summary (path/command) and format `list_dir` / `glob` as an `ls`-style grid instead of truncated JSON.
- `/full` is a runhug-framed alt screen (wordmark, cyan gutter, truncating status, pinned footer). `/mini` restores scrollback.
- `runhug run` plan/agent modes (`/plan`, `/agent`): plan is read-only (`list_dir` / `read_file` / `glob`); `/agent` implements with tools.
- Mutating tools prompt with clickable chips (also arrows / y n a / Esc): Allow, Deny, Always, Never. `/perm ask|allow|deny` and `/settings` persist to `settings.json`.
- `/settings` switches model (ensure + tunnel) and can upsize GPU: RunPod PATCH endpoint pools, GCP stop / set-machine-type / guest accelerator / start / retunnel. Local points at `runhug gpu set`.
- GCP/Run `ensure` prints live status: gcloud account, instance describe/start, SSH vs IAP tunnel, and `/v1/models` attempts with elapsed time and last HTTP error (no more silent hang on “waiting /v1/models”).
- GCP chat tunnels to `127.0.0.1:18080`, not `:8080`. A local Express (or other) app on 8080 is no longer mistaken for llama.cpp.
- After idle stop / dead tunnel, `runhug run` wakes the GCP/RunPod instance with the same ensure steps, then retries the message. Chat errors print in the transcript and copy to the clipboard (`/copy`).
- `runhug gcp deploy` (and `deploy --provider gcp`) uses `{region}-docker.pkg.dev/{project}/runhug/llama-server:cuda` when `--image` is omitted. If the tag is missing, one y/n builds+pushes it (AR repo + `gcloud auth configure-docker`) then creates the VM. Non-GGUF Hub repos y/n onto RunPod serverless instead of dumping commands.
- After `runhug search`, type a row `#` to inspect that model (`deploy N`, `copy N`, or Enter to skip). The same numbers work on the next `runhug inspect 3` / `deploy 3`. `deploy` of a GGUF repo uses GCP Spot llama.cpp (not RunPod vLLM).

### Changed
- `inspect` Next is `deploy` (GCP for GGUF) and `recommend gpu`, not `init` or `search`. `runhug init` is removed (`packs install` / `local add` / `deploy`).
- GCP deploy Next is `runhug run` and `runhug gcp stop` (last instance from the registry). No tunnel/curl/export one-liners.
- GCP llama-server default context is **8192** (32k KV OOMs 30B-class Q4 on L4 24GB).

### Fixed
- `runhug run` no longer polls `/v1/models` with `connection reset by peer` forever: guest docker/startup is shown, and if the VM has no Cloud NAT it gets an ephemeral IP and a reset so docker/GGUF can download.
- `gcp deploy` uses an ephemeral external IP when the project has no Cloud NAT (no-address VMs cannot apt-install docker or pull GGUF).
- Guest docker install prefers Ubuntu `docker.io` and forces apt IPv4.
- `inspect` / `deploy` of a search row number no longer calls Hugging Face with `"3"` (that 401’d). A bare `#` requires `./bin/runhug search` first.
- `runhug run` streams model reasoning (`thinking>`) then the answer; tool rounds stream too instead of freezing on `thinking…`.
- `/full` wheel scrolls chat (↑/↓ still cycle input history). `/mini` and `/full` replay the same transcript instead of blanking the conversation.
- Invalid llama.cpp tool-call JSON is retried without stream, not treated as “tools unsupported”.
- Tool rounds always send `content` on user/tool messages so llama.cpp does not 400 with “All non-assistant messages must contain 'content'”.
- When llama.cpp rejects a huge `write_file` tool JSON, chat retries with a clean no-tools prompt and writes from a `FILEPATH:` / markdown / raw source dump instead of aborting.
- `runhug run` no longer treats every tool-JSON error as a file dump (the old “tool JSON failed” loop). Dump is only for continue/finish of an incomplete write; other turns answer without tools. The coding detector no longer matches the word “writes” inside runhug’s own addendum.
- `runhug run` tools honor a user path focus (last dropped/quoted path), refuse `write_file` overwrites (use `edit_file` / `append_file`), and normalize absolute/glued drag-drop paths to the workspace.
- `read_file` on a directory lists it instead of erroring `is a directory`. Agent turns cap at 8 tool calls and skip repeat package listings so llama.cpp does not loop the whole `internal/` tree until tool JSON breaks.
- `/full` wheel and PgUp/PgDn scroll the transcript; drag-copy uses the same visible rows (including after scroll). `ls` / `pwd` list the workspace immediately instead of continuing a prior write. Agent turns print a files-this-turn summary. Leading-slash tool paths like `/c2edux/...` map into the repo instead of escaping cwd.
- Chat shows OpenCode-style context usage (`ctx  ███░░░░░░░  32%  2.5k/8k`) after each turn, on the status strip, in `/full`, and via `/context`. `/context up` (or `/context 65536`) raises llama.cpp `-c` on GCP by recreating the container. `/gpu` / `/gpu up` / `/gpu A100` resizes the Spot VM. Uses API `usage` when llama.cpp/OpenAI send it; otherwise estimates.
- `/gpu` quotes Google Cloud Billing Spot GPU SKUs (ADC). Unpublished SKUs are omitted. 8× machines × per-GPU SKU. The picker uses a shared column table.
- `runhug cost` tables every registry instance (up/down), GPU SKU or stored $/hr, NOW burn, and 24/7 day/month — plus totals and per-backend sums.

### Fixed
- GCP `/gpu` resize detaches guest accelerators **before** `set-machine-type`, so L4 (nvidia-l4 on G2) can move to A100/A2.

## [0.4.3] - 2026-10-03

### Added
- `runhug run` / `start` ensure cloud backends are up before chat: GCP starts the Spot VM + SSH tunnel, Runpod waits on workers, with step progress and cold-start ETA. Local ollama is started when needed.

### Fixed
- GCP background tunnel no longer hangs on SSH host-key prompts (`BatchMode` + `ExitOnForwardFailure`); failures surface stderr and retry via IAP.

### Changed
- `runhug run` endpoint picker is a color-coded truncated table (`#` / `MODEL` / `BACKEND` / `WHERE` / `DETAIL`).
- `runhug gcp tunnel` default `--local-port` is `18080` (matches deploy / registry).

## [0.4.2] - 2026-10-02

- Cleaned up output for `runhug run`.


## [0.4.1] - 2026-10-02

### Changed
- Root help regrouped: setup (wizard/init/packs/gpu/config), search, deploy, heretic, run (includes local + proxy).
- Credentials live under `runhug config connect|disconnect` (top-level `connect`/`disconnect` still work as aliases). Bare `runhug gpu` shows GPU help.
- VHS demos split: `assets/{quickstart,search,deploy,heretic,run,full}.tape` (`make demo-vhs TAPE=…`).

## [0.4.0] - 2026-10-02

### Changed
- **Producer/consumer split:** Hub crawl / pack build / csv·parquet export live in [hfpacks](https://github.com/openhat-security/hfpacks). `runhug packs` only installs, lists, and removes packs from **openhat-security/hfpacks** Releases (`RUNHUG_PACKS_REPO` override still works).
- Removed in-CLI `packs build` / `upsert` / `index` / `hfpacks` exec bridge, `search --index`/`--share`, Hub mass-crawl `update`/`index-setup`/`index-update`, and the release-index-packs workflow (moved to hfpacks).
- `runhug update` / `update --packs` refresh from Releases (delta JSONL when published, else pack upsert; skip when already on current release SHA).
- `runhug packs list` shows installed + release + full catalog (REL, MATCH, Hub≈, coverage) with `--local` / `--remote`; `install` / `remove` are interactive when no ids are given. `categories` / `types` redirect to `list`.

### Added
- `runhug packs update` (refresh installed packs; `--force` for full replace). `runhug packs install|list|remove|update` consumer commands.
- Pack manifest `hub_total` / `quality_skipped` (from hfpacks builds) for coverage stats.

## [0.3.2] - 2026-10-01

### Added
- `runhug search --type`: broad pack buckets (`llm`, `vision`, `image`, `video`, `audio`, `embeddings`, `gguf`, …) distinct from `--task` (exact Hub `pipeline_tag`).
- `runhug search --index`: upsert the search pool into `models.db` + `packs/<type>.db` with `pack_membership` (deduped by repo id); optional `--share=true|false` or TTY prompt to open a community PR via `gh`.
- `runhug packs` subcommands: `categories`, `build`, `upsert`, `index` (hfpacks workflow embedded).
- Expanded release pack set: `vision`, `embeddings` (plus existing text-generation / image / video / audio / gguf).
- CLI self-update detection: `runhug upgrade` / `runhug update --cli` / `update self` also cover winget, AUR (yay/paru), and `go install` paths.

### Changed
- README: in-CLI indexing + community share via `--index` / `--share` (hfpacks remains compatible for standalone builds).

## [0.3.1] - 2026-09-29

### Added
- `runhug inspect`: Alternatives section (same-repo lighter GGUF quants + related Hub ports/siblings), capped at 3; JSON `alternatives`; Next suggests top pick.

### Fixed
- Quiet CLI by default: search no longer auto-wakes Ollama for embed rerank (use `HF_TOKEN` or `RUNHUG_OLLAMA_EMBED=1`); hide rank notes / source / ACTIONS behind `--verbose` / `RUNHUG_VERBOSE`.
- Quiet local servers by default: ollama `serve`, llama-server, and mlx redirect to `/dev/null`.
- `runhug local` / discover: no `/api/tags` or `ollama list`; `local add --pick` skips semantic embed.
- Local GGUF scan: honor `HF_HOME` / `HUGGINGFACE_HUB_CACHE` and `~/.cache/huggingface/hub`; follow snapshot→blob symlinks; skip broken/incomplete downloads.
- Subcommand help: bare `inspect` / `deploy` / `search` / `recommend` (and `recommend gpu`) print usage + flags instead of a one-line usage error.

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
