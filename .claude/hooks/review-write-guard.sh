#!/bin/bash
# Hook JSON on stdin. `<main-checkout-root> <artifact>` (frontmatter hooks): allow = stdout "review-write-guard: allow".
# `--agent` (settings.json; runs where untrusted folders skip frontmatter hooks): exit 0 for agents whose ../agents
# frontmatter hook does not call this guard, else the same checks. Deny = exit 2; settings.json allows Write.

set -uo pipefail

if [ "${1:-}" = "--agent" ]; then
  MODE=agent
  PREFIX="review-write-guard (settings hook)"
else
  MODE=positional
  PREFIX="review-write-guard"
fi

deny() {
  printf '%s: %s — write denied.\n' "$PREFIX" "$1" >&2
  exit 2
}

trap 'deny "internal error in review-write-guard.sh"' ERR

if [ "$MODE" = agent ]; then
  [ $# -eq 1 ] || deny "--agent takes no further arguments"
else
  [ $# -eq 2 ] || deny "needs <root> <artifact-file-name>"
fi
command -v python3 >/dev/null 2>&1 || deny "python3 not found, cannot check the path"

AGENTS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/../agents"

# -I: isolated mode, so modules in the reviewed working tree cannot shadow json/os/re.
VERDICT=$(python3 -I -c '
import glob, json, os, re, subprocess, sys

MARKER = ".review-open"
ARTIFACT_RE = r"(review|review-[a-z]+(-[a-z]+)*|plan-review)\.md"

def check(root, artifact, data):
    if not os.path.isabs(root) or os.path.normpath(root) != root:
        return "deny:guard root is not a normalized absolute path"
    if not re.fullmatch(ARTIFACT_RE, artifact):
        return "deny:guard was given an invalid artifact name"
    try:
        path = data["tool_input"]["file_path"]
    except Exception:
        return "deny:hook input has no tool_input.file_path"
    if not isinstance(path, str) or not os.path.isabs(path) or os.path.normpath(path) != path:
        return "deny:file_path is not a normalized absolute path"
    reviews = os.path.join(root, ".claude", "reviews")
    change_dir, name = os.path.split(path)
    change_id = os.path.basename(change_dir)
    if os.path.dirname(change_dir) != reviews or name != artifact:
        return "deny:this reviewer may write only " + reviews + "/<change-id>/" + artifact
    if not re.fullmatch(r"[a-z0-9][a-z0-9.-]*", change_id):
        return "deny:change-id must be lowercase letters, digits, dots and dashes"
    if not os.path.isdir(change_dir) or os.path.realpath(change_dir) != os.path.join(os.path.realpath(reviews), change_id):
        return "deny:" + change_dir + " must already exist as a real directory (the caller creates it)"
    if not os.path.isfile(os.path.join(change_dir, MARKER)):
        return "deny:" + change_dir + " has no " + MARKER + " marker, so no review of this change is open"
    if os.path.lexists(path) and (os.path.islink(path) or not os.path.isfile(path)):
        return "deny:" + path + " exists and is not a regular file"
    return "allow"

def reviewer_artifact(agents_dir, agent_type):
    for f in sorted(glob.glob(os.path.join(agents_dir, "*.md"))):
        text = open(f, encoding="utf-8").read()
        m = re.match(r"---\n(.*?)\n---\n", text, re.S)
        if not m:
            continue
        front = m.group(1)
        name = re.search(r"^name:[ \t]*(\S+)[ \t]*$", front, re.M)
        if name is None or name.group(1) != agent_type or "review-write-guard.sh" not in front:
            continue
        a = re.search(r"bash \"\$g\" \"\$m\" ([^\s\")]+)", front)
        return a.group(1) if a else None
    return ""

def main_checkout(cwd):
    env = {k: v for k, v in os.environ.items() if k not in ("GIT_DIR", "GIT_COMMON_DIR", "GIT_WORK_TREE", "BASH_ENV", "ENV")}
    r = subprocess.run(["git", "rev-parse", "--path-format=absolute", "--git-common-dir"],
                       cwd=cwd, env=env, capture_output=True, text=True)
    common = r.stdout.strip()
    if r.returncode != 0 or not common.endswith("/.git"):
        return None
    return common[: -len("/.git")]

try:
    data = json.load(sys.stdin)
except Exception:
    print("deny:hook input is not valid JSON"); sys.exit(0)
if not isinstance(data, dict):
    print("deny:hook input is not a JSON object"); sys.exit(0)

if sys.argv[1] != "--agent":
    print(check(sys.argv[1], sys.argv[2], data)); sys.exit(0)

agent_type = data.get("agent_type")
if not agent_type:
    print("skip"); sys.exit(0)
if not isinstance(agent_type, str):
    print("deny:agent_type is not a string"); sys.exit(0)
artifact = reviewer_artifact(sys.argv[2], agent_type)
if artifact == "":
    print("skip"); sys.exit(0)
if artifact is None:
    print("deny:the guard hook of agent " + agent_type + " names no artifact"); sys.exit(0)
cwd = data.get("cwd")
if not isinstance(cwd, str) or not os.path.isdir(cwd):
    cwd = os.getcwd()
root = main_checkout(cwd)
if root is None:
    print("deny:main checkout not found from " + cwd); sys.exit(0)
print(check(root, artifact, data))
' "$1" "${2:-$AGENTS_DIR}")

case "$VERDICT" in
  allow)
    [ "$MODE" = agent ] || echo "review-write-guard: allow"
    ;;
  skip)
    [ "$MODE" = agent ] || deny "no verdict"
    ;;
  deny:*) deny "${VERDICT#deny:}" ;;
  *) deny "no verdict" ;;
esac
