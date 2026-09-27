#!/usr/bin/env bash
# Thin wrapper: prefer GoReleaser snapshots; fall back to a local cross-build.
# Usage: ./scripts/build-release.sh [version]
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

ver="${1:-}"
if [ -z "$ver" ]; then
  ver="$(sed -n 's/.*Version = "\([^"]*\)".*/\1/p' internal/version/version.go | head -1)"
fi
ver="${ver#v}"

if command -v goreleaser >/dev/null 2>&1; then
  echo "→ goreleaser release --snapshot --clean (version hint ${ver})"
  exec goreleaser release --snapshot --clean
fi

echo "goreleaser not found — local CGO_ENABLED=0 cross-build → dist/release/" >&2
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
( cd "$out" && shasum -a 256 runhug_"${ver}"_* > SHA256SUMS.txt )
echo "OK dist/release (v${ver}) — install goreleaser for full packages (deb/rpm)"
ls -la "$out"/runhug_"${ver}"_* "$out"/SHA256SUMS.txt
