# Safety Defaults by Tool Category

Source of truth: repo root `CLAUDE.md` § Safety.

| Tool Category / Example      | confirm | dry_run | wait        | preserve | graceful | Notes                                                                 |
|------------------------------|---------|---------|-------------|----------|----------|-----------------------------------------------------------------------|
| Read (e.g. `talos_version`)  | n/a     | n/a     | n/a         | n/a      | n/a      | No guards required.                                                   |
| `talos_reboot`               | true    | n/a     | false (5m)  | n/a      | n/a      | Hits all listed nodes simultaneously — specify one node at a time.    |
| `talos_upgrade`              | true    | n/a     | n/a         | true     | n/a      | `preserve=true` keeps EPHEMERAL; differs from talosctl default false. |
| `talos_rollback`             | true    | n/a     | n/a         | n/a      | n/a      | Requires explicit nodes.                                              |
| `talos_reset`                | true    | n/a     | n/a         | n/a      | true     | `graceful=true` drains workloads, leaves etcd; false = unresponsive.  |
| `talos_patch_config`         | true    | true    | n/a         | n/a      | n/a      | `dry_run=true` by default; set false + confirm=true to apply.         |
| `talos_apply_config`         | true    | true    | n/a         | n/a      | n/a      | Exactly one node; max 1 MiB YAML/JSON; replaces entire machine config.|

## Rules

- Mutating tools MUST require `confirm=true` and explicit `nodes`.
- Config-mutating tools MUST default `dry_run=true`.
- Never wipe secrets via context — `talos_apply_config` takes a local file path only.
- `system_labels_to_wipe` empty on `talos_reset` = full disk wipe; document loudly.

## Confirm-guard invariants (enforced by `internal/tools/invariants_test.go`)

- Every mutating tool MUST carry a `Confirm bool` field in its args struct. Every destructive tool registration MUST carry a `confirm` property in its `InputSchema`.
- The invariants test runs on every CI build: destructive tools never leak through `readOnly=true` registration, and every destructive tool's schema advertises `confirm`.
- If a new mutating tool legitimately cannot carry `Confirm` yet (rare — requires justification in the PR description), add it to `knownConfirmGapTools` in `invariants_test.go` **and** file a tracking GitHub issue (`type: chore`, `priority: P2`, `area: tools`, `origin: audit`) referencing the waiver. The `t.Logf` line is not a tracker — missing issues are a process failure. Reference: issue #156 is the canonical historical example of the full protocol lifecycle (waiver added → tracking issue → fix landed → waiver removed). The waiver is no longer active — the example is preserved because it documents every step of the workflow.

## Preflight helpers

Any new `*_preflight.go` helper that informs a mutating decision must follow `.claude/rules/quorum-member-counting.md`: dedup cluster members by ID (`uint64` for etcd) before counting, never trust `len(msg.GetMembers())`.
