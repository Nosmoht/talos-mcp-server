---
change-id: feat-talosctl-parity
review-type: impl-review
reviewer-role: staff-reviewer
status: approved
timestamp: 2026-04-06T19:45:00Z
reviewed-scope:
  - internal/tools/helpers.go
  - internal/tools/lifecycle.go
  - internal/tools/system.go
  - cmd/talos-mcp/main.go
  - internal/tools/lifecycle_test.go
  - internal/prompts/upgrade.go
  - CLAUDE.md
  - /Users/thomaskrahn/go/pkg/mod/github.com/siderolabs/talos/pkg/machinery@v1.12.6/client/client.go (lines 316-600)
  - internal/talos/client.go (for InvalidateVersionCache and Rollback proxy)
findings: []
---

## Notes

### Plan review artifact

`plan-review.md` is present in `.claude/reviews/feat-talosctl-parity/` and carries `status: approved` with zero findings. Prerequisite satisfied.

### SDK usage — correct

All five `UpgradeOption` constructors (`WithUpgradeImage`, `WithUpgradePreserve`, `WithUpgradeStage`, `WithUpgradeForce`, `WithUpgradeRebootMode`) are used in the correct order with the correct argument types, verified against `client.go` lines 530–563. The `UpgradeWithOptions` call at lifecycle.go:177 matches the SDK signature at client.go:585.

`Rollback(ctx)` is called with no additional arguments (lifecycle.go:218), matching the SDK signature at client.go:368 which takes only `ctx context.Context`. The return is `error` only; the handler correctly returns a plain text success string rather than attempting to marshal a nil response.

`WithForce` and `WithPowerCycle` are `RebootMode` (func) values appended to `[]talosclient.RebootMode` — usage is identical to the pre-existing `WithPowerCycle` case and is correct.

`ClusterHealthCheck` third argument is now populated with `args.ControlPlaneNodes` and `args.WorkerNodes` (system.go:125-128). When both are nil/empty slices the proto message is equivalent to the old empty struct — no regression.

### Helper — correct

`resolvePreserve` (helpers.go:123-125) mirrors `resolveDryRun` exactly in structure and comment quality. The divergence from talosctl default is documented in both the function comment and the struct tag.

### Guard patterns — consistent

All three new/expanded handlers follow the established pattern:
1. `auditLog` before any guards (so every invocation is logged, even rejected ones)
2. `confirm` guard first, `nodes` guard second
3. Mode validation before `talos.WithNodes` (fail fast before mutating context)
4. Error messages use the `"<tool> refused: <reason>"` prefix consistently

`HandleRollback` correctly calls `ctx = talos.WithNodes(ctx, args.Nodes)` at line 214 before the gRPC call — the plan's minor observation (point 1) is addressed.

### Cache invalidation — correct

`h.Client.InvalidateVersionCache()` is called after successful `UpgradeWithOptions` (lifecycle.go:184) and after successful `Rollback` (lifecycle.go:224). Both are placed on the success path only — a failed upgrade/rollback does not clear the cache, which is the correct behavior. The method is mutex-protected in `internal/talos/client.go:92-97`.

### Error wrapping — correct

All `fmt.Errorf` calls in new code use `%w` for error wrapping. No `%v` misuse found.

### Test coverage

All new validation paths exercise the `safeH()` nil-client pattern and return before touching gRPC, making them safe as pure unit tests. The table is correct:

| Test | Path covered |
|------|-------------|
| `TestHandleRollback_Guards` | confirm=false, nodes=nil, nodes=[] |
| `TestHandleUpgrade_InvalidRebootMode` | unknown reboot_mode before WithNodes |
| `TestHandleReboot_InvalidMode` | unknown mode after confirm+nodes pass |
| `TestResolvePreserve` | nil→true, &true→true, &false→false |
| `TestResolveDryRun` | nil→true, &true→true, &false→false |

The plan's minor observation (point 2) about `TestHandleReboot_InvalidMode` test naming is reflected in the implementation: the test case is named `"unknown mode"` with `Confirm: true`, making it a clean mode-guard test rather than a confusing confirm-guard test. This is an improvement over the plan's concern.

`Force`, `Stage`, and `Preserve` are pass-through fields to the SDK with no independent guard logic, so absence of dedicated tests for those fields is acceptable — they would require a live gRPC mock to be meaningful.

### Documentation accuracy

CLAUDE.md tool count is updated to 16 (line 43), matching the addition of `talos_rollback`. The Safety section (line 89) correctly adds `talos_rollback` to the confirm+nodes requirement. The `talos_upgrade` entry (line 63) documents all four new parameters. The `talos_health` entry (line 52) documents the new override fields. The `talos_reboot` entry (line 62) now lists `force` as a supported mode.

The Logging section (line 95) now correctly lists `talos_rollback` in the mutating tools audit list: `talos_service_action`, `talos_reboot`, `talos_upgrade`, `talos_rollback`, `talos_patch_config`. The prior finding is resolved — documentation is now accurate.

`pre-upgrade-checklist` prompt (`internal/prompts/upgrade.go:75-78`) now documents `preserve`, `stage`, and the post-upgrade `talos_health` recommendation. The guidance is accurate and matches implementation behavior.

The tool description for `talos_rollback` in `main.go:231-236` accurately describes the operation, the limitation (only works if previous installation is intact), and the post-rollback verification step.

### Security

No credentials in any changed file. Input validation covers all enum-typed parameters (`mode`, `reboot_mode`). The `talos_rollback` guard structure matches `talos_reboot`/`talos_upgrade` — no weaker input handling introduced. The `talos_rollback` tool is correctly registered inside the `if !readOnly` block (main.go:202), so it is hidden when `TALOS_MCP_READ_ONLY=true`.

### Summary

The implementation is correct, follows all codebase patterns, and matches the approved plan. All findings from the initial review have been resolved. The sole finding — Logging section of CLAUDE.md omitting `talos_rollback` from the audit-event tool list — has been fixed at CLAUDE.md:95. Zero findings remain. Change is approved for commit.
