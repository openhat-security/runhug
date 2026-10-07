.PHONY: help build install test test-heretic tidy check build-release release-snapshot release demo-vhs demo-vhs-all

MODULE := github.com/adamsiwiec1/runhug
# Source SemVer (strip a trailing -localN if present).
VERSION := $(shell sed -n 's/.*Version = "\([^"]*\)".*/\1/p' internal/version/version.go | head -1)

# TAPE=full|quickstart|search|deploy|heretic|run|chat (default: full)
TAPE ?= full

help:
	@echo "build              Go binary → bin/runhug"
	@echo "install            go install to GOBIN with {version}-localN (N auto-increments)"
	@echo "test               go test ./..."
	@echo "test-heretic       unit tests for the heretic pod container scripts"
	@echo "tidy               go mod tidy"
	@echo "build-release      Alias for release-snapshot"
	@echo "release-snapshot   GoReleaser snapshot (or local cross-build fallback)"
	@echo "release BUMP=patch  Cut release: bump patch|minor|major, tag, push"
	@echo "demo-vhs           Record one VHS tape (TAPE=full|quickstart|search|deploy|heretic|run|chat)"
	@echo "demo-vhs-all       Record every tape under assets/*.tape"
	@echo "check              tests + vet + build"
	@echo ""
	@echo "Index packs are built/published by https://github.com/openhat-security/hfpacks"
	@echo "Install them with: runhug packs install / runhug update --packs"

build:
	CGO_ENABLED=0 go build -trimpath -ldflags "-X $(MODULE)/internal/version.Version=$(VERSION)" -o bin/runhug ./cmd/runhug

# go install → $(go env GOBIN or GOPATH/bin). Version is {semver}-localN, where
# N is one past the last -local suffix already stamped into that destination binary.
# If Homebrew (or anything else) shadows GOBIN on PATH, also copy to ~/.local/bin.
install:
	@base="$(VERSION)"; \
	base="$${base%%-local*}"; \
	gobin="$$(go env GOBIN)"; \
	[ -n "$$gobin" ] || gobin="$$(go env GOPATH)/bin"; \
	bin="$$gobin/runhug"; \
	n=1; \
	if [ -x "$$bin" ]; then \
	  cur="$$(go version -m "$$bin" 2>/dev/null | sed -n 's/.*internal\/version\.Version=\([^" ]*\).*/\1/p' | head -1)"; \
	  case "$$cur" in \
	    "$$base"-local[0-9]*) n=$$(( $${cur##*-local} + 1 ));; \
	  esac; \
	fi; \
	ver="$$base-local$$n"; \
	echo "→ go install $(MODULE)/cmd/runhug ($$ver) → $$bin"; \
	CGO_ENABLED=0 go install -trimpath -ldflags "-X $(MODULE)/internal/version.Version=$$ver" ./cmd/runhug; \
	first="$$(command -v runhug 2>/dev/null || true)"; \
	if [ -n "$$first" ] && [ "$$first" != "$$bin" ]; then \
	  mkdir -p "$$HOME/.local/bin"; \
	  cp "$$bin" "$$HOME/.local/bin/runhug"; \
	  echo "→ also $$HOME/.local/bin/runhug (PATH currently hits $$first first)"; \
	fi

test:
	go test ./...

test-heretic:
	go test ./internal/cli/ -run 'Heretic|Dashboard|PoolGPUName|DateTimeFormat' -count=1
	python3 -m unittest discover -s containers/heretic/tests -p 'test_run_pipeline.py'

tidy:
	go mod tidy

check: test
	go vet ./...
	$(MAKE) build

build-release release-snapshot:
	bash scripts/build-release.sh

# Record assets/screenshots from assets/$(TAPE).tape (needs: brew install vhs).
# Unset NO_COLOR / force a real TERM so runhug ANSI colors render in the GIF/mp4
# (agent/CI shells often export NO_COLOR=1 and TERM=dumb).
demo-vhs: build
	@command -v vhs >/dev/null || (echo "install vhs: brew install vhs" >&2; exit 1)
	@test -f "assets/$(TAPE).tape" || (echo "missing assets/$(TAPE).tape" >&2; exit 1)
	mkdir -p assets/screenshots
	env -u NO_COLOR TERM=xterm-256color COLORTERM=truecolor CLICOLOR_FORCE=1 vhs "assets/$(TAPE).tape"

demo-vhs-all: build
	@command -v vhs >/dev/null || (echo "install vhs: brew install vhs" >&2; exit 1)
	mkdir -p assets/screenshots
	@for t in quickstart search deploy heretic run chat full; do \
		echo "→ vhs assets/$$t.tape"; \
		env -u NO_COLOR TERM=xterm-256color COLORTERM=truecolor CLICOLOR_FORCE=1 vhs "assets/$$t.tape"; \
	done

# Example: make release BUMP=patch
# Extra flags: make release BUMP=minor ARGS='--dry-run'
release:
	@test -n "$(BUMP)" || (echo "usage: make release BUMP=patch|minor|major [ARGS='--dry-run']" >&2; exit 1)
	bash scripts/release.sh "$(BUMP)" $(ARGS)
