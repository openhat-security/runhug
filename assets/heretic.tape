# runhug heretic — abliteration wizard + dry-run make (no pods).
#
#   make demo-vhs TAPE=heretic

Output assets/screenshots/heretic.gif
Output assets/screenshots/heretic.mp4

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

Type "./bin/runhug heretic"
Enter
Sleep 1.4s
ScrollUp 20
Sleep 250ms
ScrollDown@70ms 22
Sleep 1.4s

Type "./bin/runhug heretic wizard --help || true"
Enter
Sleep 1.2s

Type "./bin/runhug heretic make Qwen/Qwen2.5-0.5B-Instruct --dry-run --no-upload || true"
Enter
Sleep 2s
ScrollUp 35
Sleep 300ms
ScrollDown@60ms 40
Sleep 2s

Type "./bin/runhug heretic status || true"
Enter
Sleep 1.2s

Type "./bin/runhug heretic logs --help"
Enter
Sleep 1s

Type "echo '✓ heretic tour (dry-run / help)'"
Enter
Sleep 2.5s
