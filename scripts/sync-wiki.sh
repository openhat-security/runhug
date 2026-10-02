#!/usr/bin/env bash
# Push wiki/*.md to the GitHub Wiki remote (openhat-security/runhug.wiki).
# Prerequisite: create the first page once in the GitHub UI so the .wiki.git remote exists:
#   https://github.com/openhat-security/runhug/wiki/_new
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
SRC="$ROOT/wiki"
TMP="$(mktemp -d)"
cleanup() { rm -rf "$TMP"; }
trap cleanup EXIT

test -d "$SRC" || { echo "missing $SRC" >&2; exit 1; }

TOKEN="$(gh auth token)"
REMOTE="https://x-access-token:${TOKEN}@github.com/openhat-security/runhug.wiki.git"

if ! git ls-remote "$REMOTE" HEAD >/dev/null 2>&1; then
  echo "Wiki remote does not exist yet." >&2
  echo "Open https://github.com/openhat-security/runhug/wiki/_new" >&2
  echo "Create a page titled Home (any content), Save, then re-run:" >&2
  echo "  ./scripts/sync-wiki.sh" >&2
  exit 1
fi

git clone --depth 1 "$REMOTE" "$TMP/wiki"
rsync -a --delete --exclude .git "$SRC/" "$TMP/wiki/"
cd "$TMP/wiki"
git add -A
if git diff --cached --quiet; then
  echo "Wiki already up to date."
  exit 0
fi
git -c user.email='41898282+github-actions[bot]@users.noreply.github.com' \
    -c user.name='runhug-bot' \
    commit -m "Sync wiki from runhug repo"
git push origin HEAD
echo "OK → https://github.com/openhat-security/runhug/wiki"
