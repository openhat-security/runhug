# runhug agents — launch Claude Code and OpenCode against a local …/v1.
#
#   make demo-vhs TAPE=run
#
# Needs: Ollama on 127.0.0.1:11434 with the model below, plus `claude` and
# `opencode` on PATH. The tape actually execs both TUIs (no --no-launch / --dry-run).
# Claude Code exits with Ctrl-D twice.

Output assets/screenshots/run.gif
Output assets/screenshots/run.mp4

Require go
Require ffmpeg
Require claude
Require opencode

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

Type "./bin/runhug start claude --yes --base-url http://127.0.0.1:11434/v1 --model qwen3:8b"
Enter
Sleep 10s
Ctrl+D
Sleep 400ms
Ctrl+D
Sleep 2.5s

Type "./bin/runhug start opencode --yes --base-url http://127.0.0.1:11434/v1 --model qwen3:8b"
Enter
Sleep 10s
Ctrl+C
Sleep 1s
Ctrl+C
Sleep 2.5s

Type "echo '✓ Claude Code + OpenCode launched through runhug'"
Enter
Sleep 3.5s
