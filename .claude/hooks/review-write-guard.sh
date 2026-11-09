#!/bin/bash
# Usage: review-write-guard.sh <main-checkout-root> <artifact-file-name>, hook JSON on stdin.
# Allow = stdout "review-write-guard: allow" + exit 0; every other outcome blocks (exit 2),
# because settings.json allows Write and this guard is the only confinement for reviewers.

set -uo pipefail

deny() {
  printf 'review-write-guard: %s — write denied.\n' "$1" >&2
  exit 2
}

trap 'deny "internal error in review-write-guard.sh"' ERR

[ $# -eq 2 ] || deny "needs <root> <artifact-file-name>"
command -v python3 >/dev/null 2>&1 || deny "python3 not found, cannot check the path"

# -I: isolated mode, so modules in the reviewed working tree cannot shadow json/os/re.
VERDICT=$(python3 -I -c '
import json, os, re, sys

MARKER = ".review-open"
root, artifact = sys.argv[1], sys.argv[2]
if not os.path.isabs(root) or os.path.normpath(root) != root:
    print("deny:guard root is not a normalized absolute path"); sys.exit(0)
if not re.fullmatch(r"(review|review-[a-z]+(-[a-z]+)*|plan-review)\.md", artifact):
    print("deny:guard was given an invalid artifact name"); sys.exit(0)

try:
    path = json.load(sys.stdin)["tool_input"]["file_path"]
except Exception:
    print("deny:hook input has no tool_input.file_path"); sys.exit(0)
if not isinstance(path, str) or not os.path.isabs(path) or os.path.normpath(path) != path:
    print("deny:file_path is not a normalized absolute path"); sys.exit(0)

reviews = os.path.join(root, ".claude", "reviews")
change_dir, name = os.path.split(path)
change_id = os.path.basename(change_dir)
if os.path.dirname(change_dir) != reviews or name != artifact:
    print("deny:this reviewer may write only " + reviews + "/<change-id>/" + artifact); sys.exit(0)
if not re.fullmatch(r"[a-z0-9][a-z0-9.-]*", change_id):
    print("deny:change-id must be lowercase letters, digits, dots and dashes"); sys.exit(0)
if not os.path.isdir(change_dir) or os.path.realpath(change_dir) != os.path.join(os.path.realpath(reviews), change_id):
    print("deny:" + change_dir + " must already exist as a real directory (the caller creates it)"); sys.exit(0)
if not os.path.isfile(os.path.join(change_dir, MARKER)):
    print("deny:" + change_dir + " has no " + MARKER + " marker, so no review of this change is open"); sys.exit(0)
if os.path.lexists(path) and (os.path.islink(path) or not os.path.isfile(path)):
    print("deny:" + path + " exists and is not a regular file"); sys.exit(0)
print("allow")
' "$1" "$2")

case "$VERDICT" in
  allow) echo "review-write-guard: allow" ;;
  deny:*) deny "${VERDICT#deny:}" ;;
  *) deny "no verdict" ;;
esac
