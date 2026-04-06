---
change-id: <semantic-slug, e.g. "add-etcd-defrag-tool">
review-type: <plan-review | impl-review | final-approval>
reviewer-role: <senior-plan-reviewer | staff-reviewer | principal-architect-reviewer>
status: <approved | changes-requested>
timestamp: <ISO 8601, e.g. 2026-04-06T14:00:00Z>
reviewed-scope:
  - <file path or "full plan" or "full change">
findings: []
# For final-approval, also include:
# gate-checks:
#   plan-review: <approved | missing | changes-requested>
#   impl-review: <approved | missing | changes-requested>
#   role-separation: <verified | violated>
---

## Notes

Free-form reviewer commentary, rationale, or cross-references.

---

## Usage

1. Copy this template to `.claude/reviews/<change-id>/<review-type>.md`
2. Fill in all YAML frontmatter fields
3. Set `status: approved` if and only if `findings` list is empty
4. Add findings as structured YAML entries in the frontmatter (not markdown bullets)
5. The hook script validates the YAML frontmatter — formatting matters

### Finding structure

```yaml
findings:
  - severity: critical    # critical | major | minor
    description: "what's wrong"
    location: "internal/tools/etcd.go:45"
    fix: "how to fix it"
```

### Severity guide

- **critical**: Runtime failure, data loss, security vulnerability, CI breakage
- **major**: Violates established patterns, missing test coverage, incorrect docs
- **minor**: Style nits, naming suggestions, optional improvements
