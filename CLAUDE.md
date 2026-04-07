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
| `TALOS_MCP_READ_ONLY` | `false` | Set to `"true"` to disable all mutating tools |
| `TALOS_MCP_HTTP_ADDR` | (unset) | If set (e.g. `:8080`), serve HTTP instead of stdio |
| `TALOS_MCP_AUTH_TOKEN` | (unset) | Required bearer token when HTTP mode is active |
| `TALOS_MCP_SKIP_VERSION_CHECK` | `false` | Set to `"true"` to bypass upgrade path validation |

## Compatibility

Tested against Talos Linux v1.9.x – v1.12.x (machinery SDK v1.12.6). The server logs a startup warning when the connected cluster version is outside this range.

- `talos_upgrade` validates that the target version is at most +1 minor from current (Talos upgrade path rule). Images with unparseable tags (factory images, `:latest`, custom registries) skip validation automatically.
- Set `TALOS_MCP_SKIP_VERSION_CHECK=true` to bypass validation in emergency scenarios.
- Compatibility range constants: `internal/version/version.go` (`MinSupported`, `MaxTested`) — update these when bumping the machinery SDK.

## Resources

### Static
- `talos://cluster/version` — Talos version information from the cluster's default endpoint
- `talos://cluster/resource-definitions` — all available COSI resource types with aliases and default namespaces; read this first to discover what types can be queried

### Templates
- `talos://{node}/resource/{namespace}/{type}` — list all COSI resources of a given type in a namespace on a specific node
- `talos://{node}/resource/{namespace}/{type}/{id}` — get a specific COSI resource by namespace, type, and ID on a specific node

## Tools (18)

### Read-only
- `talos_resource_definitions` — list all resource types and aliases
- `talos_get` — get/list any COSI resource by type
- `talos_version` — node version info
- `talos_services` — service list with state
- `talos_containers` — CRI container list (default namespace: `k8s.io`)
- `talos_processes` — running process list
- `talos_health` — cluster health check (etcd, k8s API, node readiness); supports `control_plane_nodes` / `worker_nodes` override
- `talos_logs` — recent service logs
- `talos_dmesg` — kernel ring buffer messages
- `talos_events` — recent Talos runtime events
- `talos_etcd` — etcd members or status
- `talos_list_files` — directory listing from node filesystem
- `talos_read_file` — read file contents from node filesystem

### Mutating (require explicit confirmation)
- `talos_service_action` — start/stop/restart a service (note: restarting `etcd` is not supported by the Talos API)
- `talos_reboot` — reboot nodes (requires `confirm=true` + explicit `nodes`); supports `mode`: `default`, `powercycle`, `force`
- `talos_upgrade` — upgrade Talos on nodes (requires `confirm=true` + `nodes` + `image`); supports `preserve` (default `true`), `stage`, `force`, `reboot_mode`
- `talos_rollback` — roll back the last upgrade on nodes (requires `confirm=true` + explicit `nodes`)
- `talos_patch_config` — apply machine config patch (defaults to `dry_run=true`)

## Prompts (5)

Prompts are guided workflows that instruct the AI agent which tools to call and in what order. They accept arguments and return structured investigation or action plans.

- `diagnose-node` — systematic node diagnosis: services → logs → events → MachineStatus → dmesg
  - `node` (required): IP address or hostname of the node to diagnose
- `pre-upgrade-checklist` — verify cluster readiness before upgrading to a target Talos version
  - `target_version` (required): target Talos version, e.g. `v1.9.0`
  - `nodes` (optional): comma-separated node IPs; omit to check all nodes in the active context
- `investigate-etcd` — deep-dive etcd health: status, members, logs, control-plane services, dmesg
  - `node` (optional): control plane node IP to focus on; omit to query all nodes
- `debug-service` — debug a crashing or failing service: state, logs, events, processes, dmesg
  - `service` (required): service name, e.g. `kubelet`, `containerd`, `etcd`
  - `node` (required): target node IP or hostname
  - `tail_lines` (optional): number of log lines to retrieve (default: `200`)
- `apply-config` — safe config patch workflow: health check → dry-run → user confirmation → apply (mutating; not registered in read-only mode)
  - `patch` (required): machine config patch as a JSON or YAML string
  - `node` (required): target node IP or hostname
  - `mode` (optional): apply mode — `auto`, `reboot`, `no_reboot`, `staged`, or `try` (default: `try`)

## Safety

- `talos_reboot`, `talos_upgrade`, and `talos_rollback` require `confirm=true` and explicit `nodes` — will error without both.
- `talos_upgrade` `preserve` defaults to `true` (keep EPHEMERAL partition) — differs from `talosctl` default of `false`. Set `preserve=false` explicitly to wipe.
- `talos_patch_config` defaults `dry_run=true` — you must explicitly pass `dry_run=false` to apply.

## Logging

Mutating tools (`talos_service_action`, `talos_reboot`, `talos_upgrade`, `talos_rollback`, `talos_patch_config`) emit `notifications/message` audit events to connected MCP clients in **stdio mode**:
- `info` level: tool invocation with tool name, target nodes, and arguments summary
- `error` level: operational errors (after guard checks pass, on Talos gRPC failures)

Delivery is best-effort — clients must call `logging/setLevel` to receive notifications. In **HTTP mode**, audit lines are written to server stderr only (MCP notifications not emitted).

## HTTP Transport

Set `TALOS_MCP_HTTP_ADDR=:8080` to start the server in Streamable HTTP mode (multi-session) instead of stdio.

**Authentication** — `TALOS_MCP_AUTH_TOKEN` must be set; the server refuses to start without it. All requests must carry `Authorization: Bearer <token>`. Generate a token with:

```
openssl rand -hex 32
```

**TLS** — not terminated by the binary; use a reverse proxy (nginx, Caddy, Tailscale funnel). The reverse proxy must preserve the `Origin` request header.

`DisableLocalhostProtection` is enabled — the built-in DNS rebinding guard is inactive. The server must be behind a reverse proxy or on a trusted network. The reverse proxy must preserve the `Origin` request header — the SDK's cross-origin protection uses it to reject untrusted origins; stripping it will cause all MCP client connections to fail.

**MCP log notifications** (`notifications/message`) are only emitted in stdio mode. In HTTP mode, audit lines are written to the server's stderr only.

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
go build -o talos-mcp ./cmd/talos-mcp
go test -race ./...
go vet ./...
gofmt -l .
```

## Release

Releases are fully automated via conventional commits:

1. Merge to `main` triggers `auto-tag.yml`
2. Conventional commit prefixes determine the version bump:
   - `fix(scope):` → patch (0.0.x)
   - `feat(scope):` → minor (0.x.0)
   - `BREAKING CHANGE:` or `feat!:` → major (x.0.0)
   - `docs:`, `ci:`, `chore:`, `refactor:`, `test:` → no tag, no release
3. The created tag triggers `release.yml` → GoReleaser builds linux/darwin binaries (amd64/arm64), publishes a GitHub Release, and publishes npm packages

The auto-tag workflow uses a GitHub App token (`RELEASE_APP_ID` / `RELEASE_APP_PRIVATE_KEY`) to push tags that trigger downstream workflows.

### npm OIDC Trusted Publishing (release.yml)

- Use `node-version: "24"` in the npm-publish job — Node.js 22 bundles npm 10.x which cannot self-upgrade to npm 11 (`MODULE_NOT_FOUND` during `npm install -g npm@11`); Node.js 24 ships with npm 11 natively
- Do **not** set `registry-url` in `actions/setup-node` — it writes `.npmrc` with `_authToken=${NODE_AUTH_TOKEN}` placeholder that blocks the OIDC token exchange (causes 404)
- Keep `--provenance` on all `npm publish` calls — provenance is not auto-generated despite what the docs say; the flag is harmless with OIDC and required with tokens
- Trusted Publishers must be configured per-package on npmjs.com before OIDC works (one-time setup per package at `https://www.npmjs.com/package/<name>/access`)

## MCP Development Setup

This repo ships a `.mcp.json.example` with two MCP servers. `.mcp.json` is gitignored so local paths stay out of source control.

```bash
cp .mcp.json.example .mcp.json
```

- **talos** — for local dev, replace `npx` with `./talos-mcp` (build first: `go build -o talos-mcp ./cmd/talos-mcp`)
- **github** — requires the `github-mcp-server` binary in `$PATH` and a token in the environment

```bash
# 1. Download github-mcp-server binary from:
#    https://github.com/github/github-mcp-server/releases
#    Extract and place in /usr/local/bin or ~/bin

# 2. Export your GitHub PAT (add to ~/.zshrc or ~/.bashrc):
export GITHUB_PERSONAL_ACCESS_TOKEN=ghp_...
```

The token value uses `${GITHUB_PERSONAL_ACCESS_TOKEN}` — Claude Code expands this from the environment at startup. The actual token never appears in `.mcp.json`.

## Change Governance

Every change to tracked files requires review before commit. No exceptions for size, type, or perceived risk.

### What constitutes a change

Any modification to Go source, tests, documentation, CI config, prompts, or generated output.

### Required review flow

1. **Plan** → invoke `senior-plan-reviewer` agent. Artifact: `.claude/reviews/<change-id>/plan-review.md`
2. **Implement** → invoke `senior-implementer` agent (or implement manually)
3. **Code + doc review** → invoke `staff-reviewer` agent. Artifact: `.claude/reviews/<change-id>/impl-review.md`
4. **Final approval** → invoke `principal-architect-reviewer` agent. Artifact: `.claude/reviews/<change-id>/final-approval.md`
5. **Commit** → only after all three artifacts show `status: approved` with zero findings

### Change-id convention

Use semantic slugs (e.g., `add-etcd-defrag-tool`, `fix-health-timeout`). Include in commit message:

```
feat(etcd): add defrag tool [review:add-etcd-defrag-tool]
```

### Role separation

The implementing agent/person must not serve as reviewer for the same change. The `principal-architect-reviewer` verifies this mechanically.

### Research

When uncertain about API behavior, conventions, or prior decisions: invoke the `researcher` agent. Repo-first, official-docs fallback.

### Enforcement

- `.claude/settings.json` hooks block `git commit` (Bash) and MCP GitHub push tools without valid review artifacts
- Native git `pre-commit` hook provides defense-in-depth for commits outside Claude Code

Install the git hook (one-time per clone):

```bash
cp .claude/hooks/pre-commit .git/hooks/pre-commit && chmod +x .git/hooks/pre-commit
```

### Artifact storage

Review artifacts (`.claude/reviews/`) and plan files (`.claude/plans/`) are **local-only** — gitignored and never committed. They exist on disk solely as process gates for the pre-commit hooks. The `[review:change-id]` tag in commit messages serves as the permanent audit trail.

### Known limitations (v1)

- Hook enforcement covers Claude Code sessions (Bash tool, MCP GitHub tools) and git CLI via the pre-commit hook
- Review artifacts are process guards, not cryptographically signed — trust is enforced by role separation and the principal-architect-reviewer gate
- Post-review file modifications are not detected (tracked for v2: content hashing in artifact frontmatter)
- Review artifacts are local-only (gitignored); fresh clones start without them
