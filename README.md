# runhug

<p align="center">
  <img src="assets/hero.svg" alt="runhug — Find the best model. Deploy it in minutes. Run it for pennies." width="100%"/>
</p>

**Find the best model. Deploy it in minutes. Run it for pennies.**

## What it does

1. **Search** Hugging Face — find models that fit your use case (local SQLite index + optional embeddings)
2. **Deploy** RunPod serverless vLLM (default) — or **GCP Spot L4/T4** llama.cpp via `--provider gcp`
3. **Use** an OpenAI-compatible URL — chat from any OpenAI client, `runhug run`, or `runhug start claude` / `start opencode`

**Providers:** RunPod remains the default `deploy`. GCP Phase 1 (`runhug deploy --provider gcp` / `runhug gcp …`) picks a project + HF GGUF model at use time, thin-wraps the official CUDA `llama-server` image (`runhug gcp push` → Artifact Registry; no secrets baked in), creates a Spot L4/T4 DLVM that `docker pull`s + runs that image (cold start = pull + start) with stop-on-idle, and exposes OpenAI `/v1` on `127.0.0.1` through an SSH local-forward tunnel (`runhug gcp tunnel`) with a CLI-managed Bearer. Default is no public IP (Cloud NAT for pull/egress; `--public-ip` is dogfood-only). Hugging Face stays Hub search + optional embeddings.

## How to use

```bash
runhug wizard          # guided setup (no live deploy without confirm)
runhug init --yes      # search NLP + optional index packs
runhug connect         # RunPod API key
runhug connect hf      # optional Hub / embeddings token

runhug search -q "small instruct llm"
runhug recommend -q "cheap chat on a small GPU"
runhug deploy <model> --dry-run
runhug deploy <model>              # default endpoint type: QUEUE (RunPod)
runhug deploy --provider gcp <gguf-model> --project <id> --dry-run
runhug gcp push --image REGION-docker.pkg.dev/PROJECT/runhug/llama-server:cuda
runhug gcp tunnel                  # SSH local-forward → http://127.0.0.1:8080/v1
runhug list
runhug proxy                       # local OpenAI proxy @ 127.0.0.1:8080/v1
runhug run [model]                 # chat
runhug start claude                # point Claude Code at the endpoint
```

**QUEUE** OpenAI base: `https://api.runpod.ai/v2/{id}/openai/v1`  
**Load balancer** OpenAI base: `https://{id}.api.runpod.ai/v1`

Other useful commands: `inspect`, `update` / `update --packs`, `gpus`, `url`, `status`, `local add` / `local setup`, `config get|set`. Env: `RUNPOD_API_KEY`, `HF_TOKEN`, `RUNHUG_CONFIG`, `NO_COLOR`.

## Install

**Homebrew (macOS / Linux):**

```bash
brew install --cask openhat-security/tap/runhug
```

**apt (Debian / Ubuntu):**

```bash
curl -fsSL https://openhat-security.github.io/packages/install-apt.sh | sudo bash
```

**dnf (Fedora / RHEL-ish):**

```bash
curl -fsSL https://openhat-security.github.io/packages/install-dnf.sh | sudo bash
```

**Arch (AUR):**

```bash
yay -S runhug-bin
```

**Scoop (Windows):**

```powershell
scoop bucket add openhat https://github.com/openhat-security/scoop-bucket
scoop install runhug
```

**winget (Windows):**

```powershell
winget install OpenHatSecurity.Runhug
```

**npm** (Node ≥ 18):

```bash
npm install -g runhug
# or
npx runhug wizard
```

**Direct binary (macOS / Linux):**

```bash
curl -fsSL https://raw.githubusercontent.com/openhat-security/runhug/main/scripts/install.sh | bash
```

**Direct binary (Windows PowerShell):**

```powershell
irm https://raw.githubusercontent.com/openhat-security/runhug/main/scripts/install.ps1 | iex
```

**Go** (optional):

```bash
go install github.com/adamsiwiec1/runhug/cmd/runhug@latest
```

Packaging details and release secrets: [packaging/README.md](packaging/README.md). Releases: [github.com/openhat-security/runhug/releases](https://github.com/openhat-security/runhug/releases).

From source: `git clone … && go build -o bin/runhug ./cmd/runhug`.

## License & contributing

[LICENSE](LICENSE) · [CONTRIBUTING.md](CONTRIBUTING.md) · [SECURITY.md](SECURITY.md) · [CHANGELOG.md](CHANGELOG.md) · [SUPPORT.md](SUPPORT.md)
