#!/usr/bin/env node

"use strict";

const { spawnSync } = require("child_process");
const path = require("path");
const fs = require("fs");

const PLATFORMS = {
  "darwin-arm64": "@talos-mcp/darwin-arm64",
  "darwin-x64": "@talos-mcp/darwin-x64",
  "linux-arm64": "@talos-mcp/linux-arm64",
  "linux-x64": "@talos-mcp/linux-x64",
};

const platformKey = `${process.platform}-${process.arch}`;
const pkg = PLATFORMS[platformKey];

if (!pkg) {
  process.stderr.write(
    `talos-mcp: unsupported platform ${platformKey}.\n` +
    `Supported: ${Object.keys(PLATFORMS).join(", ")}.\n` +
    `Build from source: https://github.com/Nosmoht/talos-mcp-server\n`
  );
  process.exit(1);
}

let binPath;
try {
  const pkgDir = path.dirname(require.resolve(`${pkg}/package.json`));
  binPath = path.join(pkgDir, "bin", "talos-mcp");
} catch (_) {
  process.stderr.write(
    `talos-mcp: platform package ${pkg} is not installed.\n` +
    `Try: npm install ${pkg}\n`
  );
  process.exit(1);
}

if (!fs.existsSync(binPath)) {
  process.stderr.write(`talos-mcp: binary not found at ${binPath}\n`);
  process.exit(1);
}

const result = spawnSync(binPath, process.argv.slice(2), { stdio: "inherit" });
process.exit(result.status ?? 1);
