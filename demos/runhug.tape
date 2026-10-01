# runhug CLI tour — every command / subcommand (dry-run & read-only where possible).
#
# Requires: vhs (brew install vhs), ffmpeg, go
#
#   make demo-vhs
#   vhs demos/runhug.tape
#
# Nothing billed: prefer --dry-run / --check / --yes checklists / help.
# Live mutators (connect keys, delete endpoints, proxy serve) are help-only.

# After tall output we ScrollUp to the start of the dump, then ScrollDown@…
# so the GIF pans through lines instead of freezing on the bottom.

Output assets/screenshots/runhug-demo.gif
Output assets/screenshots/runhug-demo.mp4

Require go
Require ffmpeg

Set Shell "bash"
Set FontSize 15
Set Width 1280
Set Height 800
Set TypingSpeed 28ms
Set Theme "Catppuccin Mocha"
Set Padding 16
Set BorderRadius 8
Set WindowBar Colorful
Set Framerate 30

# --- ensure binary exists (off-camera; `make demo-vhs` also pre-builds) ---
Hide
Type "test -x ./bin/runhug || go build -o bin/runhug ./cmd/runhug"
Enter
Sleep 2s
Type "clear"
Enter
Sleep 300ms
Show

# =============================================================================
# 1) Intro
# =============================================================================
Type "./bin/runhug version"
Enter
Sleep 1.2s

Type "./bin/runhug help"
Enter
Sleep 1s
ScrollUp 50
Sleep 350ms
ScrollDown@65ms 55
Sleep 1.5s

Type "clear"
Enter
Sleep 350ms

# =============================================================================
# 2) Setup
# =============================================================================
Type "./bin/runhug wizard --yes"
Enter
Sleep 1s
ScrollUp 30
Sleep 300ms
ScrollDown@70ms 35
Sleep 1.5s

Type "./bin/runhug init --help"
Enter
Sleep 1.2s

Type "./bin/runhug connect --help"
Enter
Sleep 1s

Type "./bin/runhug connect hf --help"
Enter
Sleep 1s

Type "./bin/runhug disconnect --help"
Enter
Sleep 800ms

Type "./bin/runhug login --help || true"
Enter
Sleep 800ms

Type "./bin/runhug hf --help || true"
Enter
Sleep 800ms

Type "clear"
Enter
Sleep 350ms

# =============================================================================
# 3) Config
# =============================================================================
Type "./bin/runhug config"
Enter
Sleep 1.2s
ScrollUp 20
Sleep 250ms
ScrollDown@70ms 22
Sleep 1.2s

Type "./bin/runhug config get update_limit"
Enter
Sleep 800ms

Type "./bin/runhug config get no_color"
Enter
Sleep 800ms

Type "clear"
Enter
Sleep 350ms

# =============================================================================
# 4) Search index & packs
# =============================================================================
Type "./bin/runhug index"
Enter
Sleep 1.2s
ScrollUp 18
Sleep 250ms
ScrollDown@70ms 20
Sleep 1.2s

Type "./bin/runhug packs"
Enter
Sleep 1.2s

Type "./bin/runhug packs categories --release"
Enter
Sleep 1s
ScrollUp 12
Sleep 250ms
ScrollDown@70ms 14
Sleep 1.2s

Type "./bin/runhug packs categories | head -20"
Enter
Sleep 1.2s

Type "./bin/runhug packs build --help"
Enter
Sleep 1s

Type "./bin/runhug packs upsert"
Enter
Sleep 1s

Type "./bin/runhug packs index"
Enter
Sleep 1s

Type "clear"
Enter
Sleep 350ms

# =============================================================================
# 5) Search / recommend / inspect
# =============================================================================
Type "./bin/runhug search --help"
Enter
Sleep 1.2s
ScrollUp 22
Sleep 250ms
ScrollDown@70ms 24
Sleep 1.2s

Type "./bin/runhug search -q 'small instruct' --limit 5 || true"
Enter
Sleep 1s
ScrollUp 18
Sleep 250ms
ScrollDown@70ms 20
Sleep 1.2s

Type "./bin/runhug search aero --type llm --limit 5 || true"
Enter
Sleep 1s
ScrollUp 18
Sleep 250ms
ScrollDown@70ms 20
Sleep 1.2s

Type "./bin/runhug search aero --type vision --online --limit 5 || true"
Enter
Sleep 1.5s
ScrollUp 18
Sleep 250ms
ScrollDown@70ms 20
Sleep 1.2s

