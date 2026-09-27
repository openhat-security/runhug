#!/usr/bin/env bash
# Add the runhug dnf/yum repo and install runhug.
set -euo pipefail
REPO_URL="${REPO_URL:-@REPO_URL@}"
if [ "$(id -u)" -ne 0 ]; then
  echo "re-run with sudo" >&2
  exit 1
fi
curl -fsSL "${REPO_URL}/runhug.repo" -o /etc/yum.repos.d/runhug.repo
if command -v dnf >/dev/null 2>&1; then
  dnf install -y runhug
elif command -v yum >/dev/null 2>&1; then
  yum install -y runhug
else
  echo "need dnf or yum" >&2
  exit 1
fi
echo "OK: $(runhug version)"
