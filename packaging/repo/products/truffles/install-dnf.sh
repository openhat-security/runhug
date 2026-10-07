#!/usr/bin/env bash
# Add the truffles dnf/yum repo and install truffles.
set -euo pipefail
REPO_URL="${REPO_URL:-@REPO_URL@}"
if [ "$(id -u)" -ne 0 ]; then
  echo "re-run with sudo" >&2
  exit 1
fi
curl -fsSL "${REPO_URL}/truffles.repo" -o /etc/yum.repos.d/truffles.repo
if command -v dnf >/dev/null 2>&1; then
  dnf install -y truffles
elif command -v yum >/dev/null 2>&1; then
  yum install -y truffles
else
  echo "need dnf or yum" >&2
  exit 1
fi
echo "OK: $(truffles version)"
