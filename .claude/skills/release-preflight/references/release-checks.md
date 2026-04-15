# Release Checks

Catalogue of pre-tag validation items for talos-mcp-server. Each entry: check name, how to verify, expected result. Derived from `.claude/rules/release-workflow.md` and `CLAUDE.md § Release`.

## release.yml Configuration Checks

1. **node-version pin = "24"**
   - How: `grep -n 'node-version' .github/workflows/release.yml`
   - Expected: every `actions/setup-node` step specifies `node-version: "24"`. Node 22 ships npm 10.x which cannot self-upgrade to npm 11 (`MODULE_NOT_FOUND`). Node 24 ships npm 11 natively.

2. **No `registry-url` in `actions/setup-node`**
   - How: `grep -n 'registry-url' .github/workflows/release.yml`
   - Expected: zero matches. `registry-url` writes `.npmrc` with `_authToken=${NODE_AUTH_TOKEN}` placeholder, which blocks the OIDC token exchange and returns 404.

3. **`--provenance` on every `npm publish`**
   - How: `grep -nE 'npm publish' .github/workflows/release.yml`
   - Expected: every `npm publish` invocation includes `--provenance`. Provenance is not auto-generated; the flag is harmless under OIDC and required under token auth.

4. **Trusted Publisher configured on npmjs.com**
   - How: Manual verification at `https://www.npmjs.com/package/<name>/access`.
   - Expected: Trusted Publisher entry exists for this GitHub repo + workflow. One-time per package.

## Release-Range Conventional Commit Scan

5. **Commit prefix scan for bump derivation**
   - How: `git log <prev-tag>..HEAD --pretty=format:'%s'`
   - Expected: each subject begins with a recognised conventional prefix. Map to bump:

     | Prefix                                  | Server-path bump |
     |-----------------------------------------|------------------|
     | `fix:`                                  | patch            |
     | `feat:`                                 | minor            |
     | `feat!:` / body contains `BREAKING CHANGE:` | major        |
     | `docs:` / `ci:` / `chore:`              | no release       |

     Only server paths trigger a release — doc/CI/chore-only ranges must not be tagged.

6. **Bump consistency**
   - How: Take the highest bump implied by the scan.
   - Expected: matches the tag the user intends to push. Mismatch = abort.

## Build-System Checks

7. **`goreleaser check`**
   - How: `goreleaser check`
   - Expected: exit 0, no warnings. Validates `.goreleaser.yaml` syntax and config before a tag push triggers a real release.

## Final Gate

8. **Go/No-Go report**
   - How: Aggregate results of checks 1–7.
   - Expected: all checks pass → GO. Any failure → NO-GO with the offending check id(s) listed.
