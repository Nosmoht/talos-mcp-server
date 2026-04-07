---
change-id: feat-talosctl-parity
review-type: plan-review
reviewer-role: senior-plan-reviewer
status: approved
timestamp: 2026-04-06T18:10:00Z
reviewed-scope:
  - /Users/thomaskrahn/.claude/plans/cuddly-juggling-moonbeam.md
  - /Users/thomaskrahn/workspace/talos-mcp/internal/tools/lifecycle.go
  - /Users/thomaskrahn/workspace/talos-mcp/internal/tools/helpers.go
  - /Users/thomaskrahn/workspace/talos-mcp/internal/tools/system.go
  - /Users/thomaskrahn/workspace/talos-mcp/internal/tools/lifecycle_test.go
  - /Users/thomaskrahn/workspace/talos-mcp/cmd/talos-mcp/main.go
  - /Users/thomaskrahn/go/pkg/mod/github.com/siderolabs/talos/pkg/machinery@v1.12.6/client/client.go (lines 316–600)
  - /Users/thomaskrahn/workspace/talos-mcp/internal/talos/client.go
findings: []
---

## Notes

### SDK surface verification

All referenced SDK symbols were verified against `client.go` (v1.12.6):

- `UpgradeWithOptions(ctx, opts ...UpgradeOption)` — exists at line 585. The plan correctly replaces the current 4-arg `Upgrade(ctx, image, stage, force)` call.
- `WithUpgradeImage`, `WithUpgradePreserve`, `WithUpgradeStage`, `WithUpgradeForce`, `WithUpgradeRebootMode` — all exported option constructors confirmed present (lines 531–563).
- `WithForce` — exists at line 341 as a package-level `RebootMode` func, same type as `WithPowerCycle`. The `append(opts, talosclient.WithForce)` pattern is consistent with existing code.
- `Rollback(ctx context.Context) error` — confirmed at line 368. Returns only an error; no response to marshal. Handler must return a plain text success string, not a marshaled response.
- `ClusterHealthCheck(ctx, waitTimeout, *clusterapi.ClusterInfo)` — confirmed at line 721. Third arg is already wired in the current code (empty struct); plan correctly populates it with `args.ControlPlaneNodes` and `args.WorkerNodes`.
- `clusterapi.ClusterInfo.ControlPlaneNodes` and `.WorkerNodes` — verified against `cluster.pb.go` (lines 82–83). Field names match exactly.

### Design decision review

**D1 (`preserve` defaults `true`)**: The `*bool` pointer pattern with `nil → true` is the established codebase convention (`resolveDryRun` at `helpers.go:116`). The `resolvePreserve` helper mirrors it exactly. Divergence from talosctl default is justified and documented in the jsonschema description — this is appropriate for AI-agent tooling.

**D2 (reboot `force` as mode value)**: Correct. The existing `opts []talosclient.RebootMode` slice accepts both `WithPowerCycle` and `WithForce` by the same append pattern. The mutually exclusive enum model matches the protobuf `RebootRequest.Mode`.

**D3 (`reboot_mode` naming on upgrade)**: Avoids ambiguity with reboot's `mode` param. Protobuf field is `upgrade_request.reboot_mode`. Naming is appropriate.

**D4 (rollback safety guards)**: Mirrors reboot/upgrade guard pattern. Justified — rollback causes a reboot and is irreversible if the previous installation is damaged.

**D5 (no client-side etcd restart validation)**: Correct engineering decision. API-level errors are more informative and future-proof.

### Scope completeness

All 7 files to be modified are identified and the rationale for each change is clear. The plan correctly defers high-risk items (`talos_reset`, `talos_etcd_snapshot`) with explicit reasons. The CLAUDE.md tool count increment (15 → 16) is accurate given one new tool (`talos_rollback`).

### Test strategy

The four proposed test cases cover the critical guard paths: `resolvePreserve` semantics (analogous to the existing `TestResolveDryRun`), unknown `reboot_mode` rejection, unknown reboot `mode` extension, and rollback guards. The existing test file uses `safeH()` + table-driven patterns; the plan follows these conventions. Coverage of the new validation paths is adequate.

### Minor observations (non-blocking)

1. The `HandleRollback` sketch in Step 5 does not explicitly show `ctx = talos.WithNodes(ctx, args.Nodes)`. The plan states "same pattern as reboot/upgrade," which implies this is intended. The implementer should ensure this line is present — without it, rollback would execute against the default context nodes rather than the explicitly specified `args.Nodes`.

2. The `TestHandleReboot_InvalidMode` test description says "mode: 'force' with no confirm still rejected." Because the confirm guard fires first (before the mode switch), the rejection reason will be the confirm guard, not the mode guard. This is functionally correct but the test comment/name may mislead a future reader. Consider naming the case `"force mode accepted when confirm missing"` or similar to make the guard ordering explicit.

Both observations are documentation/clarity matters that can be resolved during implementation without reopening the plan.

### Conclusion

The plan is complete, well-scoped, and architecturally sound. SDK symbol references are accurate. Design decisions are defensible and consistent with existing codebase patterns. Test strategy covers all new validation paths. The change is ready for implementation.
