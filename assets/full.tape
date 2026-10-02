# runhug full tour — quickstart → search → deploy → heretic → run (in order).
# Prefer individual tapes for short clips; this is the end-to-end reel.
#
#   make demo-vhs
#   make demo-vhs TAPE=full

Output assets/screenshots/runhug-demo.gif
Output assets/screenshots/runhug-demo.mp4

Require go
Require ffmpeg

Set Shell "bash"
Set FontSize 15
Set Width 1280
Set Height 800
Set TypingSpeed 32ms
Set Theme "Catppuccin Mocha"
Set Padding 16
Set BorderRadius 8
Set WindowBar Colorful
Set Framerate 30

Hide
Type "test -x ./bin/runhug || go build -o bin/runhug ./cmd/runhug"
Enter
Sleep 2s
Type "clear"
Enter
Sleep 300ms
Show

# =============================================================================
# quickstart
# =============================================================================
Type "./bin/runhug version"
Enter
Sleep 1.2s

Type "./bin/runhug"
Enter
Sleep 1.2s
ScrollUp 40
Sleep 350ms
ScrollDown@70ms 45
Sleep 1.5s

Type "clear"
Enter
Sleep 350ms

Type "./bin/runhug wizard --yes"
Enter
Sleep 1.2s
ScrollUp 26
Sleep 300ms
ScrollDown@75ms 30
Sleep 1.5s

Type "./bin/runhug packs list"
Enter
Sleep 1.4s
ScrollUp 12
Sleep 250ms
ScrollDown@70ms 14
Sleep 1.2s

Type "./bin/runhug config --help"
Enter
Sleep 1.2s

Type "clear"
Enter
Sleep 350ms

# =============================================================================
# search
# =============================================================================
Type "./bin/runhug search -q 'small instruct' --limit 5 || true"
Enter
Sleep 1.2s
ScrollUp 16
Sleep 250ms
ScrollDown@70ms 18
Sleep 1.2s

Type "./bin/runhug recommend 'small instruct for a laptop' --no-llm --candidates 5 || true"
Enter
Sleep 1.4s
ScrollUp 20
Sleep 250ms
ScrollDown@70ms 22
Sleep 1.4s

Type "./bin/runhug inspect Qwen/Qwen2.5-0.5B-Instruct || true"
Enter
Sleep 1.5s
ScrollUp 32
Sleep 300ms
ScrollDown@65ms 36
Sleep 1.5s

Type "clear"
Enter
Sleep 350ms

# =============================================================================
# deploy
# =============================================================================
Type "./bin/runhug gpu list --filter runpod --sort value --limit 6 || true"
Enter
Sleep 1.2s
ScrollUp 16
Sleep 250ms
ScrollDown@70ms 18
Sleep 1.2s

Type "./bin/runhug deploy Qwen/Qwen2.5-0.5B-Instruct --dry-run --estimate || true"
Enter
Sleep 2s
ScrollUp 45
Sleep 350ms
ScrollDown@55ms 50
Sleep 2s

Type "./bin/runhug gcp"
Enter
Sleep 1.2s
ScrollUp 22
Sleep 250ms
ScrollDown@70ms 26
Sleep 1.3s

Type "clear"
Enter
Sleep 350ms

# =============================================================================
# heretic
# =============================================================================
Type "./bin/runhug heretic"
Enter
Sleep 1.2s
ScrollUp 18
Sleep 250ms
ScrollDown@70ms 20
Sleep 1.2s

Type "./bin/runhug heretic make Qwen/Qwen2.5-0.5B-Instruct --dry-run --no-upload || true"
Enter
Sleep 2s
ScrollUp 32
Sleep 300ms
ScrollDown@60ms 36
Sleep 1.8s

Type "clear"
Enter
Sleep 350ms

# =============================================================================
# run
# =============================================================================
Type "./bin/runhug local setup"
Enter
Sleep 1.4s
ScrollUp 24
Sleep 250ms
ScrollDown@70ms 28
Sleep 1.4s

Type "./bin/runhug run --help"
Enter
Sleep 1.1s

Type "./bin/runhug start claude --no-launch --yes || true"
Enter
Sleep 1.3s
ScrollUp 14
Sleep 250ms
ScrollDown@70ms 16
Sleep 1.2s

Type "./bin/runhug proxy --help"
Enter
Sleep 1.1s

Type "echo '✓ full tour — quickstart → search → deploy → heretic → run'"
Enter
Sleep 3s
