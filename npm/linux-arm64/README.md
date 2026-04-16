# @talos-mcp/linux-arm64

The Linux ARM64 (`linux` / `arm64`) binary distribution of [`talos-mcp`](https://www.npmjs.com/package/talos-mcp), an MCP server for Talos Linux cluster management via the native gRPC API.

## Do not install directly

This package is an internal platform artifact. Install the top-level wrapper instead:

```bash
npx talos-mcp
```

The wrapper uses npm's `optionalDependencies` + `os`/`cpu` constraints to pull in the correct platform package automatically.

## What's inside

A statically-linked Go binary (`bin/talos-mcp`) built from the upstream [Nosmoht/talos-mcp-server](https://github.com/Nosmoht/talos-mcp-server) repository. Releases are tagged, built reproducibly by GoReleaser, and published with:

- npm OIDC trusted publishing (tokenless)
- npm `--provenance` attestations
- Sigstore cosign keyless signatures on release checksums
- SLSA L2 GitHub build-provenance attestations
- CycloneDX SBOM (uploaded as a release asset)

## Links

- **Source / docs / issue tracker:** [github.com/Nosmoht/talos-mcp-server](https://github.com/Nosmoht/talos-mcp-server)
- **Top-level package:** [talos-mcp on npm](https://www.npmjs.com/package/talos-mcp)
- **Security policy:** [SECURITY.md](https://github.com/Nosmoht/talos-mcp-server/blob/main/SECURITY.md)
- **Changelog:** [CHANGELOG.md](https://github.com/Nosmoht/talos-mcp-server/blob/main/CHANGELOG.md)

## License

[MIT](https://github.com/Nosmoht/talos-mcp-server/blob/main/LICENSE)
