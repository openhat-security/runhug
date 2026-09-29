# runhug

**Find the best model. Deploy it in minutes. Run it for pennies.**

npm wrapper for the [runhug](https://github.com/openhat-security/runhug) CLI.
`postinstall` downloads the matching GitHub Release binary (macOS / Linux / Windows × amd64 / arm64). Requires Node ≥ 18.

Full docs, demo GIF, and packaging notes: [github.com/openhat-security/runhug](https://github.com/openhat-security/runhug).

## Install

```bash
# npm (this package)
npm install -g runhug
# or: npx runhug wizard

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

# macOS / Linux — direct binary
curl -fsSL https://raw.githubusercontent.com/openhat-security/runhug/main/scripts/install.sh | bash

# Windows — direct binary (PowerShell)
irm https://raw.githubusercontent.com/openhat-security/runhug/main/scripts/install.ps1 | iex
```

## Quick start

```bash
runhug wizard
runhug connect && runhug connect hf
runhug search -q "small instruct llm"
runhug deploy <model> --dry-run --estimate
runhug deploy <model>
runhug start claude          # or: runhug start opencode
```

Plans stay short by default — use `--verbose` / `-v` for cost assumptions and extra detail. `runhug help` / `runhug gcp help` are colorized command lists.
