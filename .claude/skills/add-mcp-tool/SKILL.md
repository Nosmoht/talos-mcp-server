---
name: add-mcp-tool
description: Scaffold a new Talos MCP tool handler with correct signature, args struct, safety-guard defaults, registration, inventory update, and a test stub. Use when adding a new talos_* tool to internal/tools/.
allowed-tools: Read, Write, Edit, Grep, Glob, Bash
argument-hint: <tool-name> [mutating|read]
---

# Add MCP Tool

Scaffold a new tool handler that conforms to the project's handler signature
(`func (h *Handlers) HandleXxx(ctx, req, args XxxArgs)`) and safety conventions.

## Arguments

Parse `$ARGUMENTS`:

- `$1` — snake_case tool name (required). Must start with `talos_` (e.g. `talos_drain_node`).
- `$2` — category, one of `mutating` | `read` (default `read`).

Derive `{{ToolName}}` = PascalCase of `$1` with the `talos_` prefix stripped
(e.g. `talos_drain_node` → `DrainNode`).

If any required argument is missing, ask the user once for the missing value,
then proceed non-interactively.

## Key Steps

1. **Validate name.** Reject if `$1` does not match `^talos_[a-z][a-z0-9_]*$`.
2. **Collision check.** Use Glob to confirm `internal/tools/<name>.go` does not
   exist. If it does, stop and report — do not overwrite.
3. **Generate handler.** Render `references/handler-template.go.tmpl` with
   `{{ToolName}}`, `{{tool_name}}`, `{{category}}` substituted. Write to
   `internal/tools/<name>.go`.
4. **Apply safety defaults.** For `mutating`, inline `confirm bool` and
   `nodes []string` on `{{ToolName}}Args`; set `dry_run` / `preserve` /
   `graceful` defaults from `references/safety-defaults.md`.
5. **Register the tool.** Grep for `mcp.AddTool` in `internal/tools/` to
   locate the registration site. Add the new entry in alphabetical order:

   ```go
   mcp.AddTool(server, &mcp.Tool{Name: "{{tool_name}}"}, h.Handle{{ToolName}})
   ```

6. **Generate test stub.** Write `internal/tools/<name>_test.go` with one
   happy-path test and (for `mutating`) a guards-enforcement test.
7. **Format.** Run `gofmt -w internal/tools/<name>.go internal/tools/<name>_test.go`.
8. **Update inventory.** Edit the tool-inventory tables in `AGENTS.md` and
   `README.md`. Preserve alphabetical order.

## Output

On success, print:

```
Created:   internal/tools/<name>.go
           internal/tools/<name>_test.go
Modified:  <registration-file>
           AGENTS.md  (tool inventory)
           README.md  (tool inventory)
Next:      make check
           invoke staff-reviewer  →  .claude/reviews/<change-id>/review.md
           commit  →  feat(tools): add <tool_name> handler [review:<change-id>]
```

On abort (name invalid, collision, template missing): print the reason and
exit without writes.

## References

- `references/handler-template.go.tmpl` — handler + args struct template patterned on `internal/tools/lifecycle.go`.
- `references/safety-defaults.md` — per-category default values (mirror of repo-root `CLAUDE.md` § Safety).
