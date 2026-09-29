#!/usr/bin/env bash
# Cut a SemVer release: bump version.go, roll CHANGELOG Unreleased, commit, tag, push.
# Tag push (v*) triggers .github/workflows/release.yml (GoReleaser + channels).
#
# Usage:
#   ./scripts/release.sh patch|minor|major
#   ./scripts/release.sh patch --dry-run
#   ./scripts/release.sh minor --no-push
#   ./scripts/release.sh 0.1.9              # explicit version still allowed
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

die() { echo "error: $*" >&2; exit 1; }
info() { echo "→ $*"; }

usage() {
  cat <<'EOF'
Cut a SemVer release: bump version.go, roll CHANGELOG Unreleased, commit, tag, push.
Tag push (v*) triggers .github/workflows/release.yml (GoReleaser + channels).

Usage:
  ./scripts/release.sh patch|minor|major   # increment from version.go
  ./scripts/release.sh patch --dry-run
  ./scripts/release.sh minor --no-push     # commit + tag locally only
  ./scripts/release.sh major --skip-tests
  ./scripts/release.sh patch --allow-dirty
  ./scripts/release.sh patch --yes         # skip confirm; allow non-main
  ./scripts/release.sh 0.1.9               # explicit version (optional)
EOF
  exit "${1:-0}"
}

# bump_semver <current> <major|minor|patch>
# Strips any -prerelease / +build, then increments the chosen component.
bump_semver() {
  local cur="$1" kind="$2"
  python3 - "$cur" "$kind" <<'PY'
import re, sys
cur, kind = sys.argv[1], sys.argv[2]
m = re.match(r"^(\d+)\.(\d+)\.(\d+)", cur)
if not m:
    raise SystemExit(f"cannot parse SemVer from {cur!r}")
major, minor, patch = (int(m.group(i)) for i in range(1, 4))
if kind == "major":
    major, minor, patch = major + 1, 0, 0
elif kind == "minor":
    minor, patch = minor + 1, 0
elif kind == "patch":
    patch += 1
else:
    raise SystemExit(f"unknown bump {kind!r}")
print(f"{major}.{minor}.{patch}")
PY
}

BUMP=""
VERSION=""
DRY_RUN=0
NO_PUSH=0
SKIP_TESTS=0
ALLOW_DIRTY=0
YES=0

while [[ $# -gt 0 ]]; do
  case "$1" in
    -h|--help) usage 0 ;;
    --dry-run) DRY_RUN=1; shift ;;
    --no-push) NO_PUSH=1; shift ;;
    --skip-tests) SKIP_TESTS=1; shift ;;
    --allow-dirty) ALLOW_DIRTY=1; shift ;;
    --yes|-y) YES=1; shift ;;
    major|minor|patch)
      [[ -z "$BUMP" && -z "$VERSION" ]] || die "unexpected argument $1"
      BUMP="$1"
      shift
      ;;
    -*) die "unknown flag $1 (see --help)" ;;
    *)
      [[ -z "$BUMP" && -z "$VERSION" ]] || die "unexpected argument $1"
      VERSION="$1"
      shift
      ;;
  esac
done

[[ -n "$BUMP" || -n "$VERSION" ]] || usage 1
[[ -z "$BUMP" || -z "$VERSION" ]] || die "pass either patch|minor|major or an explicit version, not both"

command -v git >/dev/null || die "git required"
command -v python3 >/dev/null || die "python3 required"

