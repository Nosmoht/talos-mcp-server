# talos-mcp

An MCP server that exposes Talos Linux cluster management to AI agents (Claude Code, OpenAI Codex, and any MCP-compatible client). Instead of pasting `talosctl` output into chat, the agent calls structured tools that return machine-readable JSON directly from the Talos gRPC API — zero token cost for intermediate output.

Connects to your cluster via the native Talos gRPC API using the same mTLS credentials as `talosctl` (`~/.talos/config`).

## Installation

**Via npm** (no Go required, Linux/macOS, amd64/arm64):

```bash
npx talos-mcp
```

**Download binary** (Linux/macOS, amd64/arm64):

Download the latest release from [GitHub Releases](https://github.com/Nosmoht/talos-mcp-server/releases), extract, and place the binary in your `$PATH`.

**Build from source** (requires Go 1.21+):

```bash
git clone https://github.com/Nosmoht/talos-mcp-server
cd talos-mcp
go build -o talos-mcp .
```

## Configuration

Reads `~/.talos/config` by default (the same file `talosctl` uses). Override via environment variables:

| Variable | Default | Description |
|---|---|---|
| `TALOSCONFIG` | `~/.talos/config` | Path to talosconfig file |
| `TALOS_CONTEXT` | active context | Context name to use |
| `TALOS_ENDPOINTS` | from config | Comma-separated endpoint overrides |

## Client Setup

### Claude Code

Add to your project's `.mcp.json`:

```json
{
  "mcpServers": {
    "talos": {
      "command": "npx",
      "args": ["-y", "talos-mcp"]
    }
  }
}
```

Or globally in `~/.claude.json` under `"mcpServers"`. If you prefer a local binary, replace `"command": "npx"` with the path to the binary.

### Claude Desktop

Add to `~/Library/Application Support/Claude/claude_desktop_config.json`:

```json
{
  "mcpServers": {
    "talos": {
      "command": "npx",
      "args": ["-y", "talos-mcp"]
    }
  }
}
```

### OpenAI Codex

Add to `.codex/config.toml` (project) or `~/.codex/config.toml` (global):

```toml
[mcp_servers.talos]
command = "npx"
args = ["-y", "talos-mcp"]

[mcp_servers.talos.env]
TALOSCONFIG = "/path/to/talosconfig"
```

### Generic MCP client

The server speaks the [MCP protocol](https://modelcontextprotocol.io) over stdio:

```bash
./talos-mcp
```

## Tools

### Read-only

| Tool | Description |
|---|---|
| `talos_resource_definitions` | List all available resource types and their aliases. Call this first to discover what can be queried. |
| `talos_get` | Get or list any COSI resource by type (e.g. `MachineStatus`, `Member`, `NodeAddress`, `Service`). |
| `talos_version` | Get Talos version info from target nodes. |
| `talos_services` | List all Talos services and their current state (running, stopped, health). |
| `talos_containers` | List containers in a namespace (default: `k8s.io` for Kubernetes containers). |
| `talos_processes` | List running processes on target nodes. |
| `talos_health` | Check cluster health (etcd, Kubernetes API, node readiness). |
| `talos_logs` | Fetch recent service logs (last N lines, no follow). |
| `talos_dmesg` | Read kernel ring buffer messages. |
| `talos_events` | Fetch recent Talos runtime events (service changes, config changes). |
| `talos_etcd` | Query etcd cluster: `members` (default) or `status`. |
| `talos_list_files` | List files and directories on a node filesystem. |
| `talos_read_file` | Read file contents from a node filesystem. |

### Mutating

These tools modify cluster state and have explicit safety guards.

| Tool | Description | Guards |
|---|---|---|
| `talos_service_action` | Start, stop, or restart a Talos service. | — |
| `talos_reboot` | Reboot target nodes. | `confirm=true` required; `nodes` must be explicit |
| `talos_upgrade` | Upgrade Talos on target nodes. | `confirm=true` required; `nodes` and `image` required |
| `talos_patch_config` | Apply a machine config patch (JSON or YAML strategic merge). | `dry_run` defaults to `true`; set `dry_run=false` to apply |

All tools accept an optional `nodes` field (list of node IPs or hostnames). When omitted, the active context from talosconfig is used.

## Safety Model

- **Destructive operations** (`talos_reboot`, `talos_upgrade`) require `confirm: true` and explicit `nodes` — they will error if either is missing.
- **Config patching** (`talos_patch_config`) defaults to `dry_run: true`. You must explicitly pass `dry_run: false` to apply changes.
- **Service actions** (`talos_service_action`) are guarded by the `DestructiveHint` annotation, which signals to the MCP client that user confirmation may be appropriate.

## Development

```bash
# Build
go build -o talos-mcp .

# Test
go test -race ./...

# Lint (requires golangci-lint v2)
golangci-lint run

# Format check
gofmt -l .
```

## License

[MIT](LICENSE)
