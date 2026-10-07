# runhug chat — interactive CLI REPL against a local …/v1.
#
#   make demo-vhs TAPE=chat
#
# Needs a local Ollama (or any …/v1) listening on 127.0.0.1:11434 with the model below.

Output assets/screenshots/chat.gif
Output assets/screenshots/chat.mp4

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

# Force color even when the host shell exported NO_COLOR / TERM=dumb.
Env NO_COLOR ""
Env TERM "xterm-256color"
Env COLORTERM "truecolor"
Env CLICOLOR_FORCE "1"

Hide
Type "test -x ./bin/runhug || go build -o bin/runhug ./cmd/runhug"
Enter
Sleep 2s
Type "clear"
Enter
Sleep 300ms
Show

Type "./bin/runhug run --base-url http://127.0.0.1:11434/v1 --model qwen3:8b --yes --new"
Enter
Sleep 4s

Type "/tools off"
Enter
Sleep 1.5s

Type "Say hello from runhug in one short sentence."
Enter
Sleep 16s

Type "/mini"
Enter
Sleep 2.5s

Type "/full"
Enter
Sleep 2.5s

Type "In five words, what is an OpenAI-compatible endpoint?"
Enter
Sleep 16s

Type "/exit"
Enter
Sleep 2.5s

Type "echo '✓ runhug run — fullscreen chat · /mini for scrollback'"
Enter
Sleep 3.5s
