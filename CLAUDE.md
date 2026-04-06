# talos-mcp

MCP server exposing Talos Linux cluster management to AI agents via the native gRPC API.

## Build

```bash
make build
```

## Configuration

Environment variables (set in `.mcp.json` env block or shell):

| Variable | Default | Description |
|---|---|---|
| `TALOSCONFIG` | `~/.talos/config` | Path to talosconfig file |
| `TALOS_CONTEXT` | active context | Context name override |
| `TALOS_ENDPOINTS` | from config | Comma-separated endpoint overrides |

## Tools (15)

### Read-only
- `talos_resource_definitions` — list all resource types and aliases
- `talos_get` — get/list any COSI resource by type
- `talos_version` — node version info
- `talos_services` — service list with state
- `talos_containers` — CRI container list (default namespace: `k8s.io`)
- `talos_processes` — running process list
- `talos_health` — cluster health check (etcd, k8s API, node readiness)
- `talos_logs` — recent service logs
- `talos_dmesg` — kernel ring buffer messages
- `talos_events` — recent Talos runtime events
- `talos_etcd` — etcd members or status
- `talos_list_files` — directory listing from node filesystem
- `talos_read_file` — read file contents from node filesystem

### Mutating (require explicit confirmation)
- `talos_service_action` — start/stop/restart a service
- `talos_reboot` — reboot nodes (requires `confirm=true` + explicit `nodes`)
- `talos_upgrade` — upgrade Talos on nodes (requires `confirm=true` + `nodes` + `image`)
- `talos_patch_config` — apply machine config patch (defaults to `dry_run=true`)

## Safety

- `talos_reboot` and `talos_upgrade` require `confirm=true` and explicit `nodes` — will error without both.
- `talos_patch_config` defaults `dry_run=true` — you must explicitly pass `dry_run=false` to apply.

## Development

```bash
make build      # build binary with version info
make test       # run tests with race detector + coverage
make lint       # run golangci-lint
make fmt        # check formatting
make check      # full CI parity (fmt + vet + lint + test)
make help       # list all targets
```

Raw commands (no make):

```bash
go build -o talos-mcp .
go test -race ./...
go vet ./...
gofmt -l .
```

## Release

Tag and push to trigger GitHub Actions release workflow:

```bash
git tag v0.1.0
git push --tags
```

GoReleaser builds linux/darwin binaries (amd64/arm64) and publishes a GitHub Release.

## MCP Development Setup

This repo ships a `.mcp.json.example` with two MCP servers. `.mcp.json` is gitignored so local paths stay out of source control.

```bash
cp .mcp.json.example .mcp.json
```

- **talos** — for local dev, replace `npx` with `./talos-mcp` (build first: `go build -o talos-mcp .`)
- **github** — requires the `github-mcp-server` binary in `$PATH` and a token in the environment

```bash
# 1. Download github-mcp-server binary from:
#    https://github.com/github/github-mcp-server/releases
#    Extract and place in /usr/local/bin or ~/bin

# 2. Export your GitHub PAT (add to ~/.zshrc or ~/.bashrc):
export GITHUB_PERSONAL_ACCESS_TOKEN=ghp_...
```

The token value uses `${GITHUB_PERSONAL_ACCESS_TOKEN}` — Claude Code expands this from the environment at startup. The actual token never appears in `.mcp.json`.
