# runhug deploy — dry-run RunPod + GCP, registry, GPU pick (no paid resources).
#
#   make demo-vhs TAPE=deploy

Output assets/screenshots/deploy.gif
Output assets/screenshots/deploy.mp4

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

Type "./bin/runhug gpu"
Enter
Sleep 1.2s

Type "./bin/runhug gpu list --filter runpod --sort value --limit 6 || true"
Enter
Sleep 1.2s
ScrollUp 18
Sleep 250ms
ScrollDown@70ms 20
Sleep 1.2s

Type "./bin/runhug deploy Qwen/Qwen2.5-0.5B-Instruct --dry-run --estimate || true"
Enter
Sleep 2s
ScrollUp 50
Sleep 350ms
ScrollDown@55ms 55
Sleep 2s

Type "clear"
Enter
Sleep 350ms

Type "./bin/runhug deploy --provider gcp Qwen/Qwen2.5-0.5B-Instruct-GGUF --dry-run --estimate || true"
Enter
Sleep 2s
ScrollUp 40
Sleep 350ms
ScrollDown@60ms 45
Sleep 2s

Type "./bin/runhug gcp"
Enter
Sleep 1.2s
ScrollUp 24
Sleep 250ms
ScrollDown@70ms 28
Sleep 1.4s

Type "./bin/runhug list || true"
Enter
Sleep 1.2s

Type "./bin/runhug status || true"
Enter
Sleep 1s

Type "echo '✓ deploy tour (dry-run only)'"
Enter
Sleep 2.5s
