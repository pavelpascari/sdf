BINARY   := sdf
MODULE   := github.com/pavelpascari/sdf
VERSION  ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS  := -s -w -X main.version=$(VERSION)
BUILD    := go build -ldflags "$(LDFLAGS)" -trimpath

# Default: build for current platform
.PHONY: build
build:
	$(BUILD) -o bin/$(BINARY) .

# Build with spy recording enabled (for E2E tests)
.PHONY: build-spy
build-spy:
	$(BUILD) -tags spyrecord -o bin/$(BINARY) .

# Run the binary
.PHONY: run
run: build
	./bin/$(BINARY) $(ARGS)

# ── Testing ───────────────────────────────────────────────────────

# Run all tests (unit + property + golden, no network needed)
.PHONY: test
test:
	go test -count=1 ./...

# Unit tests only (skip property-based tests)
.PHONY: test-unit
test-unit:
	go test -count=1 -skip TestProperty ./...

# Property-based invariant tests
.PHONY: test-property
test-property:
	go test -count=1 -run TestProperty ./...

# Golden file snapshot tests
.PHONY: test-golden
test-golden:
	go test -count=1 -run 'Golden|TestBuildStackNav|TestReplaceStackNav|TestReplaceDescription' ./cmd/...

# Regenerate golden files after intentional output changes
.PHONY: test-golden-update
test-golden-update:
	go test -count=1 -run 'Golden|TestBuildStackNav|TestReplaceStackNav|TestReplaceDescription' ./cmd -args -update

# E2E tests against a real GitHub repo (requires SDF_E2E_REPO and GH_TOKEN)
.PHONY: test-e2e
test-e2e: build-spy
	go test -tags e2e -v -count=1 -timeout 10m ./e2e/...

# Run vet and static checks
.PHONY: vet
vet:
	go vet ./...

# Run golangci-lint (install: https://golangci-lint.run/docs/install/)
.PHONY: lint
lint:
	golangci-lint run

# Run govulncheck for known dependency vulnerabilities
.PHONY: vulncheck
vulncheck:
	govulncheck ./...

# Format code
.PHONY: fmt
fmt:
	gofmt -s -w .

# Check that go.mod is tidy
.PHONY: mod-tidy-check
mod-tidy-check:
	go mod tidy
	@git diff --exit-code go.mod go.sum || (echo "ERROR: go.mod is not tidy. Run 'go mod tidy' and commit." && exit 1)

# Clean build artifacts
.PHONY: clean
clean:
	rm -rf bin/ dist/

# ── Cross-compilation ──────────────────────────────────────────────

PLATFORMS := \
	linux/amd64 \
	linux/arm64 \
	darwin/amd64 \
	darwin/arm64 \
	windows/amd64 \
	windows/arm64

.PHONY: dist
dist: clean
	@mkdir -p dist
	@for platform in $(PLATFORMS); do \
		os=$${platform%/*}; \
		arch=$${platform#*/}; \
		output="dist/$(BINARY)-$${os}-$${arch}"; \
		if [ "$$os" = "windows" ]; then output="$${output}.exe"; fi; \
		echo "Building $$os/$$arch → $$output"; \
		GOOS=$$os GOARCH=$$arch $(BUILD) -o $$output . || exit 1; \
	done
	@echo "\nAll binaries built in dist/"
	@ls -lh dist/

# Build a single platform: make build-for OS=linux ARCH=arm64
.PHONY: build-for
build-for:
	@test -n "$(OS)" || (echo "usage: make build-for OS=<os> ARCH=<arch>" && exit 1)
	@test -n "$(ARCH)" || (echo "usage: make build-for OS=<os> ARCH=<arch>" && exit 1)
	@mkdir -p bin
	GOOS=$(OS) GOARCH=$(ARCH) $(BUILD) -o bin/$(BINARY)-$(OS)-$(ARCH) .

# Install to GOPATH/bin
.PHONY: install
install:
	go install -ldflags "$(LDFLAGS)" .

.PHONY: help
help:
	@echo "sdf build targets:"
	@echo "  make build      Build for current platform → bin/sdf"
	@echo "  make build-spy  Build with spy recording (for E2E tests)"
	@echo "  make dist       Cross-compile for all platforms → dist/"
	@echo "  make build-for OS=linux ARCH=arm64"
	@echo "                  Build for a specific platform → bin/sdf-<os>-<arch>"
	@echo "  make install    Install to GOPATH/bin"
	@echo "  make test              Run all tests (unit + property + golden)"
	@echo "  make test-unit         Unit tests only (skip property-based)"
	@echo "  make test-property     Property-based invariant tests"
	@echo "  make test-golden       Golden file snapshot tests"
	@echo "  make test-golden-update  Regenerate golden files"
	@echo "  make test-e2e          E2E tests (needs SDF_E2E_REPO + GH_TOKEN)"
	@echo "  make vet               Run go vet"
	@echo "  make lint              Run golangci-lint"
	@echo "  make vulncheck         Check for known vulnerabilities"
	@echo "  make fmt               Format code"
	@echo "  make mod-tidy-check    Verify go.mod is tidy"
	@echo "  make clean             Remove build artifacts"
	@echo "  make blog-check        Verify dateModified on changed blog posts"
	@echo "  make docs              Generate CLI reference JSON"
	@echo "  make docs-check        Check docs freshness and validate references"
	@echo "  make release-checklist Verify blog content exists for the release"

# ── Documentation ─────────────────────────────────────────────────

# Generate CLI reference JSON from Cobra command tree
.PHONY: docs
docs:
	go run ./cmd/docgen > www/src/data/cli-reference.json

# Check that generated docs are fresh and narrative references are valid
.PHONY: docs-check
docs-check:
	@echo "Checking CLI reference freshness..."
	@go run ./cmd/docgen --no-timestamp | jq '.' > /tmp/sdf-cli-ref-check.json
	@jq 'del(.generated)' www/src/data/cli-reference.json > /tmp/sdf-cli-ref-current.json 2>/dev/null || cp www/src/data/cli-reference.json /tmp/sdf-cli-ref-current.json
	@diff -q /tmp/sdf-cli-ref-check.json /tmp/sdf-cli-ref-current.json > /dev/null 2>&1 \
		|| (echo "ERROR: CLI reference is stale. Run 'make docs' and commit." && exit 1)
	@echo "CLI reference is up to date."
	@echo "Validating narrative references..."
	@go test ./cmd/docgen/... -count=1

# ── Blog content checks ──────────────────────────────────────────

# Verify modified blog posts have dateModified set
.PHONY: blog-check
blog-check:
	@scripts/check-blog-updated-at.sh

# ── Release checklist ────────────────────────────────────────────

# Verify blog content exists for the current release version
.PHONY: release-checklist
release-checklist:
	@scripts/release-checklist.sh $(RELEASE_VERSION)
