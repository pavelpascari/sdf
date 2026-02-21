BINARY   := sdf
MODULE   := github.com/pavelpascari/sdf
VERSION  ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS  := -s -w -X main.version=$(VERSION)
BUILD    := go build -ldflags "$(LDFLAGS)" -trimpath

# Default: build for current platform
.PHONY: build
build:
	$(BUILD) -o bin/$(BINARY) .

# Run the binary
.PHONY: run
run: build
	./bin/$(BINARY) $(ARGS)

# Run tests
.PHONY: test
test:
	go test ./...

# Run vet and static checks
.PHONY: vet
vet:
	go vet ./...

# Format code
.PHONY: fmt
fmt:
	gofmt -s -w .

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
	@echo "  make dist       Cross-compile for all platforms → dist/"
	@echo "  make build-for OS=linux ARCH=arm64"
	@echo "                  Build for a specific platform → bin/sdf-<os>-<arch>"
	@echo "  make install    Install to GOPATH/bin"
	@echo "  make test       Run tests"
	@echo "  make vet        Run go vet"
	@echo "  make fmt        Format code"
	@echo "  make clean      Remove build artifacts"
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
