.PHONY: help build test test-heretic tidy check build-release release-snapshot release demo-vhs

help:
	@echo "build              Go binary → bin/runhug"
	@echo "test               go test ./..."
	@echo "test-heretic       unit tests for the heretic pod container scripts"
	@echo "tidy               go mod tidy"
	@echo "build-release      Alias for release-snapshot"
	@echo "release-snapshot   GoReleaser snapshot (or local cross-build fallback)"
	@echo "release BUMP=patch  Cut release: bump patch|minor|major, tag, push"
	@echo "demo-vhs           Record demo GIF+MP4 via charmbracelet/vhs"
	@echo "check              tests + vet + build"
	@echo ""
	@echo "Index packs are built/published by https://github.com/openhat-security/hfpacks"
	@echo "Install them with: runhug packs install / runhug update --packs"

build:
	go build -o bin/runhug ./cmd/runhug

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

# Record assets/screenshots/runhug-demo.{gif,mp4} (needs: brew install vhs).
demo-vhs: build
	@command -v vhs >/dev/null || (echo "install vhs: brew install vhs" >&2; exit 1)
	mkdir -p assets/screenshots
	vhs assets/runhug.tape

# Example: make release BUMP=patch
# Extra flags: make release BUMP=minor ARGS='--dry-run'
release:
	@test -n "$(BUMP)" || (echo "usage: make release BUMP=patch|minor|major [ARGS='--dry-run']" >&2; exit 1)
	bash scripts/release.sh "$(BUMP)" $(ARGS)
