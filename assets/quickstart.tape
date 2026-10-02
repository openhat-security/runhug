# runhug quickstart — interactive wizard walkthrough (first-run story).
# Scripted y/n + model pick; dry-run only (live deploy declined).
#
#   make demo-vhs TAPE=quickstart
#   vhs assets/quickstart.tape
#
# Expects a local index (runhug init) and Runpod connected for dry-run.
# HF can be skipped — demo pastes an open model.

Output assets/screenshots/quickstart.gif
Output assets/screenshots/quickstart.mp4

Require go
Require ffmpeg

Set Shell "bash"
Set FontSize 15
Set Width 1280
Set Height 800
Set TypingSpeed 40ms
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
Sleep 1.2s

Type "./bin/runhug wizard"
Enter
Sleep 1.8s

# 2) Init — index present → decline re-run
Enter
Sleep 900ms

# 3) HF — skip (demo uses an open model)
Type "n"
Enter
Sleep 1.2s

# 4) Runpod — auto-skip when already connected
# 5) Advisor — keep existing / decline configure
Enter
Sleep 900ms

# 6) Find a model — use case → shortlist → paste open Qwen
Type "small instruct"
Enter
Sleep 2.5s
Type "Qwen/Qwen2.5-0.5B-Instruct"
Enter
Sleep 3.5s

# 7) GPU picker — Enter selects recommended
Enter
Sleep 5s

# 8) Deploy dry-run (automatic when Runpod connected)
# 9) Live deploy? — decline (default)
Sleep 1s
Enter
Sleep 1.2s

# 10) Proxy — decline if an endpoint already exists in the registry
Enter
Sleep 2.5s

# 11) Done — scroll the plan + next commands
ScrollUp 45
Sleep 500ms
ScrollDown@70ms 50
Sleep 3s
