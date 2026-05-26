# AGENTS.md

Machine-readable conventions for AI agents working on the talos-mcp repository.
Read this file before starting any work.

---

## Overview

Multiple AI agents (Claude Code, OpenAI Codex, GitHub Copilot, Cursor, and others) may
work concurrently on this repository via git worktrees. This document defines:

- **Coding conventions** — how to write consistent Go code
- **Workflow conventions** — how to discover work, claim issues, and coordinate

---

## Scope And Source Of Truth

`AGENTS.md` is the canonical cross-agent instruction file for this repository.
For this repo it carries the durable instructions every coding agent needs:

- Project purpose and repository-specific boundaries
- Build, test, lint, and verification commands
- Coding, testing, logging, and security conventions
- Multi-agent worktree, issue, commit, review, and PR workflow
- The split between agent instructions and human/operator documentation

`CLAUDE.md` is the Claude-Code-specific operative layer. It carries Claude-only
guidance (memory hygiene, review-artifact paths under `.claude/`) plus
quick-reference excerpts that link back here via Markdown links. Do not duplicate
cross-agent policy into `CLAUDE.md` — update `AGENTS.md` so the rule applies to
Claude Code, Codex, Copilot, Cursor, and other agents alike.

`README.md` is the human/operator entry point. Product usage, installation,
configuration, tool catalogs, compatibility tables, release notes, and end-user
examples live in `README.md` or purpose-built docs, not in `AGENTS.md`.

If nested `AGENTS.md` files are added later, the closest file to the edited path
wins for that subtree. Explicit user instructions in chat override this file.

### Do not put in AGENTS.md

- Secrets, tokens, credentials, or machine-local private state
- One-off investigation notes, transient TODOs, command output, or scratch plans
- Detailed product/user documentation that belongs in `README.md`
- Claude-only subagent prompts, memory notes, or UI-specific instructions
- Long release postmortems or issue-specific analysis

---

## Commands

Use `make` targets unless a narrower raw command is useful while iterating:

```bash
make build           # build binary with version info
make test            # run tests with race detector + coverage
make test-integration # integration tests against a live Talos cluster
make bench           # run Go benchmarks (use BENCH=pattern to filter)
make lint            # run golangci-lint
make fmt             # check formatting
make fmt-fix         # auto-fix formatting with gofmt
make vet             # run go vet
make check           # full CI parity (fmt + vet + lint + test)
make coverage        # HTML coverage report
make clean           # remove build artifacts
make clean-worktrees # prune stale worktree entries and orphan dirs
make mod-tidy        # tidy go module dependencies
make help            # list available targets
```

Raw equivalents (use only when a `make` target does not fit the iteration):

```bash
go build -o talos-mcp ./cmd/talos-mcp
go test -race ./...
go test ./internal/tools -run TestName     # focused single-test iteration
go vet ./...
gofmt -l .
```

Always run `make check` before opening a PR. For narrow edits, run focused tests
first, then run `make check` once the change is ready.

---

## Operator install / upgrade

The **maintainer-as-operator** persona — a contributor running their own MCP server locally and wanting the latest merged change live in their MCP client — uses the global npm install path:

```bash
# After your change merges to main and the auto-tag → release pipeline publishes the new version:
npm install -g talos-mcp@latest
talos-mcp --version    # verify the commit hash matches HEAD on origin/main
```

Discovery commands if the install path is unclear on a given machine:

```bash
which talos-mcp                     # e.g. /opt/homebrew/bin/talos-mcp (macOS, npm-global)
readlink $(which talos-mcp)         # symlink target → npm-prefix/lib/node_modules/talos-mcp/bin/run.js
npm list -g talos-mcp               # currently installed version
```

