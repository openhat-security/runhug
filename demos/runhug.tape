# runhug CLI tour — dry-run / read-only commands only (nothing created or billed).
#
# Requires: vhs (brew install vhs), ffmpeg, go
#
#   make demo-vhs
#   vhs demos/runhug.tape
#
# Live interactive tour removed — use VHS recording instead (make demo-vhs).

# After tall output we ScrollUp to the start of the dump, then ScrollDown@…
# so the GIF pans through lines instead of freezing on the bottom.

Output assets/screenshots/runhug-demo.gif
Output assets/screenshots/runhug-demo.mp4

Require go
Require ffmpeg

Set Shell "bash"
Set FontSize 16
Set Width 1280
Set Height 800
Set TypingSpeed 35ms
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

# --- intro ---
Type "./bin/runhug version"
Enter
Sleep 1.5s

Type "./bin/runhug help"
Enter
Sleep 1s
ScrollUp 40
Sleep 400ms
ScrollDown@70ms 45
Sleep 1.5s

Type "clear"
Enter
Sleep 400ms

# --- GPU catalog (no cloud creates) ---
Type "./bin/runhug gpu list --filter gcp --sort name"
Enter
Sleep 1s
ScrollUp 20
Sleep 300ms
ScrollDown@70ms 20
Sleep 1s

Type "./bin/runhug gpu list --filter runpod --sort value --limit 6"
Enter
Sleep 1s
ScrollUp 25
Sleep 300ms
ScrollDown@70ms 25
Sleep 1.5s

Type "clear"
Enter
Sleep 400ms

# --- search / inspect (read-only) ---
Type "./bin/runhug search -q 'small instruct' --limit 5 || true"
Enter
Sleep 1s
ScrollUp 20
Sleep 300ms
ScrollDown@70ms 20
Sleep 1s

Type "./bin/runhug inspect Qwen/Qwen2.5-0.5B-Instruct || true"
Enter
Sleep 1.5s
ScrollUp 35
Sleep 300ms
ScrollDown@70ms 40
Sleep 1.5s

Type "clear"
Enter
Sleep 400ms

# --- RunPod + GCP plans (dry-run only) ---
Type "./bin/runhug deploy Qwen/Qwen2.5-0.5B-Instruct --dry-run --estimate || true"
Enter
Sleep 2s
ScrollUp 60
Sleep 400ms
ScrollDown@60ms 70
Sleep 2s

Type "clear"
Enter
Sleep 400ms

Type "./bin/runhug gcp help"
Enter
Sleep 1s
ScrollUp 35
Sleep 300ms
ScrollDown@70ms 40
Sleep 1.5s

Type "./bin/runhug gcp dockerfile --model Qwen/Qwen2.5-0.5B-Instruct-GGUF"
Enter
Sleep 1.5s
ScrollUp 8
Sleep 400ms
# page through the readonly buffer (Enter ×15), then quit like vim
Enter
Sleep 120ms
Enter
Sleep 120ms
Enter
Sleep 120ms
Enter
Sleep 120ms
Enter
Sleep 120ms
Enter
Sleep 120ms
Enter
Sleep 120ms
Enter
Sleep 120ms
Enter
Sleep 120ms
Enter
Sleep 120ms
Enter
Sleep 120ms
Enter
Sleep 120ms
Enter
Sleep 120ms
Enter
Sleep 120ms
Enter
Sleep 300ms
Type ":q!"
Enter
Sleep 1.5s

Type "clear"
Enter
Sleep 400ms

Type "./bin/runhug heretic make Qwen/Qwen2.5-0.5B-Instruct --dry-run --no-upload || true"
Enter
Sleep 2s
ScrollUp 40
Sleep 400ms
ScrollDown@60ms 45
Sleep 2s

Type "echo 'dry-run tour complete — no resources created'"
Enter
Sleep 3s
