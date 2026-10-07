# Setup (step by step)

End-to-end first-run path. Prefer the guided wizard if you want checkboxes; use the manual steps when automating.

Watch it: **[Demo: Quickstart](Demo-Quickstart)**

---

## 1. Install the CLI

Pick one:

```bash
# Homebrew (macOS / Linux)
brew install --cask openhat-security/tap/runhug

# npm
npm install -g runhug

# curl installer
curl -fsSL https://raw.githubusercontent.com/openhat-security/runhug/main/scripts/install.sh | bash
```

Check:

```bash
runhug version    # expect 0.4.1+
runhug            # grouped help
```

---

## 2. Guided setup (recommended)

```bash
runhug wizard
```

Non-interactive checklist:

```bash
runhug wizard --yes
```

The wizard walks credentials, index packs, and an optional first search.

---

## 3. Credentials (`config`)

Tokens are **not** on the root help screen — they live under **config**:

```bash
runhug config --help

# RunPod (deploy / heretic)
runhug config connect
# or alias: runhug connect

# Hugging Face (gated models / uploads)
runhug config connect hf
# or alias: runhug connect hf
```

Env vars also work: `RUNPOD_API_KEY`, `HF_TOKEN`.

Inspect status (paths only — secrets never printed):

```bash
runhug config
```

Clear later:

```bash
runhug config disconnect
runhug config disconnect hf
```

---

## 4. Search index packs

Packs are SQLite category indexes from **[hfpacks Releases](https://github.com/openhat-security/hfpacks/releases)** (not from runhug releases).

```bash
# Interactive install (default: missing packs)
runhug packs install

# Or by id / alias
runhug packs install llm gguf

# See local vs remote
runhug packs list

# Refresh installed packs after a new hfpacks release
runhug packs update
```

Shortcut used by wizard/init:

```bash
runhug init --yes
```

---

## 5. GPU preference (optional)

```bash
runhug gpu                 # help (not a catalog dump)
runhug gpu list --filter runpod --sort value --limit 10
runhug gpu set             # interactive preference for deploy / local
```

---

## 6. First search & inspect

```bash
runhug search -q "small instruct" --limit 10
runhug recommend "small instruct for a laptop" --no-llm --candidates 5
runhug inspect Qwen/Qwen2.5-0.5B-Instruct
```

Add `--online` / `--hub` only when you want live Hub results (token helps rate limits).

---

## 7. Dry-run deploy (no spend)

```bash
runhug deploy Qwen/Qwen2.5-0.5B-Instruct --dry-run --estimate
```

When ready for real deploy (needs RunPod key):

```bash
runhug deploy Qwen/Qwen2.5-0.5B-Instruct
runhug proxy               # OpenAI-compatible @ 127.0.0.1:8080/v1
runhug run                 # chat REPL (fullscreen; /mini for scrollback)
runhug start claude        # or: start opencode
```

---

## 8. Keep current

```bash
runhug upgrade             # CLI binary (brew/npm/…)
runhug packs update        # index packs from hfpacks
# or: runhug update --packs
```

---

## Next demos

| Flow | Page |
|------|------|
| Slow first-run story | [Demo: Quickstart](Demo-Quickstart) |
| Search / packs / recommend | [Demo: Search](Demo-Search) |
| Deploy dry-run + GCP | [Demo: Deploy](Demo-Deploy) |
| Heretic abliteration | [Demo: Heretic](Demo-Heretic) |
| Built-in chat REPL | [Demo: Chat](Demo-Chat) |
| Claude Code / OpenCode | [Demo: Agents](Demo-Run) |
| Everything in order | [Demo: Full](Demo-Full) |
