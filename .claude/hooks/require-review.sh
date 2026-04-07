#!/bin/bash
# PreToolUse hook: require review artifacts before commits
# Tiered validation: staff-reviewer primary artifact + on-demand escalation artifacts
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

# Preflight: python3 required for JSON/YAML parsing
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
IS_COMMIT=false

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

# Resolve main repo root — works from both main repo and git worktrees.
# git rev-parse --git-common-dir returns ".git" (relative) in the main worktree
# and an absolute path in linked worktrees.
_GIT_COMMON=$(git rev-parse --git-common-dir 2>/dev/null || echo ".git")
if [[ "$_GIT_COMMON" = /* ]]; then
  _MAIN_REPO=$(cd "$_GIT_COMMON/.." && pwd)
else
  _MAIN_REPO=$(pwd)
fi
REVIEW_BASE="$_MAIN_REPO/.claude/reviews"

if [ ! -d "$REVIEW_BASE" ]; then
  deny "BLOCKED: No .claude/reviews/ directory found. Create review artifacts before committing. See CLAUDE.md governance section."
fi

# Find review directories (exclude hidden files), sorted lexicographically
REVIEW_DIRS=()
while IFS= read -r d; do REVIEW_DIRS+=("$d"); done \
  < <(find "$REVIEW_BASE" -mindepth 1 -maxdepth 1 -type d 2>/dev/null | sort)

if [ "${#REVIEW_DIRS[@]}" -eq 0 ]; then
  deny "BLOCKED: No review directories in .claude/reviews/. Invoke staff-reviewer first."
fi

# Use the last directory (lexicographically most recent by slug)
REVIEW_DIR="${REVIEW_DIRS[${#REVIEW_DIRS[@]}-1]}"
CHANGE_ID=$(basename "$REVIEW_DIR")

# Helper: extract a YAML frontmatter scalar field value
# Usage: get_yaml_field <file> <field>
get_yaml_field() {
  local file="$1"
  local field="$2"
  awk '/^---$/{c++;next} c==1{print}' "$file" 2>/dev/null \
    | (grep -E "^${field}:" || true) \
    | head -1 \
    | sed "s/^${field}:[[:space:]]*//" \
    | tr -d '[:space:]"'"'"
}

# Helper: extract YAML list items from frontmatter
# Handles inline: field: [a, b] and multiline: field:\n  - a\n  - b
# Prints one item per line; empty output = empty list
get_yaml_list() {
  local file="$1"
  local field="$2"
  python3 - "$file" "$field" <<'PYEOF'
import sys, re

file_path = sys.argv[1]
field = sys.argv[2]

try:
    with open(file_path) as f:
        content = f.read()
except Exception:
    sys.exit(0)

# Extract YAML frontmatter (between first and second ---)
parts = content.split('---')
if len(parts) < 3:
    sys.exit(0)

fm = parts[1]

# Try inline list: field: [a, b, c] or field: []
inline = re.search(rf'^{re.escape(field)}:\s*\[([^\]]*)\]', fm, re.MULTILINE)
if inline:
    raw = inline.group(1).strip()
    if raw:
        for item in raw.split(','):
            item = item.strip().strip('"\'')
            # Strip leading 'type:' prefix (LLM sometimes writes '- type: value')
            item = re.sub(r'^type:\s*', '', item)
            if item:
                print(item)
    sys.exit(0)

# Try multiline list:
# field:
#   - item1
#   - item2
ml = re.search(rf'^{re.escape(field)}:\s*\n((?:[ \t]+-[ \t]+\S.*\n?)+)', fm, re.MULTILINE)
if ml:
    for line in ml.group(1).splitlines():
        m = re.match(r'[ \t]+-[ \t]+(\S.*)', line)
        if m:
            item = m.group(1).strip().strip('"\'')
            # Strip leading 'type:' prefix (LLM sometimes writes '- type: value')
            item = re.sub(r'^type:\s*', '', item)
            if item:
                print(item)
PYEOF
}

# ── Validate primary review.md ───────────────────────────────────────────────

PRIMARY="$REVIEW_DIR/review.md"

if [ ! -f "$PRIMARY" ]; then
  deny "BLOCKED: Missing review.md for change '${CHANGE_ID}'. Invoke staff-reviewer to produce it."
fi

PRIMARY_STATUS=$(get_yaml_field "$PRIMARY" "status")

if [ -z "$PRIMARY_STATUS" ]; then
  deny "BLOCKED: review.md for '${CHANGE_ID}' has no parseable 'status' field. Check YAML frontmatter."
fi

case "$PRIMARY_STATUS" in
  approved)
    # Direct approval — proceed to role separation check below
    ;;

  escalate)
    # Escalation required — validate each referenced escalation artifact
    ESCALATIONS=$(get_yaml_list "$PRIMARY" "escalations")

    if [ -z "$ESCALATIONS" ]; then
      deny "BLOCKED: review.md for '${CHANGE_ID}' has status 'escalate' but empty escalations list. Add escalation types or set status to 'approved'."
    fi

    while IFS= read -r esc_type; do
      [ -z "$esc_type" ] && continue

      ESC_FILE="$REVIEW_DIR/review-${esc_type}.md"

      if [ ! -f "$ESC_FILE" ]; then
        deny "BLOCKED: Escalation artifact 'review-${esc_type}.md' missing for change '${CHANGE_ID}'. Invoke the ${esc_type} reviewer."
      fi

      ESC_STATUS=$(get_yaml_field "$ESC_FILE" "status")

      if [ -z "$ESC_STATUS" ]; then
        deny "BLOCKED: review-${esc_type}.md for '${CHANGE_ID}' has no parseable 'status' field."
      fi

      case "$ESC_STATUS" in
        approved)
          # Escalation approved — continue
          ;;

        escalate)
          # One level of chained escalation allowed
          NESTED_ESCS=$(get_yaml_list "$ESC_FILE" "escalations")

          if [ -z "$NESTED_ESCS" ]; then
            deny "BLOCKED: review-${esc_type}.md for '${CHANGE_ID}' has status 'escalate' but empty escalations list."
          fi

          while IFS= read -r nested_type; do
            [ -z "$nested_type" ] && continue

            NESTED_FILE="$REVIEW_DIR/review-${nested_type}.md"

            if [ ! -f "$NESTED_FILE" ]; then
              deny "BLOCKED: Nested escalation artifact 'review-${nested_type}.md' missing for change '${CHANGE_ID}'."
            fi

            NESTED_STATUS=$(get_yaml_field "$NESTED_FILE" "status")

            if [ -z "$NESTED_STATUS" ]; then
              deny "BLOCKED: review-${nested_type}.md for '${CHANGE_ID}' has no parseable 'status' field."
            fi

            if [ "$NESTED_STATUS" != "approved" ]; then
              deny "BLOCKED: review-${nested_type}.md for '${CHANGE_ID}' has status '${NESTED_STATUS}', not 'approved'."
            fi
          done <<< "$NESTED_ESCS"
          ;;

        changes-requested)
          deny "BLOCKED: review-${esc_type}.md for '${CHANGE_ID}' has status 'changes-requested'. Resolve findings before committing."
          ;;

        *)
          deny "BLOCKED: review-${esc_type}.md for '${CHANGE_ID}' has unknown status '${ESC_STATUS}'."
          ;;
      esac
    done <<< "$ESCALATIONS"
    ;;

  changes-requested)
    deny "BLOCKED: review.md for '${CHANGE_ID}' has status 'changes-requested'. Resolve all findings before committing."
    ;;

  *)
    deny "BLOCKED: review.md for '${CHANGE_ID}' has unknown status '${PRIMARY_STATUS}'. Valid values: approved, escalate, changes-requested."
    ;;
esac

# ── Role separation check ────────────────────────────────────────────────────
# No artifact in the review directory may be authored by senior-implementer

for review_file in "$REVIEW_DIR"/*.md; do
  [ -f "$review_file" ] || continue
  ROLE=$(get_yaml_field "$review_file" "reviewer-role")
  if [ "$ROLE" = "senior-implementer" ]; then
    artifact=$(basename "$review_file")
    deny "BLOCKED: ${artifact} was produced by 'senior-implementer'. Role separation violated — implementer cannot self-review."
  fi
done

# All checks passed — allow the commit
exit 0
