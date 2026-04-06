# Security Policy

## Supported Versions

Only the latest release receives security fixes. Older versions are not maintained.

## Reporting a Vulnerability

Please use [GitHub Private Vulnerability Reporting](https://github.com/Nosmoht/talos-mcp-server/security/advisories/new) to report security issues privately. Do not open a public issue.

**Response timeline:** Best effort. Expect an initial response within a week. This is an open-source project maintained by a single person in spare time — response speed is not guaranteed.

## Scope

In scope:
- Bugs in talos-mcp itself that allow bypassing safety guards (confirm gates, dry-run default, path allowlist, read-only mode)
- Vulnerabilities in the Go code that could allow privilege escalation or data exfiltration beyond what the configured talosconfig credentials permit
- Supply chain issues: compromised release artifacts or build pipeline

Out of scope:
- Vulnerabilities in the Talos Linux API, `talosctl`, or the Talos gRPC client library — report those to [Sidero Labs](https://github.com/siderolabs/talos/security)
- Vulnerabilities in the MCP client (Claude Code, Codex, etc.) — report to the respective project
- Behavior that requires a compromised talosconfig or full host access — the threat model assumes the host running talos-mcp is trusted

## Security Model Summary

See [Security Model](README.md#security-model) in the README for a full description of trust boundaries, RBAC role requirements, and safety mechanisms.
