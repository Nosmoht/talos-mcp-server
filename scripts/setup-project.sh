#!/usr/bin/env bash
set -euo pipefail

# Bootstrap labels and Projects v2 board for the talos-mcp repo.
# Run once per repo (idempotent — uses --force on label create).

OWNER="${OWNER:-Nosmoht}"
REPO="${REPO:-talos-mcp-server}"

echo "==> Creating labels for ${OWNER}/${REPO}"

# ---------------------------------------------------------------------------
# Status labels (mutually exclusive)
# ---------------------------------------------------------------------------
gh label create "status: ready"          --color "0E8A16" --description "Triaged and ready for an agent to claim"            --repo "${OWNER}/${REPO}" --force
gh label create "status: assigned"       --color "1D76DB" --description "Claimed by an agent, not yet started"               --repo "${OWNER}/${REPO}" --force
gh label create "status: in-progress"    --color "FBCA04" --description "Active work underway"                               --repo "${OWNER}/${REPO}" --force
gh label create "status: review-pending" --color "D93F0B" --description "PR open, awaiting review"                          --repo "${OWNER}/${REPO}" --force
gh label create "status: blocked"        --color "B60205" --description "Blocked on dependency or question"                  --repo "${OWNER}/${REPO}" --force

# ---------------------------------------------------------------------------
# Priority labels (mutually exclusive)
# ---------------------------------------------------------------------------
gh label create "priority: P0" --color "B60205" --description "Critical - drop everything" --repo "${OWNER}/${REPO}" --force
gh label create "priority: P1" --color "D93F0B" --description "High - next up"             --repo "${OWNER}/${REPO}" --force
gh label create "priority: P2" --color "FBCA04" --description "Medium - scheduled"         --repo "${OWNER}/${REPO}" --force
gh label create "priority: P3" --color "C2E0C6" --description "Low - backlog"              --repo "${OWNER}/${REPO}" --force

# ---------------------------------------------------------------------------
# Area labels (multiple allowed)
# ---------------------------------------------------------------------------
gh label create "area: tools"      --color "5319E7" --description "internal/tools/ - MCP tool handlers"            --repo "${OWNER}/${REPO}" --force
gh label create "area: resources"  --color "5319E7" --description "internal/resources/ - MCP resources"            --repo "${OWNER}/${REPO}" --force
gh label create "area: prompts"    --color "5319E7" --description "internal/prompts/ - MCP prompts"                --repo "${OWNER}/${REPO}" --force
gh label create "area: transport"  --color "5319E7" --description "cmd/talos-mcp/ - stdio/HTTP transport"          --repo "${OWNER}/${REPO}" --force
gh label create "area: client"     --color "5319E7" --description "internal/talos/ - Talos gRPC client"            --repo "${OWNER}/${REPO}" --force
gh label create "area: version"    --color "5319E7" --description "internal/version/ - version compatibility"      --repo "${OWNER}/${REPO}" --force
gh label create "area: ci"         --color "5319E7" --description ".github/workflows/ - CI/CD"                     --repo "${OWNER}/${REPO}" --force
gh label create "area: npm"        --color "5319E7" --description "npm/ - npm distribution packages"               --repo "${OWNER}/${REPO}" --force
gh label create "area: docs"       --color "5319E7" --description "README, CLAUDE.md, CONTRIBUTING"                --repo "${OWNER}/${REPO}" --force
gh label create "area: governance" --color "5319E7" --description ".claude/agents/, hooks, review system"          --repo "${OWNER}/${REPO}" --force

# ---------------------------------------------------------------------------
# Size labels (mutually exclusive)
# ---------------------------------------------------------------------------
gh label create "size: XS" --color "EDEDED" --description "Trivial - under 30 min, single file"    --repo "${OWNER}/${REPO}" --force
gh label create "size: S"  --color "EDEDED" --description "Small - 1-2 hours, few files"           --repo "${OWNER}/${REPO}" --force
gh label create "size: M"  --color "EDEDED" --description "Medium - half day, multi-file"          --repo "${OWNER}/${REPO}" --force
gh label create "size: L"  --color "EDEDED" --description "Large - full day, cross-package"        --repo "${OWNER}/${REPO}" --force
gh label create "size: XL" --color "EDEDED" --description "Extra large - multi-day, architectural" --repo "${OWNER}/${REPO}" --force

# ---------------------------------------------------------------------------
# Coordination labels
# ---------------------------------------------------------------------------
gh label create "agent: claimed"       --color "006B75" --description "An agent has claimed this issue"    --repo "${OWNER}/${REPO}" --force
gh label create "needs: decomposition" --color "D4C5F9" --description "Too large, needs sub-issues"       --repo "${OWNER}/${REPO}" --force
gh label create "needs: clarification" --color "D4C5F9" --description "Requirements unclear"              --repo "${OWNER}/${REPO}" --force
gh label create "needs: triage"        --color "D4C5F9" --description "Needs priority/area assignment"   --repo "${OWNER}/${REPO}" --force
gh label create "duplicate"            --color "CFD3D7" --description "Duplicate issue"                   --repo "${OWNER}/${REPO}" --force
gh label create "wontfix"              --color "CFD3D7" --description "Will not address"                  --repo "${OWNER}/${REPO}" --force

echo "==> Labels created."

# ---------------------------------------------------------------------------
# Projects v2 board
# ---------------------------------------------------------------------------
echo "==> Creating Projects v2 board..."

PROJECT_URL=$(gh project create --owner "${OWNER}" --title "talos-mcp Development" --format json | jq -r '.url')
PROJECT_NUMBER=$(echo "$PROJECT_URL" | grep -oE '[0-9]+$')

echo "    Project URL:    ${PROJECT_URL}"
echo "    Project number: ${PROJECT_NUMBER}"

gh variable set PROJECT_NUMBER --repo "${OWNER}/${REPO}" --body "$PROJECT_NUMBER"
echo "    Repository variable PROJECT_NUMBER set to ${PROJECT_NUMBER}."

# ---------------------------------------------------------------------------
# Manual steps (GraphQL required — not automated here)
# ---------------------------------------------------------------------------
cat <<EOF

==> MANUAL STEPS REQUIRED
   The following fields must be added manually to the project board via the
   GitHub web UI or GraphQL API (single-select fields cannot be created by
   the gh CLI as of this writing):

   1. Open the project at: ${PROJECT_URL}
      (or navigate to https://github.com/users/Nosmoht/projects/${PROJECT_NUMBER})

   2. Add a single-select field named "Status" with options:
        Ready | Assigned | In Progress | Review Pending | Blocked | Done

   3. Add a single-select field named "Priority" with options:
        P0 | P1 | P2 | P3

   4. Add a single-select field named "Size" with options:
        XS | S | M | L | XL

   Once these fields exist, the project-sync.yml workflow will populate them
   automatically from issue labels using their names (never hardcoded IDs).

EOF
