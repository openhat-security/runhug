#!/usr/bin/env bash
# Add the OpenHat apt repo and install truffles.
set -euo pipefail
REPO_URL="${REPO_URL:-@REPO_URL@}"
if [ "$(id -u)" -ne 0 ]; then
  echo "re-run with sudo" >&2
  exit 1
fi
curl -fsSL "${REPO_URL}/truffles.list" -o /etc/apt/sources.list.d/truffles.list
if curl -fsSL "${REPO_URL}/truffles.asc" -o /usr/share/keyrings/truffles.asc 2>/dev/null; then
  sed -i 's/\[trusted=yes\]/[signed-by=\/usr\/share\/keyrings\/truffles.asc]/' /etc/apt/sources.list.d/truffles.list || true
fi
apt-get update -qq
apt-get install -y truffles
echo "OK: $(truffles version)"
