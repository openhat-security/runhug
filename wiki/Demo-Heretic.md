# Demo: Heretic

Abliteration: wizard help, dry-run make, status (no pods created).

![Heretic demo](https://raw.githubusercontent.com/openhat-security/runhug/main/assets/screenshots/heretic.gif)

## Try it yourself

### 1. Heretic help

```bash
runhug heretic
runhug heretic wizard --help
```

### 2. Dry-run make (no upload, no spend)

```bash
runhug config connect         # RunPod key required for a real make
runhug heretic make Qwen/Qwen2.5-0.5B-Instruct --dry-run --no-upload
```

### 3. Status and logs

```bash
runhug heretic status
runhug heretic logs --help
```

← [All demos](Demos) · [Setup](Setup)
