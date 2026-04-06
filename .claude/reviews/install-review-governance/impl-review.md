---
change-id: install-review-governance
review-type: impl-review
reviewer-role: staff-reviewer
status: approved
timestamp: 2026-04-06T15:00:00Z
reviewed-scope:
  - .claude/hooks/require-review.sh
  - .claude/hooks/pre-commit
  - .claude/settings.json
  - .claude/agents/senior-implementer.md
  - .claude/agents/senior-plan-reviewer.md
  - .claude/agents/staff-reviewer.md
  - .claude/agents/principal-architect-reviewer.md
  - .claude/agents/researcher.md
  - .claude/reviews/TEMPLATE.md
  - CLAUDE.md (governance section, lines 143–191)
findings: []
---

## Notes

### Prior findings — all verified fixed

**Round 1 finding: mapfile -t (bash 4+ only)**
Fixed and still correct. Both hooks use the portable `while IFS= read -r d; do REVIEW_DIRS+=("$d"); done < <(find ...)` pattern (require-review.sh:85-86, pre-commit:27-28). Process substitution with a while-read loop is bash 3.2-compatible.

**Round 2 finding: negative array index ${REVIEW_DIRS[-1]}**
Fixed. Both hooks now use `${REVIEW_DIRS[${#REVIEW_DIRS[@]}-1]}` (require-review.sh:94, pre-commit:36). This arithmetic form works correctly on bash 3.2 (macOS system bash). The inline comment documents the rationale.

**Round 2 finding: deny() defined after ERR trap**
Fixed. The execution order in require-review.sh is now:
1. `INPUT=$(cat)` — stdin capture before any function calls (line 11)
2. `deny()` definition (lines 14-21)
3. ERR trap referencing deny() (line 24)
4. python3 preflight that calls deny() (lines 27-29)

deny() is defined before every call site, including the trap. The fail-closed contract is intact.

**Round 3 finding: python3 silently fails open**
Fixed. Lines 27-29 of require-review.sh add a `command -v python3` preflight that calls `deny()` with an actionable error message before any python3 invocation. The pre-commit hook (git hook) does not use python3 and is unaffected.

### Syntax and portability verification

Both scripts pass `bash -n` syntax check clean. Both have `#!/bin/bash` shebangs and `set -uo pipefail`. All array operations are bash 3.2-compatible.

### What passes (confirmed in this review round)

**require-review.sh**
- Fail-closed ERR trap fires correctly — deny() is defined above it.
- python3 preflight gates the two python3 invocations (COMMAND extraction and TOOL_NAME extraction); both have `2>/dev/null || echo ""` for command-level failures after the preflight confirms python3 exists.
- Commit detection regex `(^|[^a-z])git[[:space:]]+commit` with `-iE` correctly matches all realistic git commit invocations without false positives on strings like `xgit commit`.
- MCP tool matching covers `mcp__github__push_files` and `mcp__github__create_or_update_file` — consistent with settings.json matchers.
- `get_yaml_field()` awk/grep/sed/tr pipeline correctly extracts bare and quoted YAML values from frontmatter delimited by `---` lines. The `tr -d '[:space:]"'"'"` strip handles both single-quoted and double-quoted YAML scalar values.
- Role-separation check covers all three required artifacts; correctly catches `reviewer-role: senior-implementer`.
- Hook exits 0 (not non-zero) on deny — correct for Claude Code PreToolUse hooks (non-zero means hook error, not permission denial; the JSON payload carries the decision).

**pre-commit**
- Standard git hook contract: exit 0 allows, exit non-zero blocks. Correct.
- Mirrors require-review.sh logic for directory discovery, last-directory selection, get_yaml_field, status check, and role-separation check.
- Uses `echo` to stderr for user-facing messages — appropriate for a git hook.
- Does not use python3; no python3-related failure mode.

**settings.json**
- Three PreToolUse matchers: `Bash`, `mcp__github__push_files`, `mcp__github__create_or_update_file`.
- Command `bash .claude/hooks/require-review.sh` — relative path is correct; Claude Code runs hooks from the project root.
- JSON is well-formed.

**Agent definitions (all five)**
- All have valid YAML frontmatter with `temperature`, `description`, and `tools` fields.
- Reviewer agents (senior-plan-reviewer, staff-reviewer, principal-architect-reviewer, researcher): `write: false, edit: false`. Enforces read-only constraint.
- senior-implementer: `write: true, edit: true`. Self-review prohibition is explicit in both the Constraints section and the worked example.
- Output format, status rules, and severity calibration are consistent across all reviewer agents.
- researcher agent correctly specifies repo-first, official-docs fallback, with citation and confidence requirements.

**TEMPLATE.md**
- Schema exactly matches what get_yaml_field() parses: `change-id`, `review-type`, `reviewer-role`, `status`, `timestamp`, `reviewed-scope`, `findings`.
- Finding structure documented with all required fields (severity, description, location, fix).
- Severity guide is consistent with agent definitions.
- Usage instructions are accurate.

**CLAUDE.md governance section**
- Review flow (plan → implement → impl-review → final-approval) matches agent capabilities and hook enforcement.
- Change-id convention with commit message tag is clearly specified.
- Role separation rule matches principal-architect-reviewer gate check logic.
- Enforcement section accurately describes what the hooks cover and their limitations.
- Known limitations section is honest and appropriately scoped (no cryptographic signing, no post-review modification detection tracked for v2).

### Lexicographic sort assumption (pre-existing, not a finding)

Both hooks select the last lexicographically sorted review directory as the active change. This is documented and consistent with the semantic slug convention. Normal usage (one active change at a time) makes accidental override unlikely.

### No new findings

All four prior findings are resolved. No new issues identified in correctness, portability, security, or documentation accuracy. This change is approved.