Type "./bin/runhug recommend --help"
Enter
Sleep 1.2s

Type "./bin/runhug recommend 'small instruct for a laptop' --no-llm --candidates 5 || true"
Enter
Sleep 1.5s
ScrollUp 25
Sleep 250ms
ScrollDown@70ms 28
Sleep 1.5s

Type "./bin/runhug recommend gpu Qwen/Qwen2.5-0.5B-Instruct || true"
Enter
Sleep 1.5s
ScrollUp 25
Sleep 250ms
ScrollDown@70ms 28
Sleep 1.5s

Type "./bin/runhug inspect Qwen/Qwen2.5-0.5B-Instruct || true"
Enter
Sleep 1.5s
ScrollUp 40
Sleep 300ms
ScrollDown@65ms 45
Sleep 1.5s

Type "clear"
Enter
Sleep 350ms

# =============================================================================
# 6) Update / upgrade (check / help only — no self-replace)
# =============================================================================
Type "./bin/runhug update --help"
Enter
Sleep 1.2s

Type "./bin/runhug upgrade --help"
Enter
Sleep 1.2s
ScrollUp 22
Sleep 250ms
ScrollDown@70ms 24
Sleep 1.2s

Type "./bin/runhug upgrade --check || true"
Enter
Sleep 1.5s

Type "clear"
Enter
Sleep 350ms

# =============================================================================
# 7) GPU catalog
# =============================================================================
Type "./bin/runhug gpu"
Enter
Sleep 1.2s

Type "./bin/runhug gpu list --filter gcp --sort name --limit 8"
Enter
Sleep 1s
ScrollUp 18
Sleep 250ms
ScrollDown@70ms 20
Sleep 1.2s

Type "./bin/runhug gpu list --filter runpod --sort value --limit 6"
Enter
Sleep 1s
ScrollUp 22
Sleep 250ms
ScrollDown@70ms 24
Sleep 1.2s

Type "./bin/runhug gpu list --filter local --sort vram --limit 8 || true"
Enter
Sleep 1s
ScrollUp 18
Sleep 250ms
ScrollDown@70ms 20
Sleep 1.2s

Type "./bin/runhug gpus --filter nvidia --sort cheapest --limit 6 || true"
Enter
Sleep 1s
ScrollUp 18
Sleep 250ms
ScrollDown@70ms 20
Sleep 1.2s

Type "./bin/runhug gpu show"
Enter
Sleep 1s

Type "./bin/runhug gpu set --help"
Enter
Sleep 1s

Type "./bin/runhug gpu update --help"
Enter
Sleep 1s

Type "clear"
Enter
Sleep 350ms

# =============================================================================
# 8) Deploy (dry-run) + registry
# =============================================================================
Type "./bin/runhug deploy --help"
Enter
Sleep 1.2s
ScrollUp 22
Sleep 250ms
ScrollDown@70ms 24
Sleep 1.2s

Type "./bin/runhug deploy Qwen/Qwen2.5-0.5B-Instruct --dry-run --estimate || true"
Enter
Sleep 2s
ScrollUp 60
Sleep 350ms
ScrollDown@55ms 70
Sleep 2s

Type "clear"
Enter
Sleep 350ms

Type "./bin/runhug deploy --provider gcp Qwen/Qwen2.5-0.5B-Instruct-GGUF --dry-run --estimate || true"
Enter
Sleep 2s
ScrollUp 50
Sleep 350ms
ScrollDown@60ms 55
Sleep 2s

Type "clear"
Enter
Sleep 350ms

Type "./bin/runhug list || true"
Enter
Sleep 1.2s
ScrollUp 25
Sleep 250ms
ScrollDown@70ms 28
Sleep 1.2s

Type "./bin/runhug status || true"
Enter
Sleep 1.2s

Type "./bin/runhug url || true"
Enter
Sleep 1s

Type "./bin/runhug use --help || true"
Enter
Sleep 1s

Type "./bin/runhug import --help"
Enter
Sleep 1s

Type "./bin/runhug delete --help"
Enter
Sleep 1s

Type "clear"
Enter
Sleep 350ms

# =============================================================================
# 9) GCP provider
# =============================================================================
Type "./bin/runhug gcp"
Enter
Sleep 1.2s
ScrollUp 30
Sleep 250ms
ScrollDown@70ms 35
Sleep 1.5s

Type "./bin/runhug gcp deploy --help"
Enter
Sleep 1.2s
ScrollUp 20
Sleep 250ms
ScrollDown@70ms 22
Sleep 1.2s

