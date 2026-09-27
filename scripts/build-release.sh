#!/usr/bin/env bash
# Cross-compile runhug release binaries into dist/release/.
# Usage: ./scripts/build-release.sh [version]
# Version defaults to internal/version/version.go (no leading v).
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

ver="${1:-}"
if [ -z "$ver" ]; then
  ver="$(sed -n 's/.*Version = "\([^"]*\)".*/\1/p' internal/version/version.go | head -1)"
fi
ver="${ver#v}"
[ -n "$ver" ] || { echo "build-release: could not resolve version" >&2; exit 1; }

out="$ROOT/dist/release"
mkdir -p "$out"
rm -f "$out"/runhug_"${ver}"_* "$out"/SHA256SUMS.txt 2>/dev/null || true

export CGO_ENABLED=0
ldflags="-s -w -X github.com/adamsiwiec1/runhug/internal/version.Version=${ver}"

build_one() {
  local goos="$1" goarch="$2" ext="${3:-}"
  local name="runhug_${ver}_${goos}_${goarch}${ext}"
  echo "→ $name"
  GOOS="$goos" GOARCH="$goarch" go build -trimpath -ldflags "$ldflags" -o "$out/$name" ./cmd/runhug
}

build_one darwin amd64
build_one darwin arm64
build_one linux amd64
build_one linux arm64
build_one windows amd64 .exe

(
  cd "$out"
  shasum -a 256 runhug_"${ver}"_* > SHA256SUMS.txt
)

echo "OK dist/release (v${ver})"
ls -la "$out"/runhug_"${ver}"_* "$out"/SHA256SUMS.txt
