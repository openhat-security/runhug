# runhug

<p align="center">
  <img src="assets/hero.svg" alt="runhug — Find the best model. Deploy it in minutes. Run it for pennies." width="100%"/>
</p>

**Find the best model. Deploy it in minutes. Run it for pennies.**

Search Hugging Face, deploy any checkpoint (RunPod serverless vLLM or GCP Spot llama.cpp), then chat via an OpenAI-compatible URL, Claude Code, or OpenCode — including abliterated **heretic** models trained in-CLI.

<p align="center">
  <img src="assets/screenshots/runhug-demo.gif" alt="runhug CLI dry-run tour" width="100%"/>
</p>

## Install

```bash
# macOS / Linux — Homebrew
brew install --cask openhat-security/tap/runhug

# Debian / Ubuntu — apt
curl -fsSL https://openhat-security.github.io/packages/install-apt.sh | sudo bash

# Fedora / RHEL — dnf
curl -fsSL https://openhat-security.github.io/packages/install-dnf.sh | sudo bash

# Arch — AUR
yay -S runhug-bin

# Windows — Scoop
scoop bucket add openhat https://github.com/openhat-security/scoop-bucket
scoop install runhug

# Windows — winget
winget install OpenHatSecurity.Runhug

# any OS — npm (Node ≥ 18)
npm install -g runhug
# or: npx runhug wizard

# macOS / Linux — direct binary
curl -fsSL https://raw.githubusercontent.com/openhat-security/runhug/main/scripts/install.sh | bash

# Windows — direct binary (PowerShell)
irm https://raw.githubusercontent.com/openhat-security/runhug/main/scripts/install.ps1 | iex

# optional — Go
go install github.com/adamsiwiec1/runhug/cmd/runhug@latest
```

Packaging details: [packaging/README.md](packaging/README.md) · Releases: [github.com/openhat-security/runhug/releases](https://github.com/openhat-security/runhug/releases)

## Quick start

```bash
runhug wizard                 # guided setup
runhug init --yes             # search NLP + index packs
runhug connect && runhug connect hf

runhug search -q "small instruct llm"
runhug deploy <model> --dry-run --estimate
runhug deploy <model>         # sampling: recommended / none / customize

runhug heretic make <org/model> --dry-run --no-upload
runhug deploy --provider gcp <gguf-model> --project <id> --dry-run
runhug gcp dockerfile --model <gguf>   # readonly pager (vim -R / less)
runhug gcp push --image REGION-docker.pkg.dev/PROJECT/runhug/llama-server:cuda
runhug gcp tunnel

runhug proxy                  # OpenAI proxy @ 127.0.0.1:8080/v1
runhug run [model]            # chat REPL
runhug start claude           # Claude Code bridge
runhug start opencode         # OpenCode bridge
```

**RunPod OpenAI bases:** QUEUE `https://api.runpod.ai/v2/{id}/openai/v1` · load balancer `https://{id}.api.runpod.ai/v1`

## Tips

- Plans print essentials by default. Pass `--verbose` / `-v` for cost assumptions, env dumps, and Dockerfile on GCP dry-run.
- `--estimate` / `-e` shows cold/warm/daily cost scenarios (assumptions still need `-v`).
- `runhug help`, `runhug gcp help`, `runhug heretic help`, `runhug gpu help` — short colorized command lists.
- Other commands: `inspect`, `recommend gpu`, `update`, `gpu list|set|clear|update`, `list`, `local …`, `config get|set`.
- Env: `RUNPOD_API_KEY`, `HF_TOKEN`, `RUNHUG_CONFIG`, `NO_COLOR`, `PAGER`.

**Record the demo GIF:** `brew install vhs && make demo-vhs` → `assets/screenshots/runhug-demo.{gif,mp4}` (`demos/runhug.tape`).

## Build your own model index ([hfpacks](https://github.com/openhat-security/hfpacks))

`runhug init` / `update --packs` installs the **community category index packs** we ship on [GitHub Releases](https://github.com/openhat-security/runhug/releases) (`index-*.db` + `index-manifest.json`). Those packs are built with **[hfpacks](https://github.com/openhat-security/hfpacks)** — a small CLI that crawls the Hugging Face Hub and writes the same SQLite + manifest layout runhug already understands.

### Build a pack set locally

```bash
git clone https://github.com/openhat-security/hfpacks.git
cd hfpacks && go build -o bin/hfpacks ./cmd/hfpacks

# optional but recommended for Hub rate limits
export HF_TOKEN=hf_…

# crawl (auto proxy pool by default; use -no-proxy for direct Hub)
./bin/hfpacks build -out dist/index
# examples:
#   ./bin/hfpacks build -out dist/index -categories text-generation,gguf -limit 5000
#   ./bin/hfpacks build -out dist/index -no-proxy -token "$HF_TOKEN"
```

Output (compatible with runhug):

```
dist/index/
  index-manifest.json
  index-text-generation.db
  index-text-to-image.db
  index-video.db
  index-audio.db
  index-gguf.db
```

Use your packs privately by pointing a local merge / `update --packs`-style install at that directory, or keep `~/.config/runhug/models.db` after importing.

### Share packs with the community

Anyone can improve the shared indexes. The contract is the **manifest + category DBs** above — same schema as release assets.

1. **Build** with hfpacks (note your `hfpacks` commit / flags / date in the PR).
2. **Verify** locally: install or merge the DBs, then `runhug search -q "…"` and spot-check a few `runhug inspect` rows.
3. **Open a pull request** on [openhat-security/runhug](https://github.com/openhat-security/runhug) that either:
   - attaches the new `index-*.db` + `index-manifest.json` as proposed release assets (describe category coverage, row counts, and quality floors such as min likes/downloads), or
   - links a public download (Release / Gist / your fork’s Release) maintainers can promote into the next runhug index-pack release.
4. **Maintainers** review schema compatibility, size, and licensing of metadata, then publish under [Releases](https://github.com/openhat-security/runhug/releases) so `runhug init` / `update --packs` pick them up for everyone.

Prefer a PR discussion before uploading multi‑hundred‑MB artifacts to the main repo tree — Releases (or an external URL in the issue) are the right place for the binaries.

Questions or pack ideas: open a [Discussion](https://github.com/openhat-security/runhug/discussions) or issue tagged for indexing.

## License & contributing

[LICENSE](LICENSE) · [CONTRIBUTING.md](CONTRIBUTING.md) · [SECURITY.md](SECURITY.md) · [CHANGELOG.md](CHANGELOG.md) · [SUPPORT.md](SUPPORT.md)
