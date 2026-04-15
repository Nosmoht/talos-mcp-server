# Guard Matrix

Authoritative guard requirements extracted verbatim from `CLAUDE.md` § Safety.
Mutating tools require explicit guards — missing any of these will error or
cause irreversible cluster damage.

## Source (CLAUDE.md § Safety)

- `talos_reboot`, `talos_upgrade`, `talos_rollback`, `talos_reset` all require `confirm=true` and explicit `nodes`.
- `talos_reboot` hits **all listed nodes simultaneously** — specify one at a time to avoid a full outage. Use `wait=true` to block until complete (default timeout `5m`).
- `talos_upgrade` `preserve` defaults to `true` (keep EPHEMERAL partition) — differs from `talosctl` default of `false`.
- `talos_reset` `graceful` defaults to `true` (drain workloads, leave etcd). Set `false` only on unresponsive nodes. `system_labels_to_wipe` empty = full disk wipe.
- `talos_patch_config` defaults `dry_run=true`; pass `dry_run=false` + `confirm=true` to apply.
- `talos_apply_config` takes `config_file` (absolute local path to YAML/JSON, max 1 MiB — secrets never enter context). Defaults `dry_run=true`, requires exactly one node. Replaces entire machine config — prefer `talos_patch_config` for targeted changes.

## Matrix

| tool_name            | required_args                          | default_values                                               | notes |
|----------------------|----------------------------------------|--------------------------------------------------------------|-------|
| `talos_reboot`       | `confirm=true`, `nodes`                | `wait=false` (timeout `5m` when `wait=true`)                 | Hits all listed nodes simultaneously — specify one at a time to avoid full outage. |
| `talos_upgrade`      | `confirm=true`, `nodes`                | `preserve=true`                                              | `preserve` default differs from `talosctl` default (`false`); keeps EPHEMERAL partition. |
| `talos_rollback`     | `confirm=true`, `nodes`                | —                                                            | Requires explicit confirm + nodes. |
| `talos_reset`        | `confirm=true`, `nodes`                | `graceful=true`, `system_labels_to_wipe=[]`                  | Set `graceful=false` only on unresponsive nodes. Empty `system_labels_to_wipe` = full disk wipe. |
| `talos_patch_config` | (to apply) `dry_run=false`, `confirm=true` | `dry_run=true`                                           | Safe-by-default: dry run unless explicitly overridden. |
| `talos_apply_config` | `config_file` (absolute path, YAML/JSON, ≤1 MiB), exactly one node | `dry_run=true`                             | Replaces entire machine config; prefer `talos_patch_config` for targeted changes. Secrets never enter context. |
