# Demo: Quickstart

Slow, readable first-run: version → help → wizard → packs → search → inspect → dry-run deploy.

![Quickstart demo](https://raw.githubusercontent.com/openhat-security/runhug/main/assets/screenshots/quickstart.gif)

## Try it yourself

### 1. Check the CLI

```bash
runhug version
runhug
```

### 2. Guided setup

```bash
runhug wizard
# or non-interactive:
runhug wizard --yes
```

### 3. See index packs

```bash
runhug packs list
# install any missing:
runhug packs install
```

### 4. Search and inspect

```bash
runhug search -q 'small instruct' --limit 5
runhug inspect Qwen/Qwen2.5-0.5B-Instruct
```

### 5. Dry-run deploy (no spend)

```bash
runhug deploy Qwen/Qwen2.5-0.5B-Instruct --dry-run --estimate
```

← [All demos](Demos) · [Setup](Setup)
