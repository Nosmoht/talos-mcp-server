# Contributing to talos-mcp

## Quick start

```bash
git clone https://github.com/Nosmoht/talos-mcp-server
cd talos-mcp-server
make check   # fmt + vet + lint + test
```

## Prerequisites

- Go 1.21+
- [golangci-lint](https://golangci-lint.run/welcome/install/) — required for `make lint` and `make check`

```bash
go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
```

## Development

```bash
make build   # build binary
make test    # run tests with race detector + coverage
make lint    # run linter
make check   # full CI parity (fmt + vet + lint + test)
```

## Commit messages

This project uses [conventional commits](https://www.conventionalcommits.org/). All commits must use a scoped prefix:

| Prefix | Effect |
|---|---|
| `feat(scope):` | New feature → minor version bump |
| `fix(scope):` | Bug fix → patch version bump |
| `feat!:` / `BREAKING CHANGE:` | Breaking change → major version bump |
| `docs:`, `ci:`, `chore:`, `refactor:`, `test:` | No release triggered |

## Pull requests

1. Fork the repo and create a branch from `main`
2. Ensure `make check` passes locally
3. Fill in the PR template
4. One logical change per PR

## Branch protection

The `main` branch has the following protections enforced via GitHub branch protection rules:

| Rule | Setting |
|---|---|
| Required status check | `merge-guard` (sole required check) |
| Status checks must be up to date | No (not required — `merge-guard` handles skipped jobs correctly) |
| Required approving reviews | 1 |
| Force push | Disabled |
| Branch deletion | Disabled |

**Why `merge-guard` and not the individual jobs?**
The CI workflow uses a `changes` job (dorny/paths-filter) to skip Go-specific jobs (`lint`, `test`, `build`, `verify`) on PRs that do not touch server code. If those jobs were listed as required checks, a docs-only PR would be permanently blocked waiting for skipped jobs to pass. `merge-guard` is the fan-in job that runs `if: always()` and fails only when a job that _did_ run reported failure or cancellation. It handles both the "all jobs ran and passed" case and the "jobs were legitimately skipped" case.

**Re-applying protection rules**
If branch protection is accidentally removed, re-apply it with:

```bash
gh api --method PUT /repos/Nosmoht/talos-mcp-server/branches/main/protection \
  --input - <<'EOF'
{
  "required_status_checks": {
    "strict": false,
    "checks": [{"context": "merge-guard", "app_id": -1}]
  },
  "enforce_admins": false,
  "required_pull_request_reviews": {
    "required_approving_review_count": 1
  },
  "restrictions": null,
  "allow_force_pushes": false,
  "allow_deletions": false
}
EOF
```

## Security vulnerabilities

Do not open public issues for security bugs. Use [GitHub Private Vulnerability Reporting](https://github.com/Nosmoht/talos-mcp-server/security/advisories/new) instead. See [SECURITY.md](SECURITY.md) for details.

## License

By contributing, you agree your contributions will be licensed under the [MIT License](LICENSE).
