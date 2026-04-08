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
| `CLAUDE.md` | Tools, Prompts, or Resources section — add/remove entry; update count in the section heading (e.g. `## Tools (18)`) |
| `server.json` | `description` field — remove or rephrase if it references specific capabilities |
| `README.md` | Feature list or tool table (if present) |

Run a repo-wide search for the old tool/prompt name before committing to catch stale references:

```bash
grep -r "old_tool_name" --include="*.go" --include="*.md" --include="*.json" .
```

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
| Area | One or more | `area:` | `tools`, `resources`, `prompts`, `transport`, `client`, `version`, `ci`, `npm`, `docs`, `governance` |
| Size | Exactly one | `size:` | `XS` (<30 min), `S` (1–2 h), `M` (half day), `L` (full day), `XL` (multi-day) |
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
audit trail. See [CLAUDE.md — Change Governance](./CLAUDE.md#change-governance) for the full
artifact schema, hook enforcement details, and known limitations.