Full distribution-side docs (npx / binary / build-from-source variants) live in [README.md § Installation](./README.md#installation). The auto-publish mechanism that makes `@latest` meaningful within minutes of a merge is in [CONTRIBUTING.md § Post-merge release pipeline](./CONTRIBUTING.md#post-merge-release-pipeline).

---

## Agent Tooling

GitHub operations use the `mcp__github__*` MCP tools, not the `gh` CLI. MCP tools are semantically richer, participate in the permission model, and are the intended interface for GitHub in an agent session.

Reserve `gh` CLI for operations without an MCP equivalent:

- `gh pr checks <n>` — CI check-run status
- `gh auth login` — interactive auth setup
- `gh run view` — workflow run inspection

Never use `gh` for issues, PRs, reviews, comments, branches, labels, or search when an `mcp__github__*` tool exists. In particular, prefer `mcp__github__pull_request_read` with `method=get` over `gh pr view` for state and merged-at checks.

---

## Planning Discipline

Non-trivial plans must pass `senior-plan-reviewer` **and** one adversarial pass before `ExitPlanMode`. Non-trivial = multi-phase, multi-PR, touches mutating tools, or proposes an architectural change.

- Run the two reviewers in parallel with distinct perspectives: `senior-plan-reviewer` for completeness and architecture; a `general-purpose` or `codex:codex-rescue` agent for adversarial risk surfaces the primary reviewer may endorse by default.
- Fold every review finding into the plan file, or document why it is accepted as-is. Empirical note: adversarial passes have caught sequencing gaps, skill-filter dead paths, and invalid label taxonomy that the senior reviewer accepted — the two roles are complementary, not redundant.
- Trivial plans (single-line fix, typo, rename) may `ExitPlanMode` direct.

---

## Coding Conventions

### Baseline standards

This project follows these community standards. Read them before contributing:

- [Effective Go](https://go.dev/doc/effective_go) — the official style baseline
- [Go Code Review Comments](https://go.dev/wiki/CodeReviewComments) — the official checklist
- [Uber Go Style Guide](https://github.com/uber-go/guide/blob/master/style.md) — widely-adopted supplement

The linter (`make lint`) enforces formatting, vet, gosec, errorlint, gocritic, and revive.
Always run `make check` before opening a PR.

### Handler signature

All MCP tool handlers follow this exact signature:

```go
func (h *Handlers) HandleXxx(ctx context.Context, req *mcp.CallToolRequest, args XxxArgs) (*mcp.CallToolResult, any, error)
```

- Unused `req` is named `_`
- Each tool has a dedicated `XxxArgs` struct in the same file
- Args fields use `json` + `jsonschema` tags; optional fields use `omitempty`
- Use pointer types (`*bool`) when nil must be distinguished from false (e.g. `DryRun *bool`, `Preserve *bool`)
- Return responses via `textResult()` or `jsonMarshal()` helpers (never construct `mcp.CallToolResult` inline)

### Tool and prompt inventory maintenance

When adding, removing, or renaming a **tool**, **prompt**, or **resource**, update **all** surfaces that reference them:

| Surface | What to update |
|---------|----------------|
| `CLAUDE.md` | Safety section — update if adding, removing, or changing a mutating tool guard |
| `server.json` | `description` field — remove or rephrase if it references specific capabilities |
| `README.md` | Feature list or tool table (if present) |
| This file | The inventory blocks below (populated by `/refresh-tool-inventory --apply`) |

Run a repo-wide search for the old tool/prompt name before committing to catch stale references:

```bash
grep -r "old_tool_name" --include="*.go" --include="*.md" --include="*.json" .
```

#### Current inventory

The blocks below are regenerated by the `refresh-tool-inventory` skill from the registration sites in `internal/tools/register.go`, `internal/prompts`, and `internal/resources`. Do not hand-edit between the sentinel markers — changes are overwritten.

<!-- inventory:tools:start -->
| Tool | Kind | Handler (file:line) | Description |
|---|---|---|---|
| `talos_resource_definitions` | read-only | `internal/tools/resources.go:HandleResourceDefinitions` | List all available resource types with their canonical names and aliases. |
| `talos_get` | read-only | `internal/tools/resources.go:HandleGetResource` | Get or list COSI resources by type. Supports `insecure=true` + `endpoint` for maintenance mode. |
| `talos_version` | read-only | `internal/tools/system.go:HandleVersion` | Get Talos version info from target nodes. Supports `insecure=true` + `endpoint`. |
| `talos_services` | read-only | `internal/tools/system.go:HandleServices` | List Talos services and their state. |
| `talos_containers` | read-only | `internal/tools/system.go:HandleContainers` | List CRI containers in a namespace. |
| `talos_processes` | read-only | `internal/tools/system.go:HandleProcesses` | List running processes on target nodes. |
| `talos_health` | read-only | `internal/tools/system.go:HandleHealth` | Check cluster health (etcd, k8s, node readiness). |
| `talos_logs` | read-only | `internal/tools/logs.go:HandleLogs` | Fetch recent service logs. |
| `talos_dmesg` | read-only | `internal/tools/logs.go:HandleDmesg` | Read kernel ring buffer. |
| `talos_events` | read-only | `internal/tools/logs.go:HandleEvents` | Fetch recent Talos runtime events. |
| `talos_etcd` | read-only | `internal/tools/etcd.go:HandleEtcd` | Query etcd cluster: `members` (default) or `status`. |
| `talos_etcd_snapshot` | read-only | `internal/tools/etcd.go:HandleEtcdSnapshot` | Stream an etcd snapshot to a local file. |
| `talos_list_files` | read-only | `internal/tools/files.go:HandleListFiles` | List files/directories on node filesystem. |
| `talos_read_file` | read-only | `internal/tools/files.go:HandleReadFile` | Read file contents from node filesystem. |
| `talos_validate` | read-only | `internal/tools/validate.go:HandleValidate` | Validate a machine config offline (no cluster connection). |
| `talos_service_action` | mutating | `internal/tools/lifecycle.go:HandleServiceAction` | Start/stop/restart a service. `confirm=true` required. |
| `talos_reboot` | mutating | `internal/tools/lifecycle.go:HandleReboot` | Reboot nodes. `confirm=true` + explicit `nodes`. |
| `talos_upgrade` | mutating | `internal/tools/lifecycle.go:HandleUpgrade` | Upgrade Talos installer. `confirm=true` + `nodes` + `image`. |
| `talos_rollback` | mutating | `internal/tools/lifecycle.go:HandleRollback` | Roll back previous upgrade. `confirm=true` + `nodes`. |
| `talos_patch_config` | mutating | `internal/tools/lifecycle.go:HandlePatchConfig` | Apply config patch. `dry_run` defaults to `true`; `confirm=true` when `dry_run=false`. |
| `talos_reset` | mutating | `internal/tools/lifecycle.go:HandleReset` | Wipe and factory-reset nodes (irreversible). `confirm=true` + `nodes`. |
| `talos_apply_config` | mutating | `internal/tools/lifecycle.go:HandleApplyConfig` | Apply complete machine config. Supports `insecure=true` + `endpoint` for maintenance mode. |
| `talos_meta` | mutating | `internal/tools/meta.go:HandleMeta` | Read/write/delete META partition keys. Supports `insecure=true` + `endpoint`. `write`/`delete` require `confirm=true` + safelisted key. |
<!-- inventory:tools:end -->

<!-- inventory:prompts:start -->
<!-- inventory:prompts:end -->

<!-- inventory:resources:start -->
<!-- inventory:resources:end -->

### Mutating tool safety pattern

All mutating handlers follow this strict order — no exceptions:

1. `auditLog()` — first, always
2. Guard checks: `confirm` required, `nodes` required, enum fields via exhaustive `switch` with a `default` error branch
3. Input validation (parse timeouts, validate modes) before any side effects
4. gRPC call
5. `mcpLogError()` on failure

### Error handling

- Wrap with `fmt.Errorf("lowercase context verb: %w", err)` — no capital first letter, no trailing punctuation
- Use `errors.Is()` / `errors.As()` to check wrapped errors — never `==` on non-sentinel errors
- Sentinel errors are prefixed with `Err`: `var ErrFoo = errors.New("package: foo")`
- Never discard errors silently; use `//nolint:errcheck` with a comment explaining why if unavoidable
- In `stream.Recv()` loops, check `io.EOF` explicitly — non-EOF errors must be returned, not swallowed

### Imports

Three groups, separated by blank lines:

```go
import (
    "context"           // 1. stdlib
    "fmt"

    "github.com/foo/bar" // 2. third-party

    "github.com/Nosmoht/talos-mcp-server/internal/talos" // 3. internal
)
```

### Context propagation

- Every function doing I/O takes `ctx context.Context` as the first parameter
- Never recreate `context.Background()` mid-call-chain — propagate the caller's context
- Use `context.WithTimeout` for gRPC deadlines

### Testing

- Table-driven tests with `t.Run(tc.name, func(t *testing.T) {...})`
- Test names: `TestHandleFoo_GuardCondition` or `TestHandleFoo_ValidInput`
- Stdlib `testing` only — no assertion libraries
- Use `safeH()` factory for guard-logic tests that don't need a live gRPC connection
- Mark test helper functions with `t.Helper()`
- Integration tests (requiring a live cluster) go in `*_integration_test.go` with build tag `//go:build integration`

### Structured logging

- Use `log/slog` with structured key-value fields for MCP client notifications
- Server-side audit logging uses `log.Printf` (intentional — see `helpers.go` for the dual-log design)
- No `log.Printf` for anything other than audit events

### Security patterns

- Path arguments: `filepath.Clean` + `strings.HasPrefix` allowlist check before use
- Token comparison: `subtle.ConstantTimeCompare` — never `==` on secret values
- Audit logs redact sensitive content (patch bodies, credentials)
- File reads bounded by `io.LimitReader`
- No panics in handler code — return errors; reserve `panic` for init-time programmer bugs

### Output trust boundaries

LLM-generated and user-provided strings entering this MCP server's handlers are
untrusted. Talos-specific points beyond the generic security patterns above:

- **`talos_patch_config` `args.Patch`** — validate UTF-8 and non-empty before
  the `[]byte(args.Patch)` cast. The cast itself is safe for gRPC transport;
  the Talos API performs schema validation server-side.
- **Enum / action fields in tool args** — handle via exhaustive `switch`
  statements with a `default` error case. Never pass user-supplied strings
  through to the Talos API directly.
- **No string interpolation into shell** — avoid `exec.Command("sh", "-c", X)`
  entirely. Use argv arrays or strict allowlists for `talosctl` / `kubectl`
  invocations if a sub-process is unavoidable.
- **Tool-result data re-entering context** — gRPC responses (`talos_get`,
  `talos_logs`, `talos_read_file`, etc.) flow back into the LLM context.
  A compromised node could attempt prompt injection via that channel; see
  `README.md` "What Is Not in the Threat Model". Never `eval` or execute
  tool-result content downstream.

---

## Finding Work

Search for open, unclaimed issues:

```
repo:Nosmoht/talos-mcp-server is:issue is:open label:"status: ready" sort:created-asc
```

Process issues in priority order:

1. `priority: P0` — Critical, drop everything
2. `priority: P1` — High, next up
3. `priority: P2` — Medium, scheduled
4. `priority: P3` — Low, backlog

Issues without a priority label: treat as `priority: P3` until triaged.
Issues carrying `needs: triage`: require priority/area assignment before work begins.

---

## Claiming an Issue

Use the two-phase claim protocol to prevent race conditions between concurrent agents.

### Phase 1: Verify availability

1. **Read labels** — confirm `status: ready` is present. If `status: assigned`,
   `status: in-progress`, or `agent: claimed` is present, the issue is taken. Stop.

2. **Read comments** — scan all existing comments. If any comment body contains
   `<!-- agent-claim:`, at least one agent has already attempted a claim. Back off.

### Phase 2: Claim and confirm

3. **Post a claim comment** (replace `{session-id}` with a stable identifier like
   `claude-<random-suffix>`, and `{ISO-8601 timestamp}` with the current UTC time):

   ```
   <!-- agent-claim: {session-id} -->
   **Claimed** by agent `{session-id}` at {ISO-8601 timestamp}. Branch: `feat/{change-id}`
   ```

4. **Re-read all comments** — if your comment is NOT the earliest `<!-- agent-claim:`
   comment by creation timestamp, another agent claimed first. Post and stop:

   ```
   <!-- agent-unclaim: {session-id} -->
   Backing off, already claimed.
   ```

5. **Update labels** — if your comment is earliest: remove `status: ready`; add
   `status: assigned` and `agent: claimed`. Always send the complete desired label set
   (GitHub replaces the entire list on update).

---

## Label Rules

| Group | Cardinality | Prefix | Values |
|---|---|---|---|
| Status | Exactly one | `status:` | `ready`, `assigned`, `in-progress`, `review-pending`, `blocked` |
| Priority | Exactly one | `priority:` | `P0` (critical), `P1` (high), `P2` (medium), `P3` (low) |
| Type | Exactly one | `type:` | `bug`, `enhancement`, `chore`, `docs`, `test` |
| Area | One or more | `area:` | `tools`, `resources`, `prompts`, `transport`, `client`, `version`, `ci`, `npm`, `docs`, `governance` |
| Size | Exactly one | `size:` | `XS` (<30 min), `S` (1–2 h), `M` (half day), `L` (full day), `XL` (multi-day) |
| Origin | Zero or one | `origin:` | `audit` (finding from code review or security audit) |
| Coordination | Zero or more | `agent:`, `needs:` | `agent: claimed`, `needs: triage`, `needs: decomposition`, `needs: clarification` |

When updating labels, always write the **complete desired set** — never append individual labels
without also removing conflicting labels in the same call.

---

## Label State Machine

```
                    +-------------+
                    |  (no status)|
                    |   (new)     |
                    +------+------+
                           |
                    [triage/label]
                           |
                           v
                    +------+------+
              +---->|   ready     |<----+
              |     +------+------+     |
              |            |            |
              |     [claim protocol]    |
              |            |            |
              |            v            |
              |     +------+------+     |
              |     |  assigned   |     |
              |     +------+------+     |
              |            |            |
              |     [work starts]       |
              |            |            |
              |            v            |
              |     +------+-------+    |
              |     | in-progress  |    |
              |     +------+-------+    |
              |            |            |
              |     [PR opened]         |
              |            |            |
              |            v            |
              |     +------+--------+   |
              |     |review-pending |   |
              |     +------+--------+   |
              |            |            |
              |     [merged/closed]     |
              |            |            |
              |            v            |
              |     +------+------+     |
              |     |    done     |     |
              |     +-------------+     |
              |                         |
              +---[unclaim/abandon]-----+
```

### Transitions

| From | To | Trigger | Label changes |
|---|---|---|---|
| (new) | ready | Issue triaged | add `status: ready` |
| ready | assigned | Claim protocol step 5 | remove `status: ready`; add `status: assigned`, `agent: claimed` |
| assigned | in-progress | First commit pushed to branch | remove `status: assigned`; add `status: in-progress` |
| in-progress | review-pending | PR opened | remove `status: in-progress`; add `status: review-pending` |
| review-pending | done | PR merged/issue closed | remove `status: review-pending`; set `status: done` (via project-sync) |
| any | blocked | Blocker identified | add `status: blocked`; retain prior status label |
| blocked | (prior status) | Blocker resolved | remove `status: blocked`; post unblock comment |
| any | ready | Agent abandons | remove `status: assigned`, `status: in-progress`, `agent: claimed`; add `status: ready`; post comment |

### Blocked side-state

`status: blocked` is additive — keep the prior status label alongside it. When resolved:

1. Remove `status: blocked`.
2. Restore the prior status label.
3. Post: `<!-- agent-unblock: {session-id} -->\nBlocker resolved: {description}.`

---

## Commit Signing

The `main` branch requires verified commit signatures. Commits pushed to `main` must be signed — either by the committer's local key or by GitHub when merging via the web UI.

**For AI agents committing locally:** Configure SSH signing before committing:

```bash
# One-time setup (if not already configured globally)
git config --global gpg.format ssh
git config --global user.signingkey ~/.ssh/id_ed25519.pub
git config --global commit.gpgsign true
```

Then commit normally — git will sign automatically.

**For merging PRs:** Use **Squash and merge** via the GitHub web UI. GitHub signs the resulting commit automatically, satisfying the branch protection rule. Do **not** merge locally or via API without a signing key configured.

**Do not bypass signing** (`--no-gpg-sign`, `-c commit.gpgsign=false`) — the branch protection will block the merge.

---

## Opening a PR

When implementation is ready for review:

1. Run `make check` (fmt + vet + lint + test) and fix any failures.
2. Push your branch (`feat/{change-id}` or `fix/{change-id}`).
3. Open a PR using the template at `.github/PULL_REQUEST_TEMPLATE.md`.
   Title format: `feat(scope): short description [review:{change-id}]`
4. Include `Closes #{issue-number}` in the PR body.
5. Update issue labels: remove `status: in-progress`; add `status: review-pending`.
6. Remove `agent: claimed` only after the PR is merged or abandoned.

---

## Stale Claim Recovery

If an issue has `status: assigned` or `status: in-progress` with `agent: claimed`
but no branch push in >24 hours, any agent may reclaim:

1. Post a comment noting the stale claim and recovery action.
2. Run the full two-phase claim protocol as if the issue were `status: ready`.
3. Reset labels: remove `status: assigned`, `status: in-progress`, `agent: claimed`; treat as `status: ready`.

---

## Sub-issue Decomposition

Apply `needs: decomposition` when any of these are true:

- Size is `size: XL` and work spans more than two packages.
- More than three independently testable acceptance criteria.
- Multiple agents would need to modify the same files concurrently.

To decompose:

1. Add `needs: decomposition` to the parent issue.
2. Create child issues, each scoped to a single package or concern.
3. Reference parent in each child: `Part of #N`.
4. Add `status: ready` and appropriate `area:` / `size:` labels to each child.
5. Remove `needs: decomposition` from parent once all children are created.

---

## Worktree Workflow

Every code change is developed in an isolated git worktree to keep the main
working directory clean and allow parallel work.

### Setup

```bash
# Create worktree for a feature or fix (slug = change-id)
git worktree add -b feat/<change-id> .claude/worktrees/<change-id> main
# or: git worktree add -b fix/<change-id> .claude/worktrees/<change-id> main
```

Work inside `.claude/worktrees/<change-id>/` for the full lifecycle of the change.

### When to use a worktree

- **Always**: any change that will result in a commit (code, docs, config, CI)
- **Not needed**: exploration, research, reading files, running tests without changes

### Branch naming

Match the branch name to the change-id: `feat/<change-id>` or `fix/<change-id>`.

### Rebase before push

```bash
git fetch origin main && git rebase origin/main
```

### Cleanup after merge

```bash
git worktree remove .claude/worktrees/<change-id>
git branch -d feat/<change-id>
```

### Follow-up bake PRs

When a review produces both code-level fixes and reusable lessons worth encoding into `.claude/rules/`, `.claude/skills/`, or `.claude/agents/`, split into two PRs:

1. Code-fix PR on the original feature branch.
2. Bake PR on a fresh branch off `main`, created **only after** the code-fix PR merges.

**Why:** the primitive files reference specific code paths (new helpers, new test invariants, renamed packages). Branching the bake off an open PR leaves the rule or agent checklist pointing at files that do not yet exist on `main`.

Verify the merge gate using an MCP call (not `gh pr view` — see § Agent Tooling):

```
mcp__github__pull_request_read method=get owner=<owner> repo=<repo> pullNumber=<n>
→ verify .state == "MERGED" and .mergedAt != null
```

Then:

```bash
git worktree add -b feat/<bake-slug> .claude/worktrees/<bake-slug> origin/main
```

If the review also surfaced **deferred process debt** (a test-suite waiver, a known retrofit, a blocked guard invariant), file a real GitHub issue with valid label taxonomy (`type: chore` or `type: bug`, one `priority:` label, `area:` labels, `needs: triage`) and reference both the merged source PR **and** the tracking issue in the bake commit message and PR body. Test-code comments and `knownXxxGapTools` maps are waivers, not trackers — the tracker goes on GitHub.

---

## Review Governance

Every change to tracked files requires a review artifact before commit.

### Install the pre-commit hook (one-time per clone)

```bash
cp .claude/hooks/pre-commit .git/hooks/pre-commit && chmod +x .git/hooks/pre-commit
```

### Review flow

1. **Implement** in a worktree (see above).
2. **Review** — invoke `staff-reviewer` agent. Artifact: `.claude/reviews/{change-id}/review.md`
   - `status: approved` → proceed to commit
   - `status: escalate` → invoke each listed escalation reviewer
3. **Escalation** (if needed) — invoke the domain reviewer. Artifact: `.claude/reviews/{change-id}/review-{type}.md`
4. **Commit** — only once all required artifacts show `status: approved`.

### PR-level review before merge

A PR that lacks an existing GitHub approval must be reviewed by a subagent (typically `staff-reviewer`) before merge. If the review surfaces findings, fix them and re-review until clean. Batch-merge flows do not waive the gate — every PR carries its own review. Commit-time reviews (pre-commit hook) are a separate gate; the merge step itself is manual and relies on this rule.

### Review depth

| Change type | Required review |
|---|---|
| `docs` / `chore` / `ci` | `staff-reviewer` → `review.md` approved |
| Code — simple | `staff-reviewer` → `review.md` approved |
| Code — complex | `staff-reviewer` escalates → domain reviewer(s) |

### Escalation criteria

| Type | Reviewer | Triggers |
|---|---|---|
| `operational-safety` | `operational-safety-reviewer` | New/modified mutating tool, guard logic, audit logging, read-only enforcement |
| `provenance` | `provenance-reviewer` | `go.mod` or `go.sum` modified, new external import |
| `compatibility` | `compatibility-reviewer` | Tool/prompt/resource signature change, SDK bump, tool removal |
| `architecture` | `principal-architect-reviewer` | New package, >3 packages modified, structural refactor, API surface addition |
| `security` | `security-reviewer` | Auth/token handling, input validation, hook/enforcement logic |
| `performance` | `performance-reviewer` | gRPC streaming, goroutine lifecycle, hot-path caching |

### Change-id convention

Use semantic slugs (e.g., `fix-health-timeout`, `add-etcd-defrag-tool`). Include in commit message:

```
feat(etcd): add defrag tool [review:add-etcd-defrag-tool]
```

### Role separation

The implementing agent must not serve as the approving reviewer for the same change.

### Artifact storage

Review artifacts (`.claude/reviews/`) are local-only (gitignored). They act as process gates
for the pre-commit hook. The `[review:change-id]` tag in the commit message is the permanent
audit trail. See [CLAUDE.md — Change Governance](./CLAUDE.md#change-governance) for hook enforcement details.
