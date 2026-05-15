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

Commits follow [Conventional Commits v1.0.0](https://www.conventionalcommits.org/en/v1.0.0/) using the type vocabulary from [Angular's commit-message guidelines](https://github.com/angular/angular/blob/main/contributing-docs/commit-message-guidelines.md) (verbatim definitions live upstream; this table records release impact and repo-specific examples).

All commits use the form `<type>(<scope>): <subject>`. Types and scopes are lowercase ASCII (e.g. `fix(http):`, never `Fix(HTTP):`).

| Type | Release impact | Repo example |
|---|---|---|
| `feat` | MINOR | `223a0c0 feat(subscriptions): add MCP resource subscriptions via COSI watch` |
| `fix` | PATCH | `5dc4794 fix(http): restore cross-origin protection default after go-sdk v1.6.0 bump` |
| `perf` | PATCH | `546339d perf(tools): replace json.MarshalIndent with json.Marshal for compact AI-agent responses` |
| `refactor` | none | `ae16178 refactor(talos): introduce ClientInterface to decouple tool handlers from concrete client` |
| `docs` | none | `ff8cf01 docs(agents): promote four process rules from local memory to repo` |
| `test` | none | `1032816 test(transport): add CSRF regression test for cross-origin protection` |
| `ci` | none | `c7d80a8 ci(lint): add exhaustive and thelper linters` |
| `build` | none | `8a23dff build(deps): bump actions/upload-artifact from 7.0.0 to 7.0.1` |
| `chore` | none | `0f09cd1 chore(safety): gitignore mcpregistry token files` |
| `style` | none | gofmt-only diff with no semantic change |
| `revert` | matches the reverted commit's type | `git revert` output (or manual revert with the same form) |

**Breaking changes** trigger MAJOR regardless of type. Mark them either by appending `!` after the type/scope (`feat(api)!:`) **or** by adding a `BREAKING CHANGE: <description>` footer. Either form is accepted; both forms together is redundant but harmless.

### Local rule — `chore:` vs `refactor:`

Angular's guidelines do not formally include `chore:` and the boundary between `chore:` and `refactor:` is not codified upstream. This repo's local rule:

- Use **`refactor:`** if the diff modifies code that ships in the compiled binary or its test fixtures — `cmd/`, `internal/`, `pkg/`, or `*_test.go` files alongside those packages. Example: replacing `log.Printf` with `slog` in `internal/tools/` is `refactor:`, not `chore:`.
- Use **`chore:`** only when the diff is confined to repo housekeeping that does not affect the shipped binary: `.gitignore`, `.claude/`, `.github/`'s non-CI/non-build files, repo-metadata configs.
- `.goreleaser.yaml`, `go.mod`, `go.sum`, `Makefile`, `npm/` packaging are **`build:`** (they affect the build system or distribution).
- CI workflow files and scripts (`.github/workflows/**`, `.github/scripts/**`) are **`ci:`**.
- Documentation-only changes are **`docs:`** regardless of file location (including godoc-comment-only edits).

Anti-pattern, for clarity: `95a9161 chore(http): migrate off deprecated CrossOriginProtection field` modifies `cmd/talos-mcp/main.go` — under this rule, that commit would be `refactor(http):` (no release in either case, so no retroactive correction; cited only as a forward-looking example).

### Post-merge release pipeline

Every merge to `main` whose commit type triggers a version bump (`feat:` → MINOR, `fix:` → PATCH, `perf:` → PATCH, any `!`-marked or `BREAKING CHANGE:`-footer → MAJOR) drives an automatic release — *provided the commit touched server-relevant paths* (`*.go`, `cmd/**`, `internal/**`, `go.mod`, `go.sum`, `Makefile`, `.goreleaser.yaml`, `server.json`; see `.github/workflows/auto-tag.yml` for the canonical path filter):

1. `auto-tag.yml` inspects the pushed commit's conventional-commit type via `mathieudutour/github-tag-action` and pushes a `v<next>` tag to the repo (no intermediate release PR).
2. The `v*` tag triggers `release.yml`, which runs `goreleaser` (binary artifacts + provenance attestations + a GitHub Release) and then `npm publish --provenance` via OIDC Trusted Publishing (no long-lived npm tokens; see `.claude/rules/release-workflow.md` for OIDC gotchas).
3. Operators upgrade via `npm install -g talos-mcp@latest` once the npm-publish job succeeds.

`docs:` / `chore:` / `refactor:` / `test:` / `ci:` / `build:` / `style:` / `revert:` merges do **not** trigger a release (the type is excluded from version bumps and/or the touched paths fall outside the auto-tag filter). The full type → release-impact table is above.

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
