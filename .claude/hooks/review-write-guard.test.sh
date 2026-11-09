#!/bin/bash
# Exercises review-write-guard.sh and the settings.json hook entry in a scratch repo; exit 0 = all cases pass.
set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$HERE/../.." && pwd)"
T="$(mktemp -d)"
trap 'rm -rf "$T"' EXIT
export GIT_AUTHOR_NAME=test GIT_AUTHOR_EMAIL=test@example.invalid
export GIT_COMMITTER_NAME=test GIT_COMMITTER_EMAIL=test@example.invalid
unset GIT_DIR GIT_COMMON_DIR GIT_WORK_TREE

R="$T/repo"
mkdir -p "$R"
R="$(cd "$R" && pwd -P)"
git -C "$R" init -q
mkdir -p "$R/.claude/hooks" "$R/.claude/agents" "$R/.claude/reviews/c1" "$R/.claude/reviews/c2" "$R/src"
cp "$HERE/review-write-guard.sh" "$R/.claude/hooks/"
cp "$REPO_ROOT/.claude/settings.json" "$R/.claude/"
cat > "$R/.claude/agents/lens-reviewer.md" <<'EOF'
---
name: lens-reviewer
tools: Read, Write
hooks:
  PreToolUse:
    - matcher: "Write|Edit"
      hooks:
        - type: command
          command: >-
            m="$(git rev-parse --path-format=absolute --git-common-dir)"; g="$m/.claude/hooks/review-write-guard.sh";
            v="$(bash "$g" "$m" review-lens.md)"
---
Fixture reviewer.
EOF
cat > "$R/.claude/agents/broken-reviewer.md" <<'EOF'
---
name: broken-reviewer
hooks:
  PreToolUse:
    - matcher: "Write"
      hooks:
        - type: command
          command: bash .claude/hooks/review-write-guard.sh
---
Fixture reviewer whose hook names no artifact.
EOF
cat > "$R/.claude/agents/helper.md" <<'EOF'
---
name: helper
tools: Read, Write
---
Fixture non-reviewer.
EOF
git -C "$R" add -A
git -C "$R" commit -qm init
git -C "$R" worktree add -q "$R/.claude/worktrees/w" -b w
W="$R/.claude/worktrees/w"
touch "$R/.claude/reviews/c1/.review-open"
ln -s c1 "$R/.claude/reviews/c3"

REV="$R/.claude/reviews"
pass=0
fail=0

# expect <case> <want-exit> <want-stderr-substring or -> <cwd> <json> <guard args...>
expect() {
  local name="$1" want="$2" msg="$3" cwd="$4" json="$5"
  shift 5
  local out err code
  out="$(cd "$cwd" && printf '%s' "$json" | bash "$@" 2>"$T/err")"
  code=$?
  err="$(cat "$T/err")"
  if [ "$code" -eq "$want" ] && { [ "$msg" = - ] || [[ "$err$out" == *"$msg"* ]]; }; then
    pass=$((pass + 1))
  else
    fail=$((fail + 1))
    printf 'FAIL %s: exit %s (want %s), output: %s%s\n' "$name" "$code" "$want" "$out" "$err"
  fi
}

write_json() { printf '{"agent_type":"%s","cwd":"%s","tool_name":"Write","tool_input":{"file_path":"%s","content":"x"}}' "$1" "$2" "$3"; }

G="$R/.claude/hooks/review-write-guard.sh"
GW="$W/.claude/hooks/review-write-guard.sh"
POS='{"tool_input":{"file_path":"%s"}}'

# Positional mode (frontmatter hooks).
expect pos-own-artifact 0 "review-write-guard: allow" "$R" "$(printf "$POS" "$REV/c1/review-lens.md")" "$G" "$R" review-lens.md
expect pos-other-artifact 2 "may write only" "$R" "$(printf "$POS" "$REV/c1/review.md")" "$G" "$R" review-lens.md
expect pos-no-marker 2 "marker" "$R" "$(printf "$POS" "$REV/c2/review-lens.md")" "$G" "$R" review-lens.md
expect pos-traversal 2 "normalized" "$R" "$(printf "$POS" "$REV/c1/../c1/review-lens.md")" "$G" "$R" review-lens.md
expect pos-symlinked-dir 2 "real directory" "$R" "$(printf "$POS" "$REV/c3/review-lens.md")" "$G" "$R" review-lens.md
expect pos-source-file 2 "may write only" "$R" "$(printf "$POS" "$R/src/x.go")" "$G" "$R" review-lens.md

