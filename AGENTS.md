# AGENTS.md

Machine-readable conventions for AI agents working on the talos-mcp repository.

## Overview

Multiple Claude Code agents may work concurrently on this repository via git
worktrees. This document defines the conventions for discovering work, claiming
issues, and coordinating across parallel agents without conflicts.

---

## Finding Work

Search for open, unclaimed issues using the following query:

```
repo:Nosmoht/talos-mcp-server is:issue is:open label:"status: ready" sort:created-asc
```

Process issues in priority order:

1. `priority: P0` — Critical, drop everything
2. `priority: P1` — High, next up
3. `priority: P2` — Medium, scheduled
4. `priority: P3` — Low, backlog

Issues without a priority label should be treated as `priority: P3` until
triaged. Issues carrying `needs: triage` require priority/area assignment
before work begins.

---

## Claiming an Issue

Use the two-phase claim protocol to prevent race conditions between concurrent
agents.

### Phase 1: Verify availability

1. **Read labels** — confirm `status: ready` is present. If `status: assigned`,
   `status: in-progress`, or `agent: claimed` is present, the issue is taken.
   Stop.

2. **Read comments** — scan all existing comments. If any comment body contains
   the HTML marker `<!-- agent-claim:`, at least one agent has already attempted
   a claim. Back off and choose a different issue.

### Phase 2: Claim and confirm

3. **Post a claim comment** with the following body (replace `{session-id}` with
   a stable identifier for your session, e.g. `claude-<random-suffix>`, and
   `{ISO-8601 timestamp}` with the current UTC time):

   ```
   <!-- agent-claim: {session-id} -->
   **Claimed** by agent `{session-id}` at {ISO-8601 timestamp}. Branch: `feat/{change-id}`
   ```

4. **Re-read all comments** — retrieve the full comment list again. If your
   comment is NOT the earliest `<!-- agent-claim:` comment by creation
   timestamp, another agent claimed first. Post the following and stop:

   ```
   <!-- agent-unclaim: {session-id} -->
   Backing off, already claimed.
   ```

5. **Update labels** — if your comment is the earliest claim, update the issue
   labels atomically. Remove `status: ready` and add `status: assigned` and
   `agent: claimed`. Provide the complete desired label set in the write call
   (GitHub replaces the entire label list on update).

---

## Label Rules

Labels are grouped into exclusive and multi-value sets:

| Group | Cardinality | Prefix |
|---|---|---|
| Status | Exactly one at a time | `status:` |
| Priority | Exactly one | `priority:` |
| Area | One or more | `area:` |
| Size | Exactly one | `size:` |
| Coordination | Zero or more | `agent:`, `needs:`, standalone |

When updating labels, always write the complete desired set. Never append
individual labels without also removing conflicting labels in the same call.

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

`status: blocked` is additive — keep the prior status label alongside it so
the history is clear. When a blocker is resolved:

1. Remove `status: blocked`.
2. Restore the prior status label (e.g. `status: in-progress`).
3. Post a comment: `<!-- agent-unblock: {session-id} -->\nBlocker resolved: {description}.`

---

## Opening a PR

When the implementation is ready for review:

1. Push your branch (`feat/{change-id}` or `fix/{change-id}`).
2. Open a PR with title following conventional commits format:
   `feat(scope): short description [review:{change-id}]`
3. Include `Closes #{issue-number}` in the PR body.
4. Update issue labels: remove `status: in-progress`; add `status: review-pending`.
5. Remove `agent: claimed` only after the PR is merged or abandoned.

---

## Stale Claim Recovery

If an issue carries `status: assigned` or `status: in-progress` and `agent: claimed`
but no branch has been pushed in more than 24 hours, any agent may reclaim it:

1. Post a comment noting the stale claim and the recovery action.
2. Follow the full claim protocol from step 1 as if the issue were `status: ready`.
3. Reset labels: remove `status: assigned`, `status: in-progress`, `agent: claimed`;
   treat as `status: ready` during the claim check.

---

## Sub-issue Decomposition

Apply `needs: decomposition` when an issue meets any of these criteria:

- Estimated size is `size: XL` and the work spans more than two packages.
- The issue contains more than three distinct acceptance criteria that are
  independently testable.
- Multiple agents would need to modify the same files concurrently.

To decompose:

1. Add `needs: decomposition` to the parent issue.
2. Create child issues, each scoped to a single package or concern.
3. Reference the parent in each child: `Part of #N`.
4. Add `status: ready` and appropriate `area:` / `size:` labels to each child.
5. Remove `needs: decomposition` from the parent once all children are created.

---

## Review Governance

All code changes follow the review governance policy defined in
[CLAUDE.md](./CLAUDE.md#change-governance). Key points:

- Every change requires a `staff-reviewer` review artifact at
  `.claude/reviews/{change-id}/review.md` before commit.
- Complex changes may require escalation reviewers (operational-safety,
  security, architecture, etc.).
- The implementing agent must not serve as the approving reviewer.
- Commit messages must include `[review:{change-id}]`.

See CLAUDE.md for the full policy, escalation criteria, and artifact schema.
