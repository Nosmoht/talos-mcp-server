---
name: release-preflight
description: Pre-tag validation for talos-mcp releases — resolve release range, scan conventional commit prefixes, derive expected bump, run goreleaser check, verify release.yml npm OIDC invariants (node-version "24", no registry-url, --provenance). Run before pushing a v* tag.
allowed-tools: Read, Grep, Bash
argument-hint: "[--from <prev-tag>]"
---

# Release Preflight

Validate release readiness before a `v*` tag is pushed. Fails closed — any
blocker yields `NO-GO` and the skill never instructs the user to push on
`NO-GO`.

## Arguments

Parse `$ARGUMENTS`:

- `--from <tag>` — base tag. If absent, resolve via:
  ```bash
  git describe --tags --abbrev=0 --match 'v*'
  ```
  If no prior `v*` tag exists and `--from` was not supplied, abort:
  `NO-GO: no prior v* tag and --from not supplied.`

Bind the resolved value as `PREV`.

## Key Steps

1. **Resolve the release range.** Scan commits that touched server paths
   (release triggers per `CLAUDE.md` § Release):
   ```bash
   git log "$PREV"..HEAD --pretty=format:'%s%n%b' \
     -- cmd internal go.mod go.sum Makefile
   ```
   If the filtered result is empty, abort:
   `NO-GO: range contains only docs/ci/chore changes — do not tag.`

2. **Derive expected bump.** Apply this mapping to the filtered commit
   subjects, take the highest bump seen:

   | Prefix                                     | Bump   |
   |--------------------------------------------|--------|
   | `feat!:` / `BREAKING CHANGE:` in body      | major  |
   | `feat:`                                    | minor  |
   | `fix:`                                     | patch  |
   | `docs:` / `ci:` / `chore:` / `build:`      | none   |

   Confirm with the user via `AskUserQuestion` (header `Bump`): computed
   bump as the recommended option, plus alternatives and `Cancel`.
   `Cancel` ⇒ `NO-GO`.

3. **Goreleaser.**
   ```bash
   command -v goreleaser >/dev/null || blocker "goreleaser not installed"
   goreleaser check                 || blocker "goreleaser check failed"
   ```

4. **release.yml invariants.** Verify `.github/workflows/release.yml`
   exists (blocker if not), then apply the checks from
   `references/release-checks.md`:
   - `node-version: "24"` (not 22, not 20)
   - no `registry-url:` set on `actions/setup-node`
   - `--provenance` present on every `npm publish` call
   Each mismatch is a blocker; continue running remaining checks to build
   a complete report.

5. **Trusted Publisher reminder.** If the range is the first release of
   the npm package (no prior `v*` tag), prompt via `AskUserQuestion`:
   `"Trusted Publisher configured at https://www.npmjs.com/package/<pkg>/access?"` —
   `No` ⇒ `NO-GO`.

6. **Emit the report** (exact template below). On any blocker, `Verdict:
   NO-GO`.

## Report Format

```
## Release Preflight — <PREV>..HEAD

Verdict: GO | NO-GO

| # | Check                        | Status | Evidence                          |
|---|------------------------------|--------|-----------------------------------|
| 1 | node-version "24"            | pass   | release.yml:<line> matches        |
| 2 | no registry-url              | pass   | 0 matches in release.yml          |
| 3 | --provenance on publish      | pass   | release.yml:<line>                |
| 4 | commit-prefix scan           | pass   | bump=<patch|minor|major>          |
| 5 | goreleaser check             | pass   | exit 0                            |
| 6 | trusted publisher (if first) | pass   | user-confirmed                    |

Expected bump: <bump>   Intended tag: v<x.y.z>

Blockers: <empty on GO; otherwise bulleted list with fix hints>
```

## References

- `references/release-checks.md` — full catalogue expanded from `.claude/rules/release-workflow.md` with concrete verification commands.
