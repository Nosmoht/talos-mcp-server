# Maintenance-mode (insecure) invariants

Loaded automatically when editing `internal/talos/insecure.go`,
`internal/tools/meta.go`, or any handler that calls `dialInsecure` /
`canonicalizeAndCheckInsecure`.

## Transport posture

`NewInsecureClient` builds a TLS connection with `InsecureSkipVerify: true`. **It is NOT plain HTTP/2.** Fingerprint pinning (TOFU) is only meaningful over an active TLS session — switching to `insecure.NewCredentials()` from `google.golang.org/grpc/credentials/insecure` would silently break the optional `cert_fingerprint` defence.

## Endpoint canonicalisation

`talos.CanonicalIP` is the **single source of truth** for the endpoint string used in:
- `h.InsecureAllowedNodes.CheckNode(canonicalEndpoint)`
- `h.nodePatchMu("insecure:" + canonicalEndpoint)` (per-endpoint lock key)
- `talosclient.WithEndpoints(canonicalEndpoint)` (dial target)

If any future code path uses `args.Endpoint` raw instead, the IPv4-mapped-IPv6 lock-key bypass returns. Always pipe through `canonicalizeAndCheckInsecure` first.

Rejection classes (do NOT relax without reviewing the threat model):
- unspecified (`0.0.0.0`, `::`)
- loopback (`127.0.0.0/8`, `::1`)
- link-local unicast (`169.254.0.0/16` incl. cloud IMDS, `fe80::/10`)
- multicast (`224.0.0.0/4`, `ff00::/8`)
- IPv4 broadcast (`255.255.255.255`)

## Allowlist permissiveness ceiling

`config.CheckInsecureAllowlist` REFUSES at startup:
- empty / whitespace-only
- `0.0.0.0/0`, `::/0`
- IPv4 CIDR with mask `<16`
- IPv6 CIDR with mask `<48`

`slog.Warn` is emitted for IPv4 `<24` and IPv6 `<64` but still accepted. The rationale: maintenance-mode operations have no transport-layer authentication; the allowlist is the only network-layer perimeter. Operator ergonomics (a typical homelab `/24`) is preserved with a warning, but anything broader is a security floor.

## Audit pairing

Every insecure handler emits:
1. `h.auditLog(toolName, args, args.Nodes)` — FIRST statement (before any gate)
2. `defer func() { h.auditOutcome(toolName, outcome, finalErr) }()` — paired so refused-at-gate, dial-error, RPC-error, and success are all distinguishable in the audit channel.

Defenders join `(AUDIT, AUDIT_OUTCOME)` lines by `tool=` to reconstruct call outcomes. Removing either side defeats incident-response — keep both.

## Confirm semantics

`talos_apply_config insecure=true` follows the same dual-state pattern as the authenticated path: `confirm=true` is required only when `dry_run=false`. Talos honours `DryRun` in maintenance mode (verified against `cmd/talosctl/cmd/talos/apply-config.go`). Don't change to "confirm-always-required" — operators rely on the dry-run-first discipline.

## Blocklist bypass

`TALOS_MCP_BLOCKED_CONFIG_PATHS` is intentionally **bypassed** in the insecure path. The blocklist is a post-bootstrap control over targeted patches against a configured cluster; maintenance-mode `apply-config` necessarily replaces the entire config because the node has no config yet. The bypass is documented and explicit — do not silently re-enable the guard for `insecure=true`.

## META key safelist

`talos_meta` write/delete is gated by `reservedMetaKeys` (UserReserved1/2/3 always allowed) and `h.MetaPrivilegedKeys` (per-key enumeration via `TALOS_MCP_META_PRIVILEGED_KEYS`). The all-or-nothing boolean an early draft used was rejected: operators who need *one* privileged key would otherwise unlock the entire 0-255 range, exposing `StateEncryptionConfig`, `Upgrade`, `UUIDOverride`, etc. to prompt-injection.

## Fingerprint format

`ParseFingerprint` requires exactly 64 hex characters after stripping `:` and whitespace, decodes to 32 bytes, compares via `crypto/subtle.ConstantTimeCompare` against `sha256.Sum256(rawCerts[0])` (LEAF cert only, mirrors talosctl). Length, hex-only, and constant-time compare are all defence-in-depth — none of them is optional.
