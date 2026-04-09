---
description: npm OIDC Trusted Publishing gotchas for release.yml. Use when editing release CI workflows. Do NOT use for non-CI changes.
globs:
  - .github/workflows/release.yml
---

# npm OIDC Trusted Publishing (release.yml)

- Use `node-version: "24"` — Node.js 22 bundles npm 10.x which cannot self-upgrade to npm 11 (`MODULE_NOT_FOUND` during `npm install -g npm@11`); Node.js 24 ships with npm 11 natively
- Do **not** set `registry-url` in `actions/setup-node` — it writes `.npmrc` with `_authToken=${NODE_AUTH_TOKEN}` placeholder that blocks the OIDC token exchange (causes 404)
- Keep `--provenance` on all `npm publish` calls — provenance is not auto-generated; the flag is harmless with OIDC and required with tokens
- Trusted Publishers must be configured per-package on npmjs.com before OIDC works (one-time setup per package at `https://www.npmjs.com/package/<name>/access`)
