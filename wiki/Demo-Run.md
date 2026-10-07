# Demo: Agents (Claude Code & OpenCode)

Point Claude Code or OpenCode at a runhug OpenAI-compatible URL — same backend you deploy or run locally.

![Agents demo](https://raw.githubusercontent.com/openhat-security/runhug/main/assets/screenshots/run.gif)

## Try it yourself

### 1. Claude Code

```bash
runhug start claude --yes --base-url http://127.0.0.1:11434/v1 --model qwen3:8b
# starts the Anthropic→OpenAI bridge and launches Claude Code
```

### 2. OpenCode

```bash
runhug start opencode --yes --base-url http://127.0.0.1:11434/v1 --model qwen3:8b
# writes ~/.config/opencode/opencode.json and launches OpenCode
```

Use your RunPod / GCP / registry model instead of `--base-url` when a deployment is current.

← [All demos](Demos) · [Chat REPL](Demo-Chat) · [Setup](Setup)
