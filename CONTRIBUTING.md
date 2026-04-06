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

## Security vulnerabilities

Do not open public issues for security bugs. Use [GitHub Private Vulnerability Reporting](https://github.com/Nosmoht/talos-mcp-server/security/advisories/new) instead. See [SECURITY.md](SECURITY.md) for details.

## License

By contributing, you agree your contributions will be licensed under the [MIT License](LICENSE).
