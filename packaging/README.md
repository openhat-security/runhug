# Packaging

Release channels for runhug. Tagged releases (`v*`) are built by GoReleaser
(`.goreleaser.yaml` + `.github/workflows/release.yml`).

| Channel | How users install | Source |
|--------|-------------------|--------|
| GitHub Release | `scripts/install.sh` / `install.ps1` | bare binaries + `.deb` / `.rpm` |
| Homebrew | `brew install --cask openhat-security/tap/runhug` | `openhat-security/homebrew-tap` (Cask) |
| Homebrew (hfpacks) | `brew install --cask openhat-security/tap/hfpacks` | pack builder CLI (separate repo) |
| apt | `curl …/install-apt.sh \| sudo bash` | Pages repo `openhat-security/packages` |
| dnf | `curl …/install-dnf.sh \| sudo bash` | same Pages repo |
| pacman / AUR | `yay -S runhug-bin` | [`aur/`](aur/) (publish to AUR once) |
| Scoop | `scoop bucket add openhat https://github.com/openhat-security/scoop-bucket` then `scoop install runhug` | `openhat-security/scoop-bucket` |
| winget | `winget install OpenHatSecurity.Runhug` (after PR merges) | [`winget/`](winget/) |
| npm | `npm i -g runhug` | [`npm/`](npm/) |

## Layout

```
packaging/
  npm/           npm wrapper (postinstall downloads GitHub Release binary)
  aur/           Arch PKGBUILD for AUR
  winget/        winget manifest generator + per-version output
  repo/          apt/dnf Pages templates + build-pages-repos.sh
```

## Org repos (create once)

```bash
gh repo create openhat-security/homebrew-tap --public --description "Homebrew tap for OpenHat Security CLIs" --add-readme
gh repo create openhat-security/scoop-bucket --public --description "Scoop bucket for OpenHat Security CLIs" --add-readme
gh repo create openhat-security/packages --public --description "apt + dnf repos for OpenHat Security (GitHub Pages)" --add-readme
# Settings → Pages → Deploy from branch main / (root)
```

## Secrets (on `openhat-security/runhug`)

| Secret | Required for |
|--------|----------------|
| `NPM_TOKEN` | `npm publish` from `packaging/npm` (required for npm channel) |
| `PACKAGING_TOKEN` | Push `homebrew-tap`, `scoop-bucket`, and `packages` (apt/dnf Pages) |
| `HOMEBREW_TAP_TOKEN` / `SCOOP_TOKEN` | Optional overrides instead of `PACKAGING_TOKEN` |
| `WINGET_PAT` | Auto-PR to `microsoft/winget-pkgs` |
| `GPG_PRIVATE_KEY` | Optional signing for apt InRelease (unset = `trusted=yes`) |

`PACKAGING_TOKEN` should be a fine-grained PAT (or classic) with **contents: write** on `homebrew-tap`, `scoop-bucket`, and `packages`. Without it, GoReleaser still publishes the GitHub Release; brew/scoop upload is skipped (do **not** fall back to `GITHUB_TOKEN` — it cannot write other repos).

To re-publish npm for an existing tag: Actions → Release → Run workflow → set tag `vX.Y.Z` with **npm_only**.

## Cut a release

From a clean `main` (after Unreleased changelog bullets exist):

```bash
./scripts/release.sh patch            # 0.1.7 → 0.1.8
./scripts/release.sh minor            # 0.1.7 → 0.2.0
./scripts/release.sh major            # 0.1.7 → 1.0.0
./scripts/release.sh patch --dry-run
make release BUMP=patch
```

Pushing `v*` runs `.github/workflows/release.yml`. Use `--no-push` to tag locally only.
An explicit version (`./scripts/release.sh 0.1.9`) still works if you need to override.

## Local snapshot

```bash
make release-snapshot   # goreleaser --snapshot, or fallback cross-build
```
