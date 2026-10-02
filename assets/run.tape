# runhug run — local engines, chat REPL help, agents, proxy (no long servers).
#
#   make demo-vhs TAPE=run

Output assets/screenshots/run.gif
Output assets/screenshots/run.mp4

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

Type "./bin/runhug local setup"
Enter
Sleep 1.5s
ScrollUp 26
Sleep 250ms
ScrollDown@70ms 30
Sleep 1.5s

Type "./bin/runhug local add --help"
Enter
Sleep 1.2s

Type "./bin/runhug run --help"
Enter
Sleep 1.2s

Type "./bin/runhug start"
Enter
Sleep 1s

Type "./bin/runhug start claude --no-launch --yes || true"
Enter
Sleep 1.4s
ScrollUp 16
Sleep 250ms
ScrollDown@70ms 18
Sleep 1.2s

Type "./bin/runhug start opencode --dry-run --yes || true"
Enter
Sleep 1.4s
ScrollUp 18
Sleep 250ms
ScrollDown@70ms 20
Sleep 1.2s

Type "./bin/runhug proxy --help"
Enter
Sleep 1.2s

Type "echo '✓ run / local / agents tour'"
Enter
Sleep 2.5s
