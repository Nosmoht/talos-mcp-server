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
| `TALOS_MCP_ALLOWED_NODES` | (unset) | Comma-separated IPs, hostnames, and CIDR ranges permitted as tool targets. Unset or empty allows all nodes. |
| `TALOS_MCP_SKIP_VERSION_CHECK` | `false` | Set to `"true"` to bypass upgrade path validation |
| `TALOS_MCP_RATE_LIMIT` | `10` | HTTP mode: token-bucket refill rate (requests/second, float) |
| `TALOS_MCP_RATE_BURST` | `20` | HTTP mode: token-bucket burst capacity (int) |
| `TALOS_MCP_MAX_BODY_SIZE` | `4194304` | HTTP mode: max POST request body size in bytes (4 MiB default) |
| `TALOS_MCP_MAX_CONCURRENT` | `20` | HTTP mode: max concurrent POST handlers (fail-fast 503 on overload) |

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

## Tools (20)

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
- `talos_validate` — validate a Talos machine config offline (no cluster needed); supports `mode` (`metal`/`cloud`/`container`) and `strict`

### Mutating (require explicit confirmation)
- `talos_service_action` — start/stop/restart a service (note: restarting `etcd` is not supported by the Talos API)
- `talos_reboot` — reboot nodes (requires `confirm=true` + explicit `nodes`); supports `mode`: `default`, `powercycle`, `force`; supports `wait=true` + `timeout` to block until reboot completes (default: fire-and-forget); **all listed nodes are rebooted simultaneously — reboot one node at a time to avoid a full cluster outage**
- `talos_upgrade` — upgrade Talos on nodes (requires `confirm=true` + `nodes` + `image`); supports `preserve` (default `true`), `stage`, `force`, `reboot_mode`
- `talos_rollback` — roll back the last upgrade on nodes (requires `confirm=true` + explicit `nodes`)
- `talos_reset` — wipe and factory-reset nodes (requires `confirm=true` + explicit `nodes`); supports `graceful` (default `true`), `reboot` (default `false`), `system_labels_to_wipe` (specific partitions; empty = full system disk wipe)
- `talos_patch_config` — apply machine config patch (defaults to `dry_run=true`; requires `confirm=true` when `dry_run=false`)
- `talos_apply_config` — apply a complete machine config document to a single node (defaults to `dry_run=true`; requires `confirm=true` when `dry_run=false`)

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
- `talos_reboot` reboots **all listed nodes simultaneously** — specify one node at a time to maintain cluster availability. Set `wait=true` to block until the reboot completes (verified via boot ID change); use `timeout` to control the deadline (default `5m`).
- `talos_upgrade` `preserve` defaults to `true` (keep EPHEMERAL partition) — differs from `talosctl` default of `false`. Set `preserve=false` explicitly to wipe.
- `talos_reset` requires `confirm=true` and explicit `nodes`. All listed nodes are reset simultaneously — reset one node at a time to maintain cluster availability. `graceful` defaults to `true` (drain workloads and leave etcd before wiping). Set `graceful=false` only on unresponsive nodes. `system_labels_to_wipe` empty = full system disk wipe (factory reset); provide specific labels (e.g. `["EPHEMERAL"]`) for a partial wipe.
- `talos_patch_config` defaults `dry_run=true` — you must explicitly pass `dry_run=false` to apply. When `dry_run=false`, `confirm=true` is also required.
- `talos_apply_config` defaults `dry_run=true` and requires exactly one target node. When `dry_run=false`, `confirm=true` is also required. Replaces the entire machine config — use `talos_patch_config` for targeted changes.

## Logging

Mutating tools (`talos_service_action`, `talos_reboot`, `talos_upgrade`, `talos_rollback`, `talos_reset`, `talos_patch_config`, `talos_apply_config`) emit `notifications/message` audit events to connected MCP clients in **stdio mode**:
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

## Development Workflow

Every code change (features, fixes, refactors) is developed in an isolated git worktree to keep the main working directory clean and allow parallel work.

### Worktree setup

```bash
# Create a new worktree for a feature or fix (slug = change-id)
git fetch origin main
git worktree add -b feat/<slug> .claude/worktrees/<slug> origin/main
# or: git worktree add -b fix/<slug> .claude/worktrees/<slug> origin/main
```

Work inside `.claude/worktrees/<slug>/` for the full lifecycle of the change.

### When to use a worktree

- **Always**: any change that will result in a commit (code, docs, config, CI)
- **Not needed**: exploration, research, reading files, running tests without changes

