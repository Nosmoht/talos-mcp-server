---
name: refresh-tool-inventory
description: Extract the current tool/prompt/resource inventory from internal/{tools,prompts,resources} and update the inventory tables in AGENTS.md and README.md. Defaults to dry-run. Use when handlers are added, renamed, or removed.
allowed-tools: Read, Edit, Grep, Glob, Bash
argument-hint: "[--apply]"
---

# Refresh Tool Inventory

Regenerate the tool/prompt/resource tables in `AGENTS.md` and `README.md`
from the source of truth (the Go packages in `internal/`).

## Arguments

Parse `$ARGUMENTS`:

- `--apply` — write changes. Default is dry-run (print diff only).

## Key Steps

1. **Enumerate registered handlers.** Grep every registration call:
   ```bash
   grep -nE 'mcp\.(AddTool|AddPrompt|AddResource)' \
     internal/tools internal/prompts internal/resources -r
   ```
   Capture `kind` (tool/prompt/resource), `name`, `handler`, `file:line`.

2. **Extract description.** For each handler, Read the surrounding tool
   definition to pull `Description:` / `Title:` fields. If absent, record
   description as `<missing>` and flag in the report.

3. **Compute diff.** Locate the inventory tables in `AGENTS.md` and
   `README.md` using these sentinel markers (REQUIRED — abort if missing):

   ```
   <!-- inventory:tools:start -->  ...  <!-- inventory:tools:end -->
   <!-- inventory:prompts:start --> ...  <!-- inventory:prompts:end -->
   <!-- inventory:resources:start --> ...  <!-- inventory:resources:end -->
   ```

   If any sentinel is missing, abort with:
   `ERROR: sentinel <name> missing in <file> — add markers before re-running.`
   Never guess table boundaries.

4. **Emit preview.** Print the unified diff between the current tables and
   the regenerated tables.

5. **Apply (only if `--apply`).** Use Edit to replace the content between
   each matching sentinel pair. Leave everything outside the sentinels
   untouched.

## Output Format

```
Inventory diff (tools | prompts | resources):
  Added:    <name> (<kind>) — <file:line>
  Removed:  <name> (<kind>)
  Renamed:  <old> → <new>
  Changed:  <name> — description updated

Files that would be updated: AGENTS.md, README.md
Mode: dry-run | apply
```

On dry-run, print the diff and exit. On `--apply`, also write and print the
final file paths.

## Notes

Prefer promoting the extraction step to a deterministic shell or Go script
under `scripts/` once the format stabilises — this skill should then shrink
to "invoke the script, preview, apply" (per the project's deterministic
hierarchy rule).
