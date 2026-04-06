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
| `TALOS_MCP_READ_ONLY` | `false` | Set to `true` to disable all mutating tools at startup |
| `TALOS_MCP_ALLOWED_PATHS` | *(all)* | Comma-separated path prefixes allowed for `talos_read_file` and `talos_list_files` (e.g. `/etc,/proc`) |

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

## Security Model

### Trust Boundaries

```
MCP Client (Claude Code / Codex)
        │  stdio / JSON-RPC
        ▼
   talos-mcp  ◄── reads TALOSCONFIG (~/.talos/config)
        │  gRPC + mTLS
        ▼
  Talos API (each node)
        │
        ▼
    Node OS
```

**Data flow warning:** Tool responses flow directly into the LLM's context window and are sent to the LLM provider. Anything a tool returns — node IPs, hostnames, service configurations, kernel logs, file contents — becomes part of the prompt sent over the network. Do not use this server with clusters containing data you would not be comfortable sending to your LLM provider.

**Talos RBAC is server-side enforced.** The credentials in your talosconfig determine what operations are permitted on each node. talos-mcp cannot bypass Talos RBAC — a request that the API rejects will fail with an error, not silently succeed.

### Tool Classification and Minimum Required RBAC Role

| Tool | RBAC minimum |
|---|---|
| `talos_resource_definitions`, `talos_get`, `talos_version`, `talos_services`, `talos_containers`, `talos_processes`, `talos_health`, `talos_logs`, `talos_dmesg`, `talos_events`, `talos_list_files`, `talos_read_file` | `os:reader` |
| `talos_etcd`, `talos_service_action`, `talos_reboot`, `talos_upgrade` | `os:operator` |
| `talos_patch_config` | `os:admin` |

### Safety Mechanisms

| Mechanism | How it works |
|---|---|
| Read-only mode | `TALOS_MCP_READ_ONLY=true` registers only read-only tools at startup; mutating tools are never exposed to the LLM |
| Path allowlist | `TALOS_MCP_ALLOWED_PATHS=/etc,/proc` restricts `talos_read_file` and `talos_list_files` to specified prefixes |
| Confirm gates | `talos_reboot` and `talos_upgrade` require `confirm=true` and explicit `nodes`; both fields are enforced server-side |
| Dry-run default | `talos_patch_config` defaults to `dry_run=true`; changes are only applied when `dry_run=false` is explicitly set |
| Audit logging | All mutating tool calls emit a structured log line to stderr: `AUDIT timestamp=<RFC3339> tool=<name> nodes=<list> args=<json>` (patch content is redacted) |

### What Is Not in the Threat Model

- **The LLM itself** — prompt injection, hallucinated tool arguments, and LLM provider data retention are outside the scope of this server
- **The MCP client** — security of Claude Code, Codex, or other MCP clients is the responsibility of those projects
- **Network path between talos-mcp and Talos nodes** — protected by mutual TLS using the credentials in your talosconfig

### Least-Privilege Credential Setup

Create a dedicated talosconfig with minimal permissions for use with this server:

**Read-only access (recommended for most use cases):**

```bash
# Generate a reader-only talosconfig
talosctl config new --roles=os:reader talosconfig-readonly
```

Then set `TALOSCONFIG=/path/to/talosconfig-readonly` and `TALOS_MCP_READ_ONLY=true` for maximum restriction. With this setup, the server exposes only read-only tools and the credentials cannot perform any mutating operations even if a tool were somehow bypassed.

**Operator access (for service management, reboot, upgrade):**

```bash
talosctl config new --roles=os:operator talosconfig-operator
```

This covers all tools except `talos_patch_config` (which requires `os:admin`).

**Full access (required for config patching):**

Use your default talosconfig or generate one with `os:admin`. Reserve this for setups where config patch capability is explicitly needed.

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
