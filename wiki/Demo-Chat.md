# Demo: Chat (`runhug run`)

The built-in runhug CLI chat — interactive REPL with streaming, sessions, and slash commands.

![Chat demo](https://raw.githubusercontent.com/openhat-security/runhug/main/assets/screenshots/chat.gif)

## Try it yourself

```bash
runhug run --base-url http://127.0.0.1:11434/v1 --model qwen3:8b --yes --new
# then type at the prompt; /exit or Ctrl-D to quit
```

Against the current registry / remote model:

```bash
runhug run
```

One-shot (no REPL): `runhug run -q '…'`

← [All demos](Demos) · [Agents](Demo-Run) · [Setup](Setup)
