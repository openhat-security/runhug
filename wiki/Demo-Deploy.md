# Demo: Deploy

GPU catalog + RunPod/GCP dry-run deploy and registry (no paid resources).

![Deploy demo](https://raw.githubusercontent.com/openhat-security/runhug/main/assets/screenshots/deploy.gif)

## Try it yourself

### 1. GPU catalog

```bash
runhug gpu
runhug gpu list --filter runpod --sort value --limit 6
# optional: save a preference
runhug gpu set
```

### 2. Dry-run RunPod deploy

```bash
runhug config connect         # once, if you plan a real deploy later
runhug deploy Qwen/Qwen2.5-0.5B-Instruct --dry-run --estimate
```

### 3. Dry-run GCP Spot

```bash
runhug deploy --provider gcp Qwen/Qwen2.5-0.5B-Instruct-GGUF --dry-run --estimate
runhug gcp
```

### 4. Registry

```bash
runhug list
runhug status
```

← [All demos](Demos) · [Setup](Setup)
