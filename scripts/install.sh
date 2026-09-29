#!/usr/bin/env bash
# Install the latest runhug release binary from adamsiwiec1/runhug.
# Next release assets: runhug_<ver>_{darwin,linux}_{amd64,arm64}
# Fallback: older tags may still publish runhug-cli_<ver>_… — accepted until cut over.
# Usage: curl -fsSL …/scripts/install.sh | bash
set -euo pipefail

REPO="${RUNHUG_REPO:-openhat-security/runhug}"
# Optional: TAG=v0.1.3 or VERSION=0.1.3 to pin a release (default: latest)
# Prefer new asset names; fall back to pre-rename prefix for older releases.
ASSET_PREFIXES=("runhug_" "runhug-cli_")
BIN_NAME="runhug"

die() { echo "install.sh: $*" >&2; exit 1; }

need() { command -v "$1" >/dev/null 2>&1 || die "need '$1' on PATH"; }

need curl
need uname
need mktemp

os="$(uname -s | tr '[:upper:]' '[:lower:]')"
arch="$(uname -m)"

case "$os" in
  darwin|linux) ;;
  msys*|cygwin*|mingw*)
    die "Windows detected — use scripts/install.ps1 or download the windows_amd64.exe asset"
    ;;
  *)
    die "unsupported OS: $os (supported: darwin, linux)"
    ;;
esac

case "$arch" in
  x86_64|amd64) arch="amd64" ;;
  aarch64|arm64) arch="arm64" ;;
  *) die "unsupported arch: $arch (supported: amd64, arm64)" ;;
esac

if [ -n "${TAG:-}" ]; then
  tag="$TAG"
elif [ -n "${VERSION:-}" ]; then
  tag="v${VERSION#v}"
else
  tag=""
fi

if [ -n "$tag" ]; then
  echo "Using release ${tag} for ${os}/${arch}…"
  api="https://api.github.com/repos/${REPO}/releases/tags/${tag}"
else
  echo "Detecting latest release for ${os}/${arch}…"
  api="https://api.github.com/repos/${REPO}/releases/latest"
fi
json="$(curl -fsSL -H 'Accept: application/vnd.github+json' "$api")" || die "failed to fetch $api"

tag="$(printf '%s' "$json" | sed -n 's/.*"tag_name":[[:space:]]*"\([^"]*\)".*/\1/p' | head -1)"
[ -n "$tag" ] || die "could not parse tag_name from GitHub API"

ver="${tag#v}"

tmpdir="$(mktemp -d)"
trap 'rm -rf "$tmpdir"' EXIT
tmpbin="${tmpdir}/${BIN_NAME}"

asset=""
downloaded=0
for prefix in "${ASSET_PREFIXES[@]}"; do
  asset="${prefix}${ver}_${os}_${arch}"
  url="https://github.com/${REPO}/releases/download/${tag}/${asset}"
  api_url="$(printf '%s' "$json" | sed -n "s/.*\"browser_download_url\":[[:space:]]*\"\\([^\"]*${asset}\\)\"/\1/p" | head -1)"
  [ -n "$api_url" ] && url="$api_url"
  echo "Trying ${url}"
  if curl -fsSL -o "$tmpbin" "$url"; then
    downloaded=1
    break
  fi
done
[ "$downloaded" -eq 1 ] || die "download failed for prefixes ${ASSET_PREFIXES[*]} (tag ${tag})"
chmod +x "$tmpbin"

if [ -w /usr/local/bin ] 2>/dev/null || [ "$(id -u)" -eq 0 ]; then
  dest="/usr/local/bin/${BIN_NAME}"
elif mkdir -p "${HOME}/.local/bin" 2>/dev/null; then
  dest="${HOME}/.local/bin/${BIN_NAME}"
else
  die "no writable install dir (/usr/local/bin or ~/.local/bin)"
fi

if [ "$dest" = "/usr/local/bin/${BIN_NAME}" ] && [ ! -w /usr/local/bin ]; then
  need sudo
  sudo mv "$tmpbin" "$dest"
  sudo chmod +x "$dest"
else
  mv "$tmpbin" "$dest"
  chmod +x "$dest"
fi

# Gatekeeper: unsigned darwin binaries carry com.apple.quarantine from the download.
# Strip it so first launch is not blocked (until we notarize — packaging/macos/README.md).
if [ "$os" = "darwin" ]; then
  if [ -w "$dest" ]; then
    xattr -d com.apple.quarantine "$dest" 2>/dev/null || true
  else
    sudo xattr -d com.apple.quarantine "$dest" 2>/dev/null || true
  fi
fi

echo "Installed ${BIN_NAME} → ${dest} (${tag}, asset ${asset})"
case ":${PATH}:" in
  *":$(dirname "$dest"):"*) ;;
  *)
    echo "Note: $(dirname "$dest") is not on your PATH — add it, e.g.:"
    echo "  export PATH=\"$(dirname "$dest"):\$PATH\""
    ;;
esac

if command -v "$BIN_NAME" >/dev/null 2>&1; then
  "$BIN_NAME" --version 2>/dev/null || "$BIN_NAME" version 2>/dev/null || true
fi
