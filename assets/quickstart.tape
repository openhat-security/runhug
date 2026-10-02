# runhug quickstart — first-run story: install → packs → search → dry-run deploy.
# Slow, readable pacing so the setup feels short.
#
#   make demo-vhs TAPE=quickstart
#   vhs assets/quickstart.tape

Output assets/screenshots/quickstart.gif
Output assets/screenshots/quickstart.mp4

Require go
Require ffmpeg

Set Shell "bash"
Set FontSize 15
Set Width 1280
Set Height 800
Set TypingSpeed 45ms
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
Sleep 400ms
Show

Type "./bin/runhug version"
Enter
Sleep 1.5s

Type "./bin/runhug"
Enter
Sleep 1.5s
ScrollUp 40
Sleep 400ms
ScrollDown@80ms 45
Sleep 2s

Type "clear"
Enter
Sleep 400ms

Type "./bin/runhug wizard --yes"
Enter
Sleep 1.5s
ScrollUp 28
Sleep 350ms
ScrollDown@85ms 32
Sleep 2s

Type "./bin/runhug packs list"
Enter
Sleep 2s
ScrollUp 14
Sleep 300ms
ScrollDown@85ms 16
Sleep 2s

Type "./bin/runhug search -q 'small instruct' --limit 5 || true"
Enter
Sleep 2s
ScrollUp 16
Sleep 300ms
ScrollDown@85ms 18
Sleep 2s

Type "./bin/runhug inspect Qwen/Qwen2.5-0.5B-Instruct || true"
Enter
Sleep 2s
ScrollUp 30
Sleep 350ms
ScrollDown@75ms 35
Sleep 2s

Type "./bin/runhug deploy Qwen/Qwen2.5-0.5B-Instruct --dry-run --estimate || true"
Enter
Sleep 2.5s
ScrollUp 40
Sleep 400ms
ScrollDown@70ms 45
Sleep 2.5s

Type "echo '✓ setup → search → dry-run deploy — ready to go'"
Enter
Sleep 3s