# Hook mode (settings.json).
expect agent-main-session 0 - "$R" "$(printf "$POS" "$R/src/x.go")" "$G" --agent
expect agent-non-reviewer 0 - "$R" "$(write_json helper "$R" "$R/src/x.go")" "$G" --agent
expect agent-unknown 0 - "$R" "$(write_json other-reviewer "$R" "$R/src/x.go")" "$G" --agent
expect agent-own-artifact 0 - "$R" "$(write_json lens-reviewer "$R" "$REV/c1/review-lens.md")" "$G" --agent
expect agent-own-artifact-from-worktree 0 - "$W" "$(write_json lens-reviewer "$W" "$REV/c1/review-lens.md")" "$GW" --agent
expect agent-source-file 2 "(settings hook): this reviewer may write only" "$R" "$(write_json lens-reviewer "$R" "$R/src/x.go")" "$G" --agent
expect agent-worktree-file 2 "may write only" "$W" "$(write_json lens-reviewer "$W" "$W/.claude/reviews/c1/review-lens.md")" "$GW" --agent
expect agent-other-artifact 2 "may write only" "$R" "$(write_json lens-reviewer "$R" "$REV/c1/review.md")" "$G" --agent
expect agent-no-marker 2 "marker" "$R" "$(write_json lens-reviewer "$R" "$REV/c2/review-lens.md")" "$G" --agent
expect agent-notebook 2 "file_path" "$R" "$(printf '{"agent_type":"lens-reviewer","tool_input":{"notebook_path":"%s"}}' "$REV/c1/review-lens.md")" "$G" --agent
expect agent-hook-without-artifact 2 "names no artifact" "$R" "$(write_json broken-reviewer "$R" "$REV/c1/review.md")" "$G" --agent
expect agent-content-mentions-reviewer 0 - "$R" "$(printf '{"agent_type":"helper","tool_input":{"file_path":"%s","content":"\\"agent_type\\": \\"x-reviewer\\""}}' "$R/src/x.md")" "$G" --agent
expect agent-non-string-type 2 "not a string" "$R" '{"agent_type":1,"tool_input":{"file_path":"/x"}}' "$G" --agent
expect agent-bad-json 2 "not valid JSON" "$R" 'not json' "$G" --agent
expect agent-extra-arg 2 "no further arguments" "$R" '{}' "$G" --agent x

# The settings.json entry as Claude Code runs it.
SETTINGS_CMD="$(jq -r '.hooks.PreToolUse[] | select(.matcher | test("Write")) | .hooks[0].command' "$R/.claude/settings.json")"
printf '%s\n' "$SETTINGS_CMD" > "$T/settings-hook.sh"
export CLAUDE_PROJECT_DIR="$R"
expect settings-main-session 0 - "$R" "$(printf "$POS" "$R/src/x.go")" "$T/settings-hook.sh"
expect settings-reviewer-source 2 "(settings hook)" "$R" "$(write_json lens-reviewer "$R" "$R/src/x.go")" "$T/settings-hook.sh"
expect settings-reviewer-own 0 - "$R" "$(write_json lens-reviewer "$R" "$REV/c1/review-lens.md")" "$T/settings-hook.sh"
mkdir -p "$T/noguard/.claude"
CLAUDE_PROJECT_DIR="$T/noguard" expect settings-guard-missing 2 - "$R" "$(write_json lens-reviewer "$R" "$R/src/x.go")" "$T/settings-hook.sh"

# Every reviewer agent in this repository has a guard hook that names a valid artifact.
guarded=0
for f in "$REPO_ROOT"/.claude/agents/*.md; do
  name="$(sed -n 's/^name:[[:space:]]*//p' "$f" | head -1)"
  if ! grep -q 'review-write-guard.sh' "$f"; then
    case "$name" in *-reviewer)
      fail=$((fail + 1))
      echo "FAIL repo-agent-$name: reviewer without review-write-guard.sh hook" ;;
    esac
    continue
  fi
  guarded=$((guarded + 1))
  expect "repo-agent-$name" 2 "may write only" "$R" "$(write_json "$name" "$R" "$R/src/x.go")" \
    "$HERE/review-write-guard.sh" --agent
done
[ "$guarded" -gt 0 ] || { fail=$((fail + 1)); echo "FAIL repo-agents: no guarded agent found"; }

echo "pass=$pass fail=$fail"
[ "$fail" -eq 0 ]
