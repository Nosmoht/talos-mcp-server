BINARY     := talos-mcp
VERSION    := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT     := $(shell git rev-parse --short HEAD 2>/dev/null || echo "none")
DATE       := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS    := -s -w \
	-X main.version=$(VERSION) \
	-X main.commit=$(COMMIT) \
	-X main.date=$(DATE)

MISE           := mise exec --
GO_MOD_VERSION := $(shell awk '$$1 == "toolchain" {t = $$2; sub(/^go/, "", t)} $$1 == "go" {g = $$2} END {print (t != "" ? t : g)}' go.mod)
# mise resolves Go from go.mod before its [env] applies; a host GOWORK file path makes it fall back to the host go.
export GOWORK := off

.DEFAULT_GOAL := help

.PHONY: help build test test-integration bench toolchain lint arch-lint fmt fmt-fix vet check clean clean-worktrees coverage mod-tidy

help: ## Show available targets
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  %-12s %s\n", $$1, $$2}'

build: toolchain ## Build the binary (CGO_ENABLED=0, version info injected)
	$(MISE) env CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o $(BINARY) ./cmd/talos-mcp

test: toolchain ## Run tests with race detector and coverage report
	$(MISE) go test -v -race -coverprofile=coverage.out -coverpkg=./... ./...
	$(MISE) go tool cover -func=coverage.out

test-integration: toolchain build ## Run integration tests against a live Talos cluster (requires talosconfig)
	$(MISE) go test -v -tags integration -timeout 120s -count=1 ./cmd/talos-mcp/

bench: toolchain ## Run Go benchmarks (use BENCH=pattern to filter, e.g. BENCH=Marshal)
	$(MISE) go test -bench=$${BENCH:-.} -benchmem -run='^$$' ./internal/tools/

toolchain: ## Fail unless mise runs the Go version go.mod selects
	@want="go$(GO_MOD_VERSION)"; got="$$($(MISE) go env GOVERSION)"; \
	if [ "$$got" != "$$want" ]; then echo "Go toolchain mismatch: expected $$want, mise runs $$got" >&2; exit 1; fi

lint: toolchain ## Run golangci-lint
	$(MISE) golangci-lint run

arch-lint: toolchain ## Check package and vendor dependency rules (go-arch-lint)
	$(MISE) go-arch-lint check

fmt: toolchain ## Check formatting (fails if any files need formatting)
	@unformatted=$$($(MISE) gofmt -l .); \
	if [ -n "$$unformatted" ]; then \
		echo "Unformatted files (run 'make fmt-fix'):"; \
		echo "$$unformatted"; \
		exit 1; \
	fi

fmt-fix: toolchain ## Auto-fix formatting with gofmt
	$(MISE) gofmt -w .

vet: toolchain ## Run go vet
	$(MISE) go vet ./...

check: fmt vet lint arch-lint test ## Run full validation (CI parity: fmt + vet + lint + arch-lint + test)

coverage: toolchain test ## Generate HTML coverage report and open in browser
	$(MISE) go tool cover -html=coverage.out

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

mod-tidy: toolchain ## Tidy go module dependencies
	$(MISE) go mod tidy
