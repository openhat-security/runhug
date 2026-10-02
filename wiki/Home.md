# runhug wiki

**Find the best model. Deploy it in minutes. Run it for pennies.**

This wiki is the step-by-step companion to the [runhug CLI](https://github.com/openhat-security/runhug) (v0.4.1+).

## Start here

1. **[Setup (step by step)](Setup)** — install → credentials → index packs → first search
2. **[Demos](Demos)** — recorded VHS tours (GIF + how to re-record)

<p align="center">
  <img src="https://raw.githubusercontent.com/openhat-security/runhug/main/assets/screenshots/quickstart.gif" alt="runhug quickstart demo" width="100%"/>
</p>

## Command map

| Section | Commands |
|--------|----------|
| **setup** | `wizard` · `init` · `packs` · `gpu` · `config` (connect / disconnect) |
| **search** | `search` · `recommend` · `inspect` · `update` · `upgrade` |
| **deploy** | `deploy` · `gcp` · `list` |
| **heretic** | `heretic wizard` · `heretic make` |
| **run** | `run` · `start` · `local` · `proxy` |

```bash
runhug                 # short help
runhug --help-full     # full reference
```

## Install (any channel)

```bash
brew install --cask openhat-security/tap/runhug
# or: npm i -g runhug
# or: curl -fsSL https://raw.githubusercontent.com/openhat-security/runhug/main/scripts/install.sh | bash
```

See the [README install section](https://github.com/openhat-security/runhug#install) for apt, dnf, Scoop, winget, and AUR.

## Related

- Releases: https://github.com/openhat-security/runhug/releases
- Index packs (producer): https://github.com/openhat-security/hfpacks
- Discussions: https://github.com/openhat-security/runhug/discussions
