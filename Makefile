BINARY     := talos-mcp
VERSION    := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT     := $(shell git rev-parse --short HEAD 2>/dev/null || echo "none")
DATE       := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS    := -s -w \
	-X main.version=$(VERSION) \
	-X main.commit=$(COMMIT) \
	-X main.date=$(DATE)

.DEFAULT_GOAL := help

.PHONY: help build test test-integration lint fmt fmt-fix vet check clean coverage mod-tidy

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

mod-tidy: ## Tidy go module dependencies
	go mod tidy
