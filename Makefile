BINARY  := watui
MODULE  := github.com/watui/watui
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT  := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
LDFLAGS := -ldflags "-X main.version=$(VERSION) -X main.commit=$(COMMIT) -s -w"
DIST    := dist

GOOS   ?= $(shell go env GOOS)
GOARCH ?= $(shell go env GOARCH)

.PHONY: build run install clean dist help

build: ## Compile binary for the current platform
	go build $(LDFLAGS) -o $(BINARY) ./cmd/watui/

run: build ## Build and run with ./data as the data directory
	./$(BINARY) --data-dir ./data

install: ## Install to $(GOPATH)/bin (falls back to ~/go/bin)
	go install $(LDFLAGS) ./cmd/watui/

clean: ## Remove compiled binary and dist/ directory
	rm -f $(BINARY)
	rm -rf $(DIST)

dist: clean ## Build a release archive for the current platform
	@echo "Building $(BINARY) $(VERSION) for $(GOOS)/$(GOARCH)..."
	@mkdir -p $(DIST)
	GOOS=$(GOOS) GOARCH=$(GOARCH) CGO_ENABLED=1 \
		go build $(LDFLAGS) -o $(DIST)/$(BINARY) ./cmd/watui/
	@cd $(DIST) && \
		tar czf $(BINARY)-$(VERSION)-$(GOOS)-$(GOARCH).tar.gz $(BINARY) \
		&& rm $(BINARY)
	@echo "Package: $(DIST)/$(BINARY)-$(VERSION)-$(GOOS)-$(GOARCH).tar.gz"

help: ## Show available targets
	@grep -E '^[a-zA-Z_-]+:.*##' Makefile | awk 'BEGIN {FS = ":.*##"}; {printf "  %-12s %s\n", $$1, $$2}'
