.PHONY: help build test test-heretic tidy check build-index-packs build-release release-snapshot release

help:
	@echo "build              Go binary → bin/runhug"
	@echo "test               go test ./..."
	@echo "test-heretic       unit tests for the heretic pod container scripts"
	@echo "tidy               go mod tidy"
	@echo "build-index-packs  Category SQLite packs → dist/index (needs HF_TOKEN)"
	@echo "build-release      Alias for release-snapshot"
	@echo "release-snapshot   GoReleaser snapshot (or local cross-build fallback)"
	@echo "release BUMP=patch  Cut release: bump patch|minor|major, tag, push"
	@echo "check              tests + vet + build"

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

build-index-packs:
	mkdir -p dist/index
	go run ./cmd/build-index-packs --out dist/index

build-release release-snapshot:
	bash scripts/build-release.sh

# Example: make release BUMP=patch
# Extra flags: make release BUMP=minor ARGS='--dry-run'
release:
	@test -n "$(BUMP)" || (echo "usage: make release BUMP=patch|minor|major [ARGS='--dry-run']" >&2; exit 1)
	bash scripts/release.sh "$(BUMP)" $(ARGS)
