# Demo: Full tour

Combines quickstart → search → deploy → heretic → run in one reel.

![Full tour demo](https://raw.githubusercontent.com/openhat-security/runhug/main/assets/screenshots/runhug-demo.gif)

## Try it yourself

Follow each section in order (or open the linked demo for more detail).

### 1. Setup — [Quickstart](Demo-Quickstart)

```bash
runhug version
runhug wizard --yes
runhug packs list
runhug packs install
```

### 2. Search — [Search](Demo-Search)

```bash
runhug search -q 'small instruct' --limit 5
runhug recommend 'small instruct for a laptop' --no-llm --candidates 5
runhug inspect Qwen/Qwen2.5-0.5B-Instruct
```

### 3. Deploy — [Deploy](Demo-Deploy)

```bash
runhug gpu list --filter runpod --sort value --limit 6
runhug deploy Qwen/Qwen2.5-0.5B-Instruct --dry-run --estimate
runhug gcp
```

### 4. Heretic — [Heretic](Demo-Heretic)

```bash
runhug heretic
runhug heretic make Qwen/Qwen2.5-0.5B-Instruct --dry-run --no-upload
```

### 5. Chat — [Chat](Demo-Chat)

```bash
runhug run
# type at the prompt; /exit or Ctrl-D to quit
```

### 6. Agents — [Agents](Demo-Run)

```bash
runhug start claude --yes
runhug start opencode --yes
```

← [All demos](Demos) · [Setup](Setup)
