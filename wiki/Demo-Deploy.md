# Demo: Deploy

GPU catalog + RunPod/GCP dry-run deploy and registry (no paid resources).

![Deploy demo](https://raw.githubusercontent.com/openhat-security/runhug/main/assets/screenshots/deploy.gif)

**Tape source:** [`assets/deploy.tape`](https://github.com/openhat-security/runhug/blob/main/assets/deploy.tape) · **MP4:** [`deploy.mp4`](https://raw.githubusercontent.com/openhat-security/runhug/main/assets/screenshots/deploy.mp4)

## Re-record

```bash
make demo-vhs TAPE=deploy
```

## Commands shown

```bash
runhug gpu
runhug gpu list --filter runpod --sort value --limit 6
runhug deploy Qwen/Qwen2.5-0.5B-Instruct --dry-run --estimate
runhug deploy --provider gcp Qwen/Qwen2.5-0.5B-Instruct-GGUF --dry-run --estimate
runhug gcp
runhug list
runhug status
```

← [All demos](Demos) · [Setup](Setup)
