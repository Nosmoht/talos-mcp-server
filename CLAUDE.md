# talos-mcp

MCP server exposing Talos Linux cluster management to AI agents via the native gRPC API.

## Build

```bash
make build   # binary with version info
make check   # full CI parity: fmt + vet + lint + test
```

## Safety

Mutating tools require explicit guards — missing any of these will error or cause irreversible cluster damage:

- `talos_reboot`, `talos_upgrade`, `talos_rollback`, `talos_reset` all require `confirm=true` and explicit `nodes`.
- `talos_reboot` hits **all listed nodes simultaneously** — specify one at a time to avoid a full outage. Use `wait=true` to block until complete (default timeout `5m`).
- `talos_upgrade` `preserve` defaults to `true` (keep EPHEMERAL partition) — differs from `talosctl` default of `false`.
- `talos_reset` `graceful` defaults to `true` (drain workloads, leave etcd). Set `false` only on unresponsive nodes. `system_labels_to_wipe` empty = full disk wipe.
- `talos_patch_config` defaults `dry_run=true`; pass `dry_run=false` + `confirm=true` to apply.
- `talos_apply_config` takes `config_file` (absolute local path to YAML/JSON, max 1 MiB — secrets never enter context). Defaults `dry_run=true`, requires exactly one node. Replaces entire machine config — prefer `talos_patch_config` for targeted changes.

Quorum/member-count invariants for preflight helpers: see `.claude/rules/quorum-member-counting.md` (auto-loaded when editing `internal/tools/etcd_preflight.go` or any `*_preflight.go`).

### Safety Profile

`TALOS_MCP_SAFETY_PROFILE` seeds four gating flags at startup. Individual env vars override the profile. When the profile var is unset, each flag defaults to its own env var (backwards-compatible).

| Profile | `READ_ONLY` | `ALLOW_CLUSTER_WIDE` | `ENABLE_GEN` | `SKIP_VERSION_CHECK` |
|---|---|---|---|---|
| `conservative` | `true` | `false` | `false` | `false` |
| `standard` | `false` | `false` | `false` | `false` |
| `expert` | `false` | `true` | `true` | `false` |

`ALLOW_CLUSTER_WIDE` and `ENABLE_GEN` are reserved for future phases (D — cluster CA/K8s rotation, E — offline PKI gen) and currently gate no tools. The effective profile is logged at startup (`slog.Info("safety profile", ...)`).

## Development Workflow

Every change that results in a commit must use a git worktree — never commit directly on `main`.

```bash
git fetch origin main
git worktree add -b feat/<slug> .claude/worktrees/<slug> origin/main
# fix/<slug> for bug fixes
```

- Branch naming: `feat/<change-id>` or `fix/<change-id>`
- Rebase before push: `git fetch origin main && git rebase origin/main`
- **Always use `git -C <absolute-path>`** in Bash — each shell invocation starts fresh, `cd` does not persist.

## Project Management

Issue lifecycle, coding conventions, and multi-agent coordination: [AGENTS.md](./AGENTS.md).

Find ready issues: `repo:Nosmoht/talos-mcp-server is:issue is:open label:"status: ready" sort:created-asc`

## Release

Conventional commit prefixes on merge to `main` (server paths only): `fix:` → patch · `feat:` → minor · `BREAKING CHANGE:`/`feat!:` → major · `docs:`/`ci:`/`chore:` → no release.

npm OIDC gotchas: see `.claude/rules/release-workflow.md` (auto-loaded when editing `release.yml`).

## GitHub Issues

Use `--body-file` for issue bodies with backticks — heredocs with `--body` corrupt markdown code blocks.

**Bug/chore body sections:** `## Description` · `## Evidence` · `## Impact` · `## Recommended Fix` · `## Audit Source`

**Feature request body sections:** `### Severity` · `### Category` · `### Description` · `### Evidence` · `### Impact` · `### Recommended Fix` · `### Audit Source`

Minimum labels: one `type:`, one `priority:`, `needs: triage`. Full taxonomy: [AGENTS.md § Label Rules](./AGENTS.md).

## Change Governance

Every change to tracked files requires a review artifact before commit.

Invoke `staff-reviewer` after implementing. Artifact: `.claude/reviews/<change-id>/review.md`
- `status: approved` → commit · `status: escalate` → invoke each listed reviewer

Full review flow and escalation criteria: [AGENTS.md § Review Governance](./AGENTS.md#review-governance).

Commit tag format: `feat(scope): description [review:<change-id>]` — the tag in the commit message is the permanent audit trail.

Review artifacts (`.claude/reviews/`) are **gitignored local-only** process gates for the pre-commit hook. Install hook (one-time per clone):

```bash
cp .claude/hooks/pre-commit .git/hooks/pre-commit && chmod +x .git/hooks/pre-commit
```

`.claude/settings.json` hooks block `git commit` (Bash) and MCP GitHub push tools without a valid review artifact. The implementing agent must not review the same change. When uncertain about API behavior or prior decisions: invoke the `researcher` agent.

## Configuration

Environment variables and compatibility range: see [README.md § Configuration](./README.md#configuration).
