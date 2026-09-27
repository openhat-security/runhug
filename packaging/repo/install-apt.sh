#!/usr/bin/env bash
# Add the runhug apt repo and install runhug.
set -euo pipefail
REPO_URL="${REPO_URL:-@REPO_URL@}"
if [ "$(id -u)" -ne 0 ]; then
  echo "re-run with sudo" >&2
  exit 1
fi
curl -fsSL "${REPO_URL}/runhug.list" -o /etc/apt/sources.list.d/runhug.list
# Optional GPG key when present
if curl -fsSL "${REPO_URL}/runhug.asc" -o /usr/share/keyrings/runhug.asc 2>/dev/null; then
  sed -i 's/\[trusted=yes\]/[signed-by=\/usr\/share\/keyrings\/runhug.asc]/' /etc/apt/sources.list.d/runhug.list || true
fi
apt-get update -qq
apt-get install -y runhug
echo "OK: $(runhug version)"
