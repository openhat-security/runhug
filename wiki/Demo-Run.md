# Demo: Run

Local engines, chat REPL, Claude/OpenCode bridges, OpenAI proxy.

![Run demo](https://raw.githubusercontent.com/openhat-security/runhug/main/assets/screenshots/run.gif)

## Try it yourself

### 1. Local engines

```bash
runhug local setup
runhug local add --help
```

### 2. Chat REPL

```bash
runhug run --help
# against an active deployment:
runhug run
```

### 3. Point an agent at a deployment

```bash
runhug start
runhug start claude --no-launch --yes
runhug start opencode --dry-run --yes
```

### 4. Local OpenAI proxy

```bash
runhug proxy --help
# then:
runhug proxy                  # 127.0.0.1:8080/v1
```

← [All demos](Demos) · [Setup](Setup)