Type "./bin/runhug gcp dockerfile --model Qwen/Qwen2.5-0.5B-Instruct-GGUF"
Enter
Sleep 1.5s
ScrollUp 8
Sleep 400ms
# page through the readonly buffer (Enter ×12), then quit like vim
Enter
Sleep 100ms
Enter
Sleep 100ms
Enter
Sleep 100ms
Enter
Sleep 100ms
Enter
Sleep 100ms
Enter
Sleep 100ms
Enter
Sleep 100ms
Enter
Sleep 100ms
Enter
Sleep 100ms
Enter
Sleep 100ms
Enter
Sleep 100ms
Enter
Sleep 250ms
Type ":q!"
Enter
Sleep 1.2s

Type "./bin/runhug gcp status || true"
Enter
Sleep 1.2s

Type "./bin/runhug gcp tunnel --help || true"
Enter
Sleep 1s

Type "./bin/runhug gcp stop --help || true"
Enter
Sleep 800ms

Type "./bin/runhug gcp delete --help || true"
Enter
Sleep 800ms

Type "./bin/runhug gcp push --help"
Enter
Sleep 1.2s
ScrollUp 18
Sleep 250ms
ScrollDown@70ms 20
Sleep 1.2s

Type "./bin/runhug gcp opencode --dry-run || true"
Enter
Sleep 1.2s
ScrollUp 20
Sleep 250ms
ScrollDown@70ms 22
Sleep 1.2s

Type "clear"
Enter
Sleep 350ms

# =============================================================================
# 10) Heretic
# =============================================================================
Type "./bin/runhug heretic"
Enter
Sleep 1.2s
ScrollUp 22
Sleep 250ms
ScrollDown@70ms 24
Sleep 1.2s

Type "./bin/runhug heretic make Qwen/Qwen2.5-0.5B-Instruct --dry-run --no-upload || true"
Enter
Sleep 2s
ScrollUp 40
Sleep 300ms
ScrollDown@60ms 45
Sleep 2s

Type "./bin/runhug heretic status || true"
Enter
Sleep 1.2s

Type "./bin/runhug heretic logs --help"
Enter
Sleep 1s

Type "./bin/runhug heretic stop --help"
Enter
Sleep 1s

Type "clear"
Enter
Sleep 350ms

# =============================================================================
# 11) Local runtimes
# =============================================================================
Type "./bin/runhug local setup"
Enter
Sleep 1.5s
ScrollUp 30
Sleep 250ms
ScrollDown@70ms 35
Sleep 1.5s

Type "./bin/runhug local add --help"
Enter
Sleep 1.2s
ScrollUp 18
Sleep 250ms
ScrollDown@70ms 20
Sleep 1.2s

Type "./bin/runhug local start --help"
Enter
Sleep 1s

Type "./bin/runhug local stop --help"
Enter
Sleep 800ms

Type "./bin/runhug local run --help"
Enter
Sleep 1s

Type "clear"
Enter
Sleep 350ms

# =============================================================================
# 12) Chat & agents (no long-running servers / launches)
# =============================================================================
Type "./bin/runhug run --help"
Enter
Sleep 1.2s

Type "./bin/runhug start"
Enter
Sleep 1s

Type "./bin/runhug start claude --no-launch --yes || true"
Enter
Sleep 1.5s
ScrollUp 18
Sleep 250ms
ScrollDown@70ms 20
Sleep 1.2s

Type "./bin/runhug start opencode --dry-run --yes || true"
Enter
Sleep 1.5s
ScrollUp 22
Sleep 250ms
ScrollDown@70ms 24
Sleep 1.2s

Type "./bin/runhug start codex --no-launch --yes || true"
Enter
Sleep 1.2s

Type "./bin/runhug proxy --help"
Enter
Sleep 1.2s

Type "clear"
Enter
Sleep 350ms

# =============================================================================
# 13) Hidden / alias surfaces (still part of the CLI)
# =============================================================================
Type "./bin/runhug index-info || true"
Enter
Sleep 1s

Type "./bin/runhug index-setup --help"
Enter
Sleep 1s

Type "./bin/runhug index-update --help"
Enter
Sleep 1s

Type "./bin/runhug deployments --help"
Enter
Sleep 1s

Type "./bin/runhug serve --help"
Enter
Sleep 1s

Type "echo 'full CLI tour complete — dry-run / help only, no resources created'"
Enter
Sleep 3s
