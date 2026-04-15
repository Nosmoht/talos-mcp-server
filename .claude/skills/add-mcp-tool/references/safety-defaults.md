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
