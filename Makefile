BINARY     := talos-mcp
VERSION    := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT     := $(shell git rev-parse --short HEAD 2>/dev/null || echo "none")
DATE       := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS    := -s -w \
	-X main.version=$(VERSION) \
	-X main.commit=$(COMMIT) \
	-X main.date=$(DATE)

.DEFAULT_GOAL := help

.PHONY: help build test test-integration bench lint fmt fmt-fix vet check clean clean-worktrees coverage mod-tidy

help: ## Show available targets
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  %-12s %s\n", $$1, $$2}'

build: ## Build the binary (CGO_ENABLED=0, version info injected)
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o $(BINARY) ./cmd/talos-mcp

test: ## Run tests with race detector and coverage report
	go test -v -race -coverprofile=coverage.out -coverpkg=./... ./...
	go tool cover -func=coverage.out

test-integration: build ## Run integration tests against a live Talos cluster (requires talosconfig)
	go test -v -tags integration -timeout 120s -count=1 ./cmd/talos-mcp/

bench: ## Run Go benchmarks (use BENCH=pattern to filter, e.g. BENCH=Marshal)
	go test -bench=$${BENCH:-.} -benchmem -run='^$$' ./internal/tools/

lint: ## Run golangci-lint (install: curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $(go env GOPATH)/bin v2.11.4)
	@GOPATH=$$(go env GOPATH); \
	LINT=$$(command -v golangci-lint 2>/dev/null || echo "$$GOPATH/bin/golangci-lint"); \
	if [ ! -x "$$LINT" ]; then \
		echo "golangci-lint not found. Install v2.11.4:"; \
		echo "  curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b \$$(go env GOPATH)/bin v2.11.4"; \
		exit 1; \
	fi; \
	$$LINT run

fmt: ## Check formatting (fails if any files need formatting)
	@unformatted=$$(gofmt -l .); \
	if [ -n "$$unformatted" ]; then \
		echo "Unformatted files (run 'make fmt-fix'):"; \
		echo "$$unformatted"; \
		exit 1; \
	fi

fmt-fix: ## Auto-fix formatting with gofmt
	gofmt -w .

vet: ## Run go vet
	go vet ./...

check: fmt vet lint test ## Run full validation (CI parity: fmt + vet + lint + test)

coverage: test ## Generate HTML coverage report and open in browser
	go tool cover -html=coverage.out

clean: ## Remove build artifacts
	rm -f $(BINARY) coverage.out

# clean-worktrees runs `git worktree prune` to drop admin entries whose working
# trees are missing (e.g. cross-machine rsync remnants), then removes physical
# directories under .claude/worktrees/ that no longer correspond to any live
# worktree. The orphan-removal step refuses to run if `git worktree list`
# returns no entries — a defensive guard against deleting every directory when
# the live set cannot be enumerated.
clean-worktrees: ## Prune stale worktree admin entries and remove orphan .claude/worktrees/ dirs
	@echo "==> Pruning stale worktree admin entries..."
	@git worktree prune --verbose
	@if [ ! -d .claude/worktrees ]; then exit 0; fi
	@echo ""
	@echo "==> Scanning for orphan .claude/worktrees/ directories..."
	@live=$$(git worktree list --porcelain | awk '/^worktree/ {n=split($$2,a,"/"); print a[n]}' | tr '\n' ' '); \
	if [ -z "$$live" ]; then \
		echo "ERROR: git worktree list returned no entries — refusing to proceed." >&2; \
		exit 1; \
	fi; \
	orphans=""; \
	for d in .claude/worktrees/*/; do \
		[ -d "$$d" ] || continue; \
		name=$$(basename "$$d"); \
		case " $$live " in *" $$name "*) ;; \
			*) orphans="$$orphans $$name" ;; \
		esac; \
	done; \
	if [ -z "$$orphans" ]; then \
		echo "  (no orphans)"; \
		exit 0; \
	fi; \
	echo "  found:$$orphans"; \
	for name in $$orphans; do \
		echo "  removing .claude/worktrees/$$name"; \
		rm -rf ".claude/worktrees/$$name"; \
	done

mod-tidy: ## Tidy go module dependencies
	go mod tidy
