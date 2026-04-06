#!/bin/bash
# PreToolUse hook: require review artifacts before commits
#
# Triggered by .claude/settings.json for Bash commands and MCP GitHub push tools.
# Fail-closed: any unexpected error causes a deny response.
#
# Usage: called automatically by Claude Code — receives JSON on stdin.

set -uo pipefail

INPUT=$(cat)

# ── Helper: deny a commit with a reason ─────────────────────────────────────
deny() {
  local reason="$1"
  # Escape backslashes then double quotes for JSON safety
  reason="${reason//\\/\\\\}"
  reason="${reason//\"/\\\"}"
  printf '{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"deny","permissionDecisionReason":"%s"}}\n' "$reason"
  exit 0
}

# Trap unexpected errors and fail closed (deny() is now defined above the trap)
trap 'deny "BLOCKED: Internal hook error — cannot validate review artifacts. Fix .claude/hooks/require-review.sh before committing."' ERR

# Preflight: python3 required for JSON parsing
if ! command -v python3 &>/dev/null; then
  deny "BLOCKED: python3 not found — cannot parse hook input. Install python3 to enable review enforcement."
fi

# Extract the bash command (for Bash tool calls)
COMMAND=$(echo "$INPUT" | python3 -c "
import sys, json
try:
    data = json.load(sys.stdin)
    print(data.get('tool_input', {}).get('command', ''))
except Exception:
    print('')
" 2>/dev/null || echo "")

# Determine if this is a commit-capable action.
# Check both the tool name (for MCP tools) and the bash command.
IS_COMMIT=false

# MCP GitHub tools that push changes commit directly — always require review.
# The tool name is available via the hook JSON payload's tool_name field.
TOOL_NAME=$(echo "$INPUT" | python3 -c "
import sys, json
try:
    data = json.load(sys.stdin)
    print(data.get('tool_name', ''))
except Exception:
    print('')
" 2>/dev/null || echo "")

case "$TOOL_NAME" in
  mcp__github__push_files|mcp__github__create_or_update_file)
    IS_COMMIT=true ;;
esac

# For Bash tool: check if the command contains a git commit invocation.
# Matches: git commit, /usr/bin/git commit, $(git commit), chained with ; or &&
if [ "$IS_COMMIT" = false ]; then
  if echo "$COMMAND" | grep -qiE '(^|[^a-z])git[[:space:]]+commit|/git[[:space:]]+commit'; then
    IS_COMMIT=true
  fi
fi

# Not a commit action — allow without checking artifacts
if [ "$IS_COMMIT" = false ]; then
  exit 0
fi

# ── Commit detected: validate review artifacts ──────────────────────────────

REVIEW_BASE=".claude/reviews"

if [ ! -d "$REVIEW_BASE" ]; then
  deny "BLOCKED: No .claude/reviews/ directory found. Create review artifacts before committing. See CLAUDE.md governance section."
fi

# Find review directories (exclude TEMPLATE.md and hidden files)
# Portable: bash 3.2+ compatible (no mapfile, no negative array indices)
REVIEW_DIRS=()
while IFS= read -r d; do REVIEW_DIRS+=("$d"); done \
  < <(find "$REVIEW_BASE" -mindepth 1 -maxdepth 1 -type d 2>/dev/null | sort)

if [ "${#REVIEW_DIRS[@]}" -eq 0 ]; then
  deny "BLOCKED: No review directories in .claude/reviews/. Invoke senior-plan-reviewer, staff-reviewer, and principal-architect-reviewer first."
fi

# Use the last directory (lexicographically most recent by slug)
# Portable: ${REVIEW_DIRS[${#REVIEW_DIRS[@]}-1]} works on bash 3.2+ (no negative index)
REVIEW_DIR="${REVIEW_DIRS[${#REVIEW_DIRS[@]}-1]}"
CHANGE_ID=$(basename "$REVIEW_DIR")

# Helper: extract a YAML frontmatter field value
# Usage: get_yaml_field <file> <field>
get_yaml_field() {
  local file="$1"
  local field="$2"
  # Extract content between the first and second --- markers, then grep for field.
  # grep returns exit 1 on no-match; || true prevents pipefail from triggering ERR.
  awk '/^---$/{c++;next} c==1{print}' "$file" 2>/dev/null \
    | (grep -E "^${field}:" || true) \
    | head -1 \
    | sed "s/^${field}:[[:space:]]*//" \
    | tr -d '[:space:]"'"'"
}

# Check required artifacts exist and are approved
for artifact in plan-review.md impl-review.md final-approval.md; do
  FILE="$REVIEW_DIR/$artifact"

  if [ ! -f "$FILE" ]; then
    deny "BLOCKED: Missing ${artifact} for change '${CHANGE_ID}'. Invoke the appropriate reviewer agent to produce it."
  fi

  STATUS=$(get_yaml_field "$FILE" "status")

  if [ -z "$STATUS" ]; then
    deny "BLOCKED: ${artifact} for '${CHANGE_ID}' has no parseable 'status' field. Check YAML frontmatter formatting."
  fi

  if [ "$STATUS" != "approved" ]; then
    deny "BLOCKED: ${artifact} for '${CHANGE_ID}' has status '${STATUS}', not 'approved'. Resolve all findings before committing."
  fi
done

# Verify role separation: no artifact should have reviewer_role: senior-implementer
# Field name uses underscore (reviewer_role) matching artifact frontmatter convention.
for artifact in plan-review.md impl-review.md final-approval.md; do
  FILE="$REVIEW_DIR/$artifact"
  ROLE=$(get_yaml_field "$FILE" "reviewer_role")

  if [ "$ROLE" = "senior-implementer" ]; then
    deny "BLOCKED: ${artifact} was produced by 'senior-implementer'. Role separation violated — implementer cannot self-review."
  fi
done

# All checks passed — allow the commit
exit 0