### Branch naming

Match the branch name to the change-id: `feat/<change-id>` or `fix/<change-id>`.

### Rebase before push

Always rebase onto the latest main before pushing:

```bash
git fetch origin main && git rebase origin/main
```

### Cleanup after merge

```bash
git worktree remove .claude/worktrees/<slug>
git branch -d feat/<slug>
```

### Workflow summary

1. Fetch latest: `git fetch origin main`
2. Create worktree: `git worktree add -b feat/<slug> .claude/worktrees/<slug> origin/main`
3. Work in `.claude/worktrees/<slug>/`
4. Run reviews (see Change Governance below)
5. Rebase: `git fetch origin main && git rebase origin/main`
6. Push branch and open PR
7. After merge: remove worktree and delete local branch

## Project Management

Issue lifecycle, coding conventions, and multi-agent coordination are documented in [AGENTS.md](./AGENTS.md).

Find ready issues:

```
repo:Nosmoht/talos-mcp-server is:issue is:open label:"status: ready" sort:created-asc
```

- Issues are triaged with `status: ready` and claimed via the two-phase protocol in AGENTS.md
- Label groups: `status:`, `priority:`, `area:`, `size:` — see AGENTS.md for the full taxonomy
- Run `scripts/setup-project.sh` once per repo to create all labels and the Projects v2 board

## Release

Releases are fully automated via conventional commits:

1. Merge to `main` triggers `auto-tag.yml` — **only when server code changes** (path-filtered to `*.go`, `cmd/**`, `internal/**`, `go.mod`, `go.sum`, `Makefile`, `.goreleaser.yaml`)
2. Conventional commit prefixes determine the version bump:
   - `fix(scope):` → patch (0.0.x)
   - `feat(scope):` → minor (0.x.0)
   - `BREAKING CHANGE:` or `feat!:` → major (x.0.0)
   - `docs:`, `ci:`, `chore:`, `refactor:`, `test:` → no tag, no release
3. The created tag triggers `release.yml` → GoReleaser builds linux/darwin binaries (amd64/arm64), publishes a GitHub Release, and publishes npm packages

Changes to `.claude/`, docs, or CI config alone do **not** trigger a release regardless of commit prefix. Use `chore:` or `ci:` prefixes for such changes as a convention (advisory — path filter is the enforcement gate).

The auto-tag workflow uses a GitHub App token (`RELEASE_APP_ID` / `RELEASE_APP_PRIVATE_KEY`) to push tags that trigger downstream workflows.

### npm OIDC Trusted Publishing (release.yml)

- Use `node-version: "24"` in the npm-publish job — Node.js 22 bundles npm 10.x which cannot self-upgrade to npm 11 (`MODULE_NOT_FOUND` during `npm install -g npm@11`); Node.js 24 ships with npm 11 natively
- Do **not** set `registry-url` in `actions/setup-node` — it writes `.npmrc` with `_authToken=${NODE_AUTH_TOKEN}` placeholder that blocks the OIDC token exchange (causes 404)
- Keep `--provenance` on all `npm publish` calls — provenance is not auto-generated despite what the docs say; the flag is harmless with OIDC and required with tokens
- Trusted Publishers must be configured per-package on npmjs.com before OIDC works (one-time setup per package at `https://www.npmjs.com/package/<name>/access`)

## CI

GitHub Actions workflows:

| Workflow | Trigger | Purpose |
|---|---|---|
| `ci.yml` | push to main, all PRs | Go lint, test, build, vulnerability check |
| `auto-tag.yml` | push to main (server paths only) | Create semver tag on server code change |
| `release.yml` | tag push (`v*`) | GoReleaser builds + npm publish |
| `codeql.yml` | push to main, all PRs, weekly | Go static analysis |
| `scorecard.yml` | push to main, weekly | OpenSSF security posture |

**Merge-guard pattern:** `ci.yml` uses a `changes` job (dorny/paths-filter) to skip Go jobs on PRs that don't touch server code. A `merge-guard` job always runs and is the sole required status check. On `push` to main, CI always runs in full (no path filter on push — safer for the default branch).

**Path list maintenance:** Go-relevant paths are duplicated between `ci.yml` (`changes` job) and `auto-tag.yml`. Keep them in sync when adding new Go packages or build files.

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

## GitHub Issues

Issues are created via `gh` CLI. Use `--body-file` for issue bodies containing backticks — heredocs with `--body` corrupt markdown code blocks. The `.github/ISSUE_TEMPLATE/` directory provides form-based templates for issues submitted via the web UI; the structures below apply to CLI-created issues.

