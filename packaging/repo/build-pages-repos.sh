#!/usr/bin/env bash
# Build or update apt + dnf repository trees for GitHub Pages (openhat-security/packages).
#
# Usage:
#   build-pages-repos.sh [--merge] --product <name> <pkg-dir> <out-dir>
#
#   --merge   Add .deb/.rpm to an existing site tree (multi-product). Without it,
#             deb/ and rpm/ under out-dir are rebuilt from scratch (client snippets
#             for other products under out-dir are preserved when --merge).
#
# Env:
#   REPO_URL          Base URL (default https://openhat-security.github.io/packages)
#   GPG_PRIVATE_KEY   Optional; signs apt Release → InRelease and writes <product>.asc
#
set -euo pipefail

MERGE=0
PRODUCT=""
while [ $# -gt 0 ]; do
  case "$1" in
    --merge)
      MERGE=1
      shift
      ;;
    --product)
      PRODUCT="${2:?product name}"
      shift 2
      ;;
    *)
      break
      ;;
  esac
done

PKG_DIR="$(cd "${1:?pkg dir}" && pwd)"
OUT_RAW="${2:?out dir}"
REPO_URL="${REPO_URL:-https://openhat-security.github.io/packages}"

if [ -z "$PRODUCT" ]; then
  echo "error: --product <name> is required (e.g. truffles, runhug)" >&2
  exit 1
fi

ROOT="$(cd "$(dirname "$0")" && pwd)"
PROD_ROOT="$ROOT/products/$PRODUCT"
if [ ! -d "$PROD_ROOT" ]; then
  echo "error: missing templates at packaging/repo/products/$PRODUCT" >&2
  exit 1
fi

mkdir -p "$OUT_RAW"
OUT="$(cd "$OUT_RAW" && pwd)"

if [ "$MERGE" -eq 0 ]; then
  rm -rf "$OUT/deb" "$OUT/rpm"
fi

mkdir -p "$OUT/deb/pool/main" "$OUT/deb/dists/stable/main/binary-amd64" "$OUT/deb/dists/stable/main/binary-arm64" "$OUT/rpm"

shopt -s nullglob
debs=("$PKG_DIR"/*.deb)
if ((${#debs[@]})); then
  cp -a "${debs[@]}" "$OUT/deb/pool/main/"
fi

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

if ((${#debs[@]})) || { [ -d "$OUT/deb/pool/main" ] && compgen -G "$OUT/deb/pool/main/*.deb" >/dev/null; }; then
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
    gpg --batch --export --armor > "$OUT/${PRODUCT}.asc"
  fi
fi

rpms=("$PKG_DIR"/*.rpm)
if ((${#rpms[@]})); then
  cp -a "${rpms[@]}" "$OUT/rpm/"
fi

if ((${#rpms[@]})) || { [ -d "$OUT/rpm" ] && compgen -G "$OUT/rpm/*.rpm" >/dev/null; }; then
  if command -v createrepo_c >/dev/null 2>&1; then
    createrepo_c "$OUT/rpm"
  elif command -v docker >/dev/null 2>&1; then
    docker run --rm -v "$OUT/rpm:/repo" fedora:latest bash -lc 'dnf install -y -q createrepo_c >/dev/null && createrepo_c /repo'
  else
    echo "warn: createrepo_c unavailable — rpm tree has packages but no repodata" >&2
  fi
fi

sed "s#@REPO_URL@#${REPO_URL}#g" "$PROD_ROOT/apt/sources.list.in" > "$OUT/${PRODUCT}.list"
sed "s#@REPO_URL@#${REPO_URL}#g" "$PROD_ROOT/rpm/${PRODUCT}.repo.in" > "$OUT/${PRODUCT}.repo"
sed "s#@REPO_URL@#${REPO_URL}#g" "$PROD_ROOT/install-apt.sh" > "$OUT/install-apt-${PRODUCT}.sh"
sed "s#@REPO_URL@#${REPO_URL}#g" "$PROD_ROOT/install-dnf.sh" > "$OUT/install-dnf-${PRODUCT}.sh"
chmod +x "$OUT/install-apt-${PRODUCT}.sh" "$OUT/install-dnf-${PRODUCT}.sh"

# Legacy runhug filenames (unchanged URLs on the live site).
if [ "$PRODUCT" = runhug ]; then
  cp -a "$OUT/install-apt-runhug.sh" "$OUT/install-apt.sh"
  cp -a "$OUT/install-dnf-runhug.sh" "$OUT/install-dnf.sh"
fi

sed "s#@REPO_URL@#${REPO_URL}#g" "$ROOT/index.html.in" > "$OUT/index.html"
touch "$OUT/.nojekyll"

echo "OK pages tree (product=$PRODUCT merge=$MERGE) → $OUT"
find "$OUT" -type f | sort | head -80
