# Demo: Quickstart

Interactive `runhug wizard` — init/credentials → shortlist → GPU pick → dry-run (live deploy declined).

![Quickstart demo](https://raw.githubusercontent.com/openhat-security/runhug/main/assets/screenshots/quickstart.gif)

## Try it yourself

### 1. Prerequisites

```bash
runhug version
runhug init --yes          # local search index (once)
runhug connect             # Runpod key — needed for dry-run / live
# optional: runhug connect hf
```

### 2. Guided wizard

```bash
runhug wizard
```

Typical answers for a safe dry-run tour:

1. Re-run init? → **N** (Enter)
2. Connect HF? → **n** if you are pasting an open model
3. Advisor? → **N** (Enter) unless you want recommend chat
4. Use case → e.g. `small instruct`
5. Pick a row **#**, or paste `org/model` (e.g. `Qwen/Qwen2.5-0.5B-Instruct`)
6. GPU picker → **Enter** for the recommended pool
7. Live deploy? → **N** (Enter) — dry-run already ran
8. Start proxy? → **N** (Enter) if prompted

Non-interactive checklist only:

```bash
runhug wizard --yes
```

### 3. After the wizard

```bash
runhug search -q 'small instruct' --limit 5
runhug deploy Qwen/Qwen2.5-0.5B-Instruct --dry-run --estimate
runhug packs list
```

← [All demos](Demos) · [Setup](Setup)
