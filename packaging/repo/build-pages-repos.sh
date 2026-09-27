#!/usr/bin/env bash
# Build apt + dnf repository trees from a directory of .deb/.rpm files.
# Usage: build-pages-repos.sh <pkg-dir> <out-dir>
# Unsigned by default (trusted=yes / gpgcheck=0). Set GPG_PRIVATE_KEY to sign.
set -euo pipefail

PKG_DIR="$(cd "${1:?pkg dir}" && pwd)"
OUT_RAW="${2:?out dir}"
REPO_URL="${REPO_URL:-https://openhat-security.github.io/packages}"

rm -rf "$OUT_RAW"
mkdir -p "$OUT_RAW/deb/pool/main" "$OUT_RAW/deb/dists/stable/main/binary-amd64" "$OUT_RAW/deb/dists/stable/main/binary-arm64" "$OUT_RAW/rpm"
OUT="$(cd "$OUT_RAW" && pwd)"

# --- apt (flat-ish pool + dists) ---
shopt -s nullglob
debs=("$PKG_DIR"/*.deb)
if ((${#debs[@]})); then
  cp -a "${debs[@]}" "$OUT/deb/pool/main/"
  build_apt_meta() {
    pushd "$OUT/deb" >/dev/null
    for arch in amd64 arm64; do
      mkdir -p "dists/stable/main/binary-${arch}"
      apt-ftparchive --arch "$arch" packages pool/main > "dists/stable/main/binary-${arch}/Packages" || true
      gzip -9fk "dists/stable/main/binary-${arch}/Packages" || true
    done
    apt-ftparchive release dists/stable > dists/stable/Release
    popd >/dev/null
  }
  if command -v apt-ftparchive >/dev/null 2>&1; then
    build_apt_meta
  elif command -v docker >/dev/null 2>&1; then
    docker run --rm -v "$OUT/deb:/deb" -w /deb debian:bookworm-slim bash -lc '
      apt-get update -qq && apt-get install -y -qq apt-utils gzip >/dev/null
      for arch in amd64 arm64; do
        mkdir -p "dists/stable/main/binary-${arch}"
        apt-ftparchive --arch "$arch" packages pool/main > "dists/stable/main/binary-${arch}/Packages" || true
        gzip -9fk "dists/stable/main/binary-${arch}/Packages" || true
      done
      apt-ftparchive release dists/stable > dists/stable/Release
    '
  else
    echo "warn: apt-ftparchive unavailable — deb pool copied without Packages index" >&2
  fi
  if [ -n "${GPG_PRIVATE_KEY:-}" ] && command -v gpg >/dev/null 2>&1; then
    gnupg_home="$(mktemp -d)"
    export GNUPGHOME="$gnupg_home"
    printf '%s\n' "$GPG_PRIVATE_KEY" | gpg --batch --import
    gpg --batch --yes --clearsign -o "$OUT/deb/dists/stable/InRelease" "$OUT/deb/dists/stable/Release"
    gpg --batch --yes -abs -o "$OUT/deb/dists/stable/Release.gpg" "$OUT/deb/dists/stable/Release"
    gpg --batch --export --armor > "$OUT/runhug.asc"
  fi
fi

# --- rpm ---
rpms=("$PKG_DIR"/*.rpm)
if ((${#rpms[@]})); then
  cp -a "${rpms[@]}" "$OUT/rpm/"
  if command -v createrepo_c >/dev/null 2>&1; then
    createrepo_c "$OUT/rpm"
  elif command -v docker >/dev/null 2>&1; then
    docker run --rm -v "$OUT/rpm:/repo" fedora:latest bash -lc 'dnf install -y -q createrepo_c >/dev/null && createrepo_c /repo'
  else
    echo "warn: createrepo_c unavailable — rpm tree has packages but no repodata" >&2
  fi
fi

# Client snippets + index
ROOT="$(cd "$(dirname "$0")" && pwd)"
sed "s#@REPO_URL@#${REPO_URL}#g" "$ROOT/index.html.in" > "$OUT/index.html"
sed "s#@REPO_URL@#${REPO_URL}#g" "$ROOT/apt/sources.list.in" > "$OUT/runhug.list"
sed "s#@REPO_URL@#${REPO_URL}#g" "$ROOT/rpm/runhug.repo.in" > "$OUT/runhug.repo"
sed "s#@REPO_URL@#${REPO_URL}#g" "$ROOT/install-apt.sh" > "$OUT/install-apt.sh"
sed "s#@REPO_URL@#${REPO_URL}#g" "$ROOT/install-dnf.sh" > "$OUT/install-dnf.sh"
chmod +x "$OUT/install-apt.sh" "$OUT/install-dnf.sh"
touch "$OUT/.nojekyll"

echo "OK pages tree → $OUT"
find "$OUT" -type f | sort | head -80
