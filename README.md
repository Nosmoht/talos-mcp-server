# talos-mcp

[![CI](https://github.com/Nosmoht/talos-mcp-server/actions/workflows/ci.yml/badge.svg)](https://github.com/Nosmoht/talos-mcp-server/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/Nosmoht/talos-mcp-server?sort=semver)](https://github.com/Nosmoht/talos-mcp-server/releases)
[![Go Reference](https://pkg.go.dev/badge/github.com/Nosmoht/talos-mcp-server.svg)](https://pkg.go.dev/github.com/Nosmoht/talos-mcp-server)
[![codecov](https://codecov.io/gh/Nosmoht/talos-mcp-server/graph/badge.svg)](https://codecov.io/gh/Nosmoht/talos-mcp-server)
[![Go Report Card](https://goreportcard.com/badge/github.com/Nosmoht/talos-mcp-server)](https://goreportcard.com/report/github.com/Nosmoht/talos-mcp-server)
[![OpenSSF Scorecard](https://api.scorecard.dev/projects/github.com/Nosmoht/talos-mcp-server/badge)](https://scorecard.dev/viewer/?uri=github.com/Nosmoht/talos-mcp-server)
[![License](https://img.shields.io/github/license/Nosmoht/talos-mcp-server)](LICENSE)

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
go build -o talos-mcp ./cmd/talos-mcp
```

## Configuration

Reads `~/.talos/config` by default (the same file `talosctl` uses). Override via environment variables:

| Variable | Default | Description |
|---|---|---|
| `TALOSCONFIG` | `~/.talos/config` | Path to talosconfig file |
| `TALOS_CONTEXT` | active context | Context name to use |
| `TALOS_ENDPOINTS` | from config | Comma-separated endpoint overrides |
| `TALOS_MCP_READ_ONLY` | `false` | Set to `true` to disable all mutating tools at startup |
| `TALOS_MCP_HTTP_ADDR` | (unset) | If set (e.g. `:8080`), serve Streamable HTTP instead of stdio |
| `TALOS_MCP_AUTH_TOKEN` | (unset) | Required bearer token when HTTP mode is active |
| `TALOS_MCP_ALLOWED_NODES` | (unset) | Comma-separated IPs, hostnames, and CIDR ranges permitted as tool targets. Unset allows all. |
| `TALOS_MCP_ALLOWED_PATHS` | *(all)* | Comma-separated path prefixes allowed for `talos_read_file` and `talos_list_files` (e.g. `/etc,/proc`) |
| `TALOS_MCP_SKIP_VERSION_CHECK` | `false` | Set to `true` to bypass upgrade path validation (e.g. for factory images or custom tags) |
| `TALOS_MCP_ENABLE_INSECURE` | `false` | Unlock `insecure=true` on `talos_apply_config` / `talos_get` / `talos_version` / `talos_meta`. Bypasses mTLS — REQUIRES `TALOS_MCP_INSECURE_ALLOWED_NODES`. |
| `TALOS_MCP_INSECURE_ALLOWED_NODES` | (unset) | Comma-separated IPs / CIDRs permitted as maintenance-mode endpoints. Required when `TALOS_MCP_ENABLE_INSECURE=true`. Refused: `0.0.0.0/0`, `::/0`, IPv4 mask `<16`, IPv6 mask `<48`. |
| `TALOS_MCP_META_PRIVILEGED_KEYS` | *(none)* | Comma-separated META keys (decimal or `0x`-prefixed hex) that `talos_meta` is allowed to write/delete beyond `UserReserved1/2/3`. |
| `TALOS_MCP_SAFETY_PROFILE` | (unset) | `conservative` / `standard` / `expert` preset that seeds gating flags. `expert` enables `EnableInsecure`. |
| `TALOS_MCP_RATE_LIMIT` | `10` | HTTP mode: token-bucket refill rate (requests/second, float) |
| `TALOS_MCP_RATE_BURST` | `20` | HTTP mode: token-bucket burst capacity (int) |
| `TALOS_MCP_MAX_BODY_SIZE` | `4194304` | HTTP mode: max POST request body size in bytes (4 MiB default) |
| `TALOS_MCP_MAX_CONCURRENT` | `20` | HTTP mode: max concurrent POST handlers (fail-fast 503 on overload) |
| `TALOS_MCP_SUBSCRIPTION_RATE` | `1s` | Minimum interval between delivered `resources/updated` notifications per `(session, URI)` pair (Go duration, e.g. `500ms`) |
| `TALOS_MCP_SUBSCRIPTION_BURST` | `3` | Initial notification burst per `(session, URI)` before the rate kicks in |

## Compatibility

This server is tested against Talos Linux v1.9.x through v1.12.x.

| talos-mcp | Talos Linux | machinery SDK |
|-----------|-------------|---------------|
| v0.x (current) | v1.9.0 – v1.12.x | v1.12.6 |

The server logs a startup warning if the connected cluster's Talos version is outside the tested range. All 19 gRPC methods used have been stable since Talos v1.9.

### Upgrade path validation

The `talos_upgrade` tool validates that the target version follows Talos's supported upgrade path — at most one minor version at a time (e.g. v1.11.x → v1.12.x). Upgrades that skip minor versions are rejected with an error.

If your image uses a custom or factory tag (e.g. `factory.talos.dev/...` or `:latest`) the tag cannot be parsed and validation is skipped automatically. To bypass validation explicitly, set `TALOS_MCP_SKIP_VERSION_CHECK=true`.

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

<!-- inventory:tools:start -->
### Read-only

| Tool | Description |
|---|---|
| `talos_resource_definitions` | List all available resource types and their aliases. Call this first to discover what can be queried. |
| `talos_get` | Get or list any COSI resource by type (e.g. `MachineStatus`, `Member`, `NodeAddress`, `Service`). Supports maintenance-mode (`insecure=true` + `endpoint`). |
| `talos_version` | Get Talos version info from target nodes. Supports maintenance-mode (`insecure=true` + `endpoint`). |
| `talos_services` | List all Talos services and their current state (running, stopped, health). |
| `talos_containers` | List containers in a namespace (default: `k8s.io` for Kubernetes containers). |
| `talos_processes` | List running processes on target nodes. |
| `talos_health` | Check cluster health (etcd, Kubernetes API, node readiness). Supports `control_plane_nodes` / `worker_nodes` override. |
| `talos_logs` | Fetch recent service logs (last N lines, no follow). |
| `talos_dmesg` | Read kernel ring buffer messages. |
| `talos_events` | Fetch recent Talos runtime events (service changes, config changes). |
| `talos_etcd` | Query etcd cluster: `members` (default) or `status`. |
| `talos_etcd_snapshot` | Stream an etcd snapshot to a local file path. |
| `talos_list_files` | List files and directories on a node filesystem. |
| `talos_read_file` | Read file contents from a node filesystem. |
| `talos_validate` | Validate a machine config (YAML/JSON) offline — no cluster connection. |

### Mutating

These tools modify cluster state and have explicit safety guards.

| Tool | Description | Guards |
|---|---|---|
| `talos_service_action` | Start, stop, or restart a Talos service (note: restarting `etcd` is not supported by the Talos API). | `confirm=true` required |
| `talos_reboot` | Reboot target nodes. Supports `mode`: `default`, `powercycle`, `force`. | `confirm=true` required; `nodes` must be explicit |
| `talos_upgrade` | Upgrade Talos on target nodes. Supports `preserve` (default `true`), `stage`, `force`, `reboot_mode`. | `confirm=true` required; `nodes` and `image` required |
| `talos_rollback` | Roll back the last upgrade on target nodes. | `confirm=true` required; `nodes` must be explicit |
| `talos_patch_config` | Apply a machine config patch (JSON or YAML strategic merge). | `dry_run` defaults to `true`; `confirm=true` required when `dry_run=false` |
| `talos_reset` | Wipe and factory-reset target nodes (irreversible). | `confirm=true` required; `nodes` must be explicit |
| `talos_apply_config` | Apply a complete machine config to a single node. Supports maintenance-mode (`insecure=true` + `endpoint`) for fresh-node bootstrap. | `dry_run` defaults to `true`; `confirm=true` required when `dry_run=false` |
| `talos_meta` | Read, write, or delete META partition key/value pairs. Supports maintenance-mode (`insecure=true` + `endpoint`). | `write`/`delete` require `confirm=true`; non-`UserReserved*` keys require enumeration in `TALOS_MCP_META_PRIVILEGED_KEYS` |

All tools accept an optional `nodes` field (list of node IPs or hostnames). When omitted, the active context from talosconfig is used.

#### Maintenance-mode (`--insecure`) operations

`talos_apply_config`, `talos_get`, `talos_version`, and `talos_meta` accept an `insecure=true` flag that targets a node in maintenance mode (booted but not yet configured). The transport is TLS-encrypted but bypasses mTLS — there is no client certificate and (by default) no server-certificate verification. This is required for bootstrapping fresh nodes (`talosctl apply-config --insecure` equivalent).

- **Operator opt-in required.** Set `TALOS_MCP_ENABLE_INSECURE=true` (or use the `expert` safety profile). Without it, every `insecure=true` call is refused.
- **Endpoint allowlist required.** Set `TALOS_MCP_INSECURE_ALLOWED_NODES` to a comma-separated list of permitted maintenance-mode IPs / CIDRs. The startup is aborted if it is missing or contains `0.0.0.0/0`, `::/0`, an IPv4 mask `<16`, or an IPv6 mask `<48`. Use `/28` or narrower in production.
- **Endpoint must be a bare IP.** No hostnames, no `host:port`, no scheme, no IPv6 zone. Link-local (incl. `169.254.169.254` IMDS), loopback, multicast, and unspecified addresses are rejected.
- **MITM mitigation via TOFU pinning.** Pass `cert_fingerprint=<64-hex>` (server SHA-256 fingerprint, copied from the Talos console banner) to enable leaf-cert verification. Without it, the connection is MITMable by anyone on-path between the MCP server and the target node.
- **META write/delete safelist.** `talos_meta` write/delete is restricted to `meta.UserReserved1`/`2`/`3`. Privileged keys (`Upgrade`, `StateEncryptionConfig`, …) must be enumerated in `TALOS_MCP_META_PRIVILEGED_KEYS` (per-key, not a blanket flag).
<!-- inventory:tools:end -->

### Prompts

<!-- inventory:prompts:start -->
| Prompt | Description |
|---|---|
| `diagnose-node` | Guided diagnosis workflow for a single node. |
| `investigate-etcd` | Focused investigation of an etcd cluster anomaly. |
| `debug-service` | Service-specific diagnostic workflow (kubelet, containerd, etcd, …). |
| `pre-upgrade-checklist` | Pre-flight verification before a Talos upgrade. |
| `apply-config` | Guided flow for applying a machine config patch (registered only when `TALOS_MCP_READ_ONLY` is unset). |
<!-- inventory:prompts:end -->

### Resources and Subscriptions

<!-- inventory:resources:start -->

The server exposes Talos COSI resources as MCP resources:

- `talos://cluster/version` — static cluster version info.
- `talos://cluster/resource-definitions` — discover resource types.
- `talos://{node}/resource/{namespace}/{type}[/{id}]` — list or get COSI resources on a specific node.

MCP clients that implement `resources/subscribe` (Claude Desktop, Cursor) receive `notifications/resources/updated` whenever the underlying resource changes — no polling required. Subscriptions are backed by the Talos COSI `Watch` / `WatchKindAggregated` streams and honour the same `TALOS_MCP_ALLOWED_NODES` allowlist as reads.

Subscribable resource types (canonical names):

- `MachineStatuses.runtime.talos.dev` (`MachineStatus`)
- `Members.cluster.talos.dev` (`Member`)
- `NodeAddresses.net.talos.dev` (`NodeAddress`)
- `Services.v1alpha1.talos.dev` (`Service`)

Aliases resolve to the canonical type before the allowlist check, so a client subscribing to `talos://{node}/resource/runtime/ms/...` (alias for `MachineStatus`) succeeds. Other COSI types reject with `resource type %q is not subscribable`. Static `talos://cluster/*` URIs are not subscribable (no COSI backing).

Delivery is rate-limited per `(session, URI)` via `TALOS_MCP_SUBSCRIPTION_RATE` / `TALOS_MCP_SUBSCRIPTION_BURST`; over-rate events are dropped and the client re-reads the resource to catch up. The initial `Bootstrapped` event is intentionally not forwarded — the client is expected to call `resources/read` once after subscribe for initial state.
<!-- inventory:resources:end -->

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
| `talos_etcd`, `talos_service_action`, `talos_reboot`, `talos_upgrade`, `talos_rollback` | `os:operator` |
| `talos_patch_config` | `os:admin` |

### Safety Mechanisms

| Mechanism | How it works |
|---|---|
| Read-only mode | `TALOS_MCP_READ_ONLY=true` registers only read-only tools at startup; mutating tools are never exposed to the LLM |
| Path allowlist | `TALOS_MCP_ALLOWED_PATHS=/etc,/proc` restricts `talos_read_file` and `talos_list_files` to specified prefixes |
| Confirm gates | Always require `confirm=true`: `talos_service_action`, `talos_reboot`, `talos_upgrade`, `talos_rollback`, `talos_reset`. Require `confirm=true` when `dry_run=false`: `talos_patch_config`, `talos_apply_config`. All enforced server-side. |
| Preserve default | `talos_upgrade` defaults `preserve` to `true` (keep EPHEMERAL partition) — differs from `talosctl` default of `false` |
| Dry-run default | `talos_patch_config` defaults to `dry_run=true`; applying requires both `dry_run=false` and `confirm=true` |
| Audit logging | All mutating tool calls (`talos_service_action`, `talos_reboot`, `talos_upgrade`, `talos_rollback`, `talos_reset`, `talos_patch_config`, `talos_apply_config`) emit a structured log line to stderr: `AUDIT timestamp=<RFC3339> tool=<name> nodes=<list> args=<json>` (patch content is redacted) |

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

## Verifying Downloads

### Checksums (integrity)

Each release includes a `talos-mcp_<version>_checksums.txt` file with SHA-256 hashes of all archives. Verify the binary after downloading:

```bash
# Download archive and checksums
curl -LO https://github.com/Nosmoht/talos-mcp-server/releases/download/v<version>/talos-mcp_<version>_linux_amd64.tar.gz
curl -LO https://github.com/Nosmoht/talos-mcp-server/releases/download/v<version>/talos-mcp_<version>_checksums.txt

# Verify
sha256sum --check --ignore-missing talos-mcp_<version>_checksums.txt
```

This detects corruption or truncated downloads. It does not protect against a compromised release pipeline.

### GitHub Artifact Attestations (SLSA L2 provenance)

Each release includes a GitHub-native build provenance attestation that cryptographically links the binary to the specific commit and workflow run that produced it:

```bash
gh attestation verify talos-mcp_<version>_linux_amd64.tar.gz \
  --repo Nosmoht/talos-mcp-server
```

This requires the [GitHub CLI](https://cli.github.com/). A passing verification means the artifact was produced by the official release workflow in this repository, not a third-party build.

### npm Package Provenance

The npm package is published with provenance attestation:

```bash
npm audit signatures
```

A passing result means the package was published by the official GitHub Actions release workflow via OIDC trusted publishing.

## Development

```bash
# Build
go build -o talos-mcp ./cmd/talos-mcp

# Test
go test -race ./...

# Lint (requires golangci-lint v2)
golangci-lint run

# Format check
gofmt -l .
```

## License

[MIT](LICENSE)
