# talos-mcp

MCP server for Talos Linux cluster management via native gRPC API.

Connects AI agents (Claude Code, OpenAI Codex, and any MCP-compatible client) to your Talos Linux cluster. Tools return structured JSON directly from the Talos gRPC API — no shell-scraping, no token cost for intermediate output.

## Installation

```bash
npx talos-mcp
```

No Go toolchain required. The correct native binary for your platform (Linux/macOS, x64/ARM64) is installed automatically.

## Configuration

Reads `~/.talos/config` by default — the same credentials used by `talosctl`.

Set `TALOS_MCP_READ_ONLY=true` to expose only read-only tools.

## Documentation

Full docs, client setup (Claude Code, Claude Desktop, Codex), tool reference, security model, and HTTP transport guide:

**[https://github.com/Nosmoht/talos-mcp-server](https://github.com/Nosmoht/talos-mcp-server)**

## License

[MIT](https://github.com/Nosmoht/talos-mcp-server/blob/main/LICENSE)
