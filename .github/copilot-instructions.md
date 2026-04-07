# GitHub Copilot Instructions — talos-mcp

This document describes the conventions for AI agents and GitHub Copilot working
on the talos-mcp repository. Read it before starting any issue or PR work.

---

## Finding Work

Search for open issues ready to work on:

```
repo:Nosmoht/talos-mcp-server is:issue is:open label:"status: ready" sort:created-asc
```

Priority order: P0 > P1 > P2 > P3.

---

## Claiming an Issue (Two-Phase Protocol)

Race conditions between concurrent agents are prevented by a two-phase protocol.

### Phase 1 — Verify availability

1. Check labels: `status: ready` must be present. If `status: assigned`,
   `status: in-progress`, or `agent: claimed` is present, stop — issue is taken.
2. Check comments: if any comment contains `<!-- agent-claim:`, back off.

### Phase 2 — Claim and confirm

3. Post a claim comment:
   ```
   <!-- agent-claim: {session-id} -->
   **Claimed** by agent `{session-id}` at {ISO-8601 timestamp}. Branch: `feat/{change-id}`
   ```

4. Re-read all comments. If your comment is NOT the earliest `<!-- agent-claim:`
   comment by timestamp, post:
   ```
   <!-- agent-unclaim: {session-id} -->
   Backing off, already claimed.
   ```
   Then stop.

5. If your comment is the earliest: update issue labels atomically.
   Remove `status: ready`; add `status: assigned` and `agent: claimed`.
   Always send the complete desired label set (GitHub replaces the entire list).

---

## Label Taxonomy

| Group | Cardinality | Prefix | Notes |
|---|---|---|---|
| Status | Exactly one | `status:` | See state machine below |
| Priority | Exactly one | `priority:` | P0 critical → P3 low |
| Area | One or more | `area:` | tools, resources, prompts, transport, client, version, ci, npm, docs, governance |
| Size | Exactly one | `size:` | XS < 30 min, S 1-2 h, M half day, L full day, XL multi-day |
| Coordination | Zero or more | `agent:`, `needs:` | claimed, decomposition, clarification, triage |

### Status state machine

```
ready --> assigned --> in-progress --> review-pending --> (done/closed)
                                  \--> blocked (additive, keeps prior status)
```

- On PR open: swap `status: in-progress` for `status: review-pending`.
- On blocker: add `status: blocked` (keep prior status); remove when resolved.
- On abandon: remove `status: assigned`/`in-progress`/`agent: claimed`; restore `status: ready`.

---

## Opening a PR

- Title: `feat(scope): description [review:{change-id}]` (conventional commits)
- Body must include: `Closes #{issue-number}`
- Update issue labels: swap `status: in-progress` → `status: review-pending`
- Use the PR template at `.github/PULL_REQUEST_TEMPLATE.md`

---

## Stale Claim Recovery

If `status: assigned` or `status: in-progress` with `agent: claimed` and no
branch push in >24 h, any agent may reclaim. Post a comment, run the full
two-phase protocol, then reset labels to `status: ready` before starting.

---

## Code Review Requirements

Every change requires a review artifact before commit. See
[CLAUDE.md — Change Governance](../CLAUDE.md#change-governance) for the full
policy. Key rules:

- Staff reviewer artifact: `.claude/reviews/{change-id}/review.md`
- Implementing agent must not be the reviewer
- Commit message must include `[review:{change-id}]`
- Run `make check` before opening a PR

---

## Worktree Workflow

```bash
# Create worktree
git worktree add -b feat/<change-id> .claude/worktrees/<change-id> main

# Work inside the worktree
cd .claude/worktrees/<change-id>

# Rebase before push
git fetch origin main && git rebase origin/main
```

See [AGENTS.md](../AGENTS.md) for full details.