```bash
gh issue create \
  --title "fix(tools): describe the fix" \
  --label "type: bug,priority: P2,origin: audit,needs: triage" \
  --body-file /tmp/issue-body.md
```

### Issue types and body structure

**Bugs and chores** — use `##` headings:

```markdown
## Description
What is wrong and where.

## Evidence
Code snippets, file paths with line numbers, reproduction steps.

## Impact
What breaks, who is affected, severity context.

## Recommended Fix
Concrete suggestion with code examples where helpful.

## Audit Source
How this was found (e.g., "AI-assisted code review, verified at commit `abc1234` on 2026-04-07").
```

**Feature requests** — use `###` headings:

```markdown
### Severity
P0–P3 with one-line rationale.

### Category
Area(s) affected (e.g., safety, transport, security).

### Description
What is missing and why it matters.

### Evidence
Current behavior, code locations, related issues.

### Impact
What improves, what is currently blocked.

### Recommended Fix
Proposed implementation with effort estimate.

### Audit Source
How this was found.
```

### Labels

See [AGENTS.md § Label Rules](./AGENTS.md) for the full taxonomy, cardinality rules, and state machine. New issues require at minimum: one `type:`, one `priority:`, and `needs: triage` (so the triage pass can assign `size:`, `area:`, and `status: ready`).

### Referencing commits

Include the commit hash where the issue was discovered in the **Audit Source** section: ``verified at commit `c8e3db9` on 2026-04-07``.

### Cross-references

Link related issues with `Related: #38, #41` or `Depends on: #37` in the Audit Source section or inline.

## Change Governance

Every change to tracked files requires review before commit.

### What constitutes a change

Any modification to Go source, tests, documentation, CI config, prompts, or generated output.

### Review depth model

Review depth scales with change complexity (DAAO principle — difficulty-aware routing):

| Change type | Required review |
|---|---|
| `docs` / `chore` / `ci` | `staff-reviewer` → `review.md` with `status: approved` |
| Code — simple | `staff-reviewer` → `review.md` with `status: approved` |
| Code — complex | `staff-reviewer` → `review.md` with `status: escalate` → escalation reviewer(s) |

### Required review flow

1. **Implement** → invoke `senior-implementer` agent (or implement manually)
2. **Review** → invoke `staff-reviewer` agent. Artifact: `.claude/reviews/<change-id>/review.md`
   - If `status: approved`: proceed to commit
   - If `status: escalate`: invoke each listed escalation reviewer
3. **Escalation review** (if needed) → invoke the appropriate domain reviewer. Artifact: `.claude/reviews/<change-id>/review-<type>.md`
4. **Commit** → once all required artifacts show `status: approved`

**Plan review** (optional, recommended for complex changes): invoke `senior-plan-reviewer` before implementing. Artifact: `.claude/reviews/<change-id>/plan-review.md`

### Escalation criteria

The `staff-reviewer` escalates when it identifies concrete risk. Default: do **not** escalate.

| Type | Reviewer | Triggers |
|---|---|---|
| `operational-safety` | `operational-safety-reviewer` | Any new/modified mutating tool, guard logic change, audit logging change, read-only enforcement |
| `provenance` | `provenance-reviewer` | `go.mod` or `go.sum` modified, new external import |
| `compatibility` | `compatibility-reviewer` | Tool/prompt/resource signature change, SDK version bump, tool removal |
| `architecture` | `principal-architect-reviewer` | New package, >3 packages modified, structural refactor, API surface addition |
| `security` | `security-reviewer` | Auth/mTLS/token handling, input validation, hook/enforcement logic |
| `performance` | `performance-reviewer` | gRPC streaming, goroutine lifecycle, hot-path caching |

### Change-id convention

Use semantic slugs (e.g., `add-etcd-defrag-tool`, `fix-health-timeout`). Include in commit message:

```
feat(etcd): add defrag tool [review:add-etcd-defrag-tool]
```

### Role separation

The implementing agent/person must not serve as reviewer for the same change.

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

### Known limitations

- Hook enforcement covers Claude Code sessions (Bash tool, MCP GitHub tools) and git CLI via the pre-commit hook
- Review artifacts are process guards, not cryptographically signed — trust is enforced by role separation
- Post-review file modifications are not detected (tracked for future: content hashing in artifact frontmatter)
- Review artifacts are local-only (gitignored); fresh clones start without them
- The hook uses the lexicographically last review directory — ensure change-id slugs sort to the correct change
