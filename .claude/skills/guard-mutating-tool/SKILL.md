---
name: guard-mutating-tool
description: Verify that a diff touching mutating Talos tools (reboot, upgrade, rollback, reset, patch_config, apply_config) preserves all required guards — confirm=true, explicit nodes, correct dry_run/preserve/graceful defaults. Run before requesting staff-reviewer.
allowed-tools: Read, Grep, Glob, Bash
argument-hint: "[--base <ref>]"
---

# Guard Mutating Tool

Scan the current diff against the `CLAUDE.md` § Safety matrix (cached in
`references/guard-matrix.md`) and emit a structured report the
`staff-reviewer` agent can consume directly.

## Arguments

Parse `$ARGUMENTS`:

- `--base <ref>` — base ref for the diff. Defaults to `origin/main`.

Validate the base ref:

```bash
git rev-parse --verify "${BASE:-origin/main}" >/dev/null \
  || abort "Unknown base ref: $BASE"
```

## Key Steps

1. **List changed mutating-tool files.**
   ```bash
   git diff --name-only "${BASE:-origin/main}"...HEAD -- 'internal/tools/*.go'
   ```
   Filter by basename to one of: `lifecycle`, `files`, `patch_blocklist`,
   or any `*_preflight` (files that implement `reboot`, `upgrade`,
   `rollback`, `reset`, `patch_config`, `apply_config`, or preflight
   helpers that gate those mutations). Exit early with verdict `GO` if
   the filtered list is empty.

2. **Load the guard matrix.** Read `references/guard-matrix.md`. Its
   `required_args` and `default_values` columns are the ground truth.

3. **Scan each file for guard presence.** For every matched file:
   ```bash
   git diff "${BASE:-origin/main}"...HEAD -U3 -- <file> \
     | grep -nE 'confirm|nodes|dry_run|preserve|graceful|wait'
   ```
   Cross-reference the hunks against the matrix row for the tool.

4. **Classify each guard.** Flag a regression when:
   - a required-arg check was removed (deletion of `if !confirm` / `len(nodes)==0`),
   - a default literal changed (e.g. `dry_run: true` → `dry_run: false`),
   - a new control-flow path reaches the mutating gRPC call without passing
     through the guard check.

4b. **Preflight helper check.** For any `*_preflight.go` in the filtered
    set, load `.claude/rules/quorum-member-counting.md` and apply its
    invariants against the diff (dedup by member `Id` before counting,
    `map[<id-type>]struct{}` structure, no reliance on
    `len(msg.GetMembers())`). Flag as regression if a preflight helper
    is introduced or modified without the dedup pattern.

5. **Emit the report.** Use the exact template below.

## Report Format

```
## Guard Audit — <change-id>

| tool              | required_args_ok | defaults_ok | evidence                    | verdict |
|-------------------|------------------|-------------|-----------------------------|---------|
| talos_reboot      | yes/no           | yes/no      | <file:line> `<quoted code>` | pass/fail |
| talos_upgrade     | ...              | ...         | ...                         | ...     |
| talos_rollback    | ...              | ...         | ...                         | ...     |
| talos_reset       | ...              | ...         | ...                         | ...     |
| talos_patch_config| ...              | ...         | ...                         | ...     |
| talos_apply_config| ...              | ...         | ...                         | ...     |

## Findings
- **<tool>.<guard>** — <pass|regress|missing>
  Evidence: <file:line> `<quoted code>`
  Validation: re-run `grep -nE '<pattern>' <file>` — guard still present.

## Verdict
<approved | escalate: guards regressed>
```

Omit rows for tools whose file was not touched.

## References

- `references/guard-matrix.md` — guard matrix mirrored verbatim from repo-root `CLAUDE.md` § Safety.
- `.claude/rules/quorum-member-counting.md` — preflight helper invariants (member-ID dedup, strict-majority rule).
