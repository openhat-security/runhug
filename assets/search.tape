# runhug search — index packs, local search, recommend, inspect.
#
#   make demo-vhs TAPE=search

Output assets/screenshots/search.gif
Output assets/screenshots/search.mp4

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

Type "./bin/runhug packs list"
Enter
Sleep 1.4s
ScrollUp 12
Sleep 250ms
ScrollDown@70ms 14
Sleep 1.2s

Type "./bin/runhug index"
Enter
Sleep 1.2s
ScrollUp 16
Sleep 250ms
ScrollDown@70ms 18
Sleep 1.2s

Type "./bin/runhug search -q 'small instruct' --limit 5 || true"
Enter
Sleep 1.2s
ScrollUp 16
Sleep 250ms
ScrollDown@70ms 18
Sleep 1.2s

Type "./bin/runhug search aero --type llm --limit 5 || true"
Enter
Sleep 1.2s
ScrollUp 16
Sleep 250ms
ScrollDown@70ms 18
Sleep 1.2s

Type "./bin/runhug recommend 'small instruct for a laptop' --no-llm --candidates 5 || true"
Enter
Sleep 1.5s
ScrollUp 22
Sleep 250ms
ScrollDown@70ms 24
Sleep 1.5s

Type "./bin/runhug recommend gpu Qwen/Qwen2.5-0.5B-Instruct || true"
Enter
Sleep 1.5s
ScrollUp 22
Sleep 250ms
ScrollDown@70ms 24
Sleep 1.5s

Type "./bin/runhug inspect Qwen/Qwen2.5-0.5B-Instruct || true"
Enter
Sleep 1.5s
ScrollUp 35
Sleep 300ms
ScrollDown@65ms 40
Sleep 2s

Type "echo '✓ search tour complete'"
Enter
Sleep 2.5s
