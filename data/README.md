# Bundled Search Index

This directory contains a pre-built SQLite search index of Hugging Face models. The index ships with the package to provide instant search out-of-the-box.

## Usage

The CLI automatically uses this bundled index when:
1. No user-local index exists at `~/.config/runhug/models.db`
2. The bundled index is found (relative to executable or in working directory)

## Updating

Install or refresh category packs from [hfpacks Releases](https://github.com/openhat-security/hfpacks/releases):

```bash
runhug packs install
runhug update --packs
```

Once a user-local index exists, it takes precedence over the bundled index.

## Search Hierarchy

1. **User-local index** (`~/.config/runhug/models.db`) — highest priority
2. **Bundled index** (`data/models.db`) — fallback if no user index
3. **No index** — search tells you to run `runhug packs install` (or `init`). It does **not** call the Hub.
4. **`--online` / `--hub`** — optional live Hub search (rate-limited; set `HF_TOKEN`)

## Contents

- `models.db`: SQLite database with ~800+ diverse text-generation models
- Includes: model ID, tags, likes, downloads, license, library, description
- Size: ~500 KB
- Coverage: Popular models + model families (Llama, Qwen, Mistral, Phi, Gemma, DeepSeek, Yi) + GGUF + Safetensors
- Updated: Periodically with package releases

## Category packs (GitHub Releases)

Large category databases are **not** committed here. They are built and published by
[openhat-security/hfpacks](https://github.com/openhat-security/hfpacks)
(CI: `release-index-packs.yml`) and attached to **hfpacks** releases as:

- `index-manifest.json`
- `index-<category>.db`

`runhug packs install` / `init` downloads selected packs, verifies `sha256`, keeps copies
under `~/.config/runhug/packs/`, and merges into `models.db`.

`runhug packs list` shows local vs remote (✓/✗), indexed rows, Hub≈ totals, coverage %,
and remaining (from manifest `hub_total` when present).

Override with `RUNHUG_PACKS_REPO`. Optional exports (csv/parquet) are produced by hfpacks
for non-runhug consumers; runhug only installs SQLite.

**Never** attach `index-*.db` to openhat-security/runhug Releases — packs ship only from hfpacks.