current="$(sed -n 's/.*Version = "\([^"]*\)".*/\1/p' internal/version/version.go | head -1)"
[[ -n "$current" ]] || die "could not read Version from internal/version/version.go"

if [[ -n "$BUMP" ]]; then
  VERSION="$(bump_semver "$current" "$BUMP")"
else
  VERSION="${VERSION#v}"
  [[ "$VERSION" =~ ^[0-9]+\.[0-9]+\.[0-9]+([.-][0-9A-Za-z.-]+)?$ ]] \
    || die "version must look like 0.1.9 or 0.1.9-beta.1 (got $VERSION)"
fi

TAG="v${VERSION}"
DATE="$(date +%Y-%m-%d)"

branch="$(git rev-parse --abbrev-ref HEAD)"
if [[ "$branch" != "main" && "$branch" != "master" ]]; then
  if [[ "$YES" -eq 1 ]]; then
    echo "warning: releasing from branch $branch (not main)" >&2
  else
    die "must be on main (on $branch). Merge first, or pass --yes to override"
  fi
fi

if [[ "$ALLOW_DIRTY" -eq 0 ]]; then
  if ! git diff --quiet || ! git diff --cached --quiet; then
    die "working tree dirty — commit/stash first, or pass --allow-dirty"
  fi
  untracked="$(git ls-files --others --exclude-standard)"
  if [[ -n "$untracked" ]]; then
    die "untracked files present — clean up, or pass --allow-dirty"
  fi
fi

if git rev-parse "$TAG" >/dev/null 2>&1; then
  die "tag $TAG already exists locally"
fi
if git ls-remote --exit-code --tags origin "refs/tags/${TAG}" >/dev/null 2>&1; then
  die "tag $TAG already exists on origin"
fi

if ! grep -q '^## \[Unreleased\]' CHANGELOG.md; then
  die "CHANGELOG.md missing ## [Unreleased] heading"
fi

# Fail early if Unreleased has no bullets (only blank / whitespace lines).
unreleased_body="$(python3 - <<'PY'
from pathlib import Path
text = Path("CHANGELOG.md").read_text()
start = text.find("## [Unreleased]")
if start < 0:
    raise SystemExit("no Unreleased")
rest = text[start + len("## [Unreleased]"):]
nxt = rest.find("\n## [")
body = rest if nxt < 0 else rest[:nxt]
lines = body.splitlines()
while lines and not lines[0].strip():
    lines.pop(0)
while lines and not lines[-1].strip():
    lines.pop()
print("\n".join(lines))
PY
)"
if [[ -z "${unreleased_body// }" ]]; then
  die "CHANGELOG ## [Unreleased] is empty — add bullets before releasing"
fi

actions="bump version.go, roll CHANGELOG, commit, tag"
[[ "$NO_PUSH" -eq 1 ]] && actions+=" (no push)"
[[ "$DRY_RUN" -eq 1 ]] && actions+=" [dry-run]"
echo "Release plan"
echo "  current : v${current}"
if [[ -n "$BUMP" ]]; then
  echo "  bump    : ${BUMP} → ${TAG} (${DATE})"
else
  echo "  new     : ${TAG} (${DATE})"
fi
echo "  branch  : ${branch}"
echo "  remote  : origin → $(git remote get-url origin 2>/dev/null || echo '?')"
echo "  actions : ${actions}"
echo

if [[ "$YES" -eq 0 && "$DRY_RUN" -eq 0 ]]; then
  printf "Proceed? [y/N] "
  read -r ans
  case "$ans" in
    y|Y|yes|YES) ;;
    *) die "aborted" ;;
  esac
fi

if [[ "$SKIP_TESTS" -eq 0 ]]; then
  info "go test ./..."
  if [[ "$DRY_RUN" -eq 0 ]]; then
    go test ./...
  else
    echo "  (skipped in --dry-run)"
  fi
fi

bump_files() {
  python3 - "$VERSION" <<'PY'
import pathlib, re, sys
ver = sys.argv[1]
path = pathlib.Path("internal/version/version.go")
text = path.read_text()
new, n = re.subn(
    r'(Version\s*=\s*")[^"]*(")',
    rf'\g<1>{ver}\2',
    text,
    count=1,
)
if n != 1:
    raise SystemExit(f"expected 1 Version assignment, got {n}")
path.write_text(new)
print(f"updated {path} → {ver}")
PY

  python3 - "$VERSION" "$DATE" <<'PY'
import pathlib, sys
ver, date = sys.argv[1], sys.argv[2]
path = pathlib.Path("CHANGELOG.md")
text = path.read_text()
marker = "## [Unreleased]"
start = text.find(marker)
if start < 0:
    raise SystemExit("## [Unreleased] not found")
rest = text[start + len(marker) :]
nxt = rest.find("\n## [")
if nxt < 0:
    body, tail = rest, ""
else:
    body, tail = rest[:nxt], rest[nxt:]
body_stripped = body.lstrip("\n").rstrip() + "\n"
out = text[:start] + f"{marker}\n\n## [{ver}] - {date}\n\n{body_stripped}"
if tail.startswith("\n"):
    out += tail
elif tail:
    out += "\n" + tail
else:
    out += "\n"
path.write_text(out)
print(f"rolled CHANGELOG Unreleased → [{ver}] - {date}")
PY
}

if [[ "$DRY_RUN" -eq 1 ]]; then
  info "dry-run: would update files, commit, and tag ${TAG}"
  echo "--- Unreleased body that would become [${VERSION}] ---"
  printf '%s\n' "$unreleased_body"
  echo "---"
  exit 0
fi

bump_files

msg="release ${TAG}"
info "git commit + tag ${TAG}"
git add internal/version/version.go CHANGELOG.md
git commit -m "$(cat <<EOF
${msg}

EOF
)"
git tag -a "$TAG" -m "$msg"

if [[ "$NO_PUSH" -eq 1 ]]; then
  echo "OK ${TAG} created locally (not pushed). Push with:"
  echo "  git push origin HEAD && git push origin ${TAG}"
  exit 0
fi

info "git push origin HEAD && git push origin ${TAG}"
git push origin HEAD
git push origin "$TAG"

echo
echo "OK ${TAG} pushed — GitHub Actions Release workflow should start now."
echo "  https://github.com/openhat-security/runhug/actions/workflows/release.yml"
echo "  https://github.com/openhat-security/runhug/releases/tag/${TAG}"
