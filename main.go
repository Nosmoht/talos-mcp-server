// talos-mcp is a Model Context Protocol server that exposes Talos Linux cluster
// management operations to AI agents (Claude Code, Codex, etc.).
//
// It connects to a Talos cluster via the native gRPC API using the talosconfig
// credentials from ~/.talos/config (or $TALOSCONFIG).
//
// Environment variables:
//   - TALOSCONFIG: path to talosconfig file (default: ~/.talos/config)
//   - TALOS_CONTEXT: context name to use (default: active context in config)
//   - TALOS_ENDPOINTS: comma-separated endpoint overrides
//   - TALOS_MCP_READ_ONLY: set to "true" to disable all mutating tools
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Build info injected by GoReleaser via ldflags.
//
//nolint:gochecknoglobals
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	ctx := context.Background()

	readOnly := os.Getenv("TALOS_MCP_READ_ONLY") == "true"

	tc, err := NewTalosClient(ctx)
	if err != nil {
		log.Fatalf("failed to create Talos client: %v", err)
	}
	defer tc.Close() //nolint:errcheck

	log.Printf("talos-mcp version=%s commit=%s date=%s read_only=%v", version, commit, date, readOnly)

	server := mcp.NewServer(&mcp.Implementation{
		Name:    "talos",
		Version: version,
	}, &mcp.ServerOptions{
		Instructions: "Talos Linux cluster management server. " +
			"Start with talos_resource_definitions to see all available resource types, " +
			"then use talos_get to query them. " +
			"All tools accept an optional 'nodes' field to target specific node IPs; " +
			"omit it to use the active context from talosconfig. " +
			"Destructive tools (talos_reboot, talos_upgrade) require confirm=true and explicit nodes.",
	})

	// ── Read-only tools ──────────────────────────────────────────────────────

	mcp.AddTool(server, &mcp.Tool{
		Name:        "talos_resource_definitions",
		Description: "List all available Talos resource types with their aliases. Call this first to discover what resources can be queried with talos_get.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, tc.handleResourceDefinitions)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "talos_get",
		Description: "Get Talos resources by type. Use talos_resource_definitions to discover available types. Examples: MachineStatus, Member, NodeAddress, LinkStatus, Route, Service, Extension.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, tc.handleGetResource)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "talos_version",
		Description: "Get Talos version information from the target nodes.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, tc.handleVersion)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "talos_services",
		Description: "List all Talos services and their current state (running, stopped, health status).",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, tc.handleServices)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "talos_containers",
		Description: "List containers running in the specified namespace. Defaults to the 'k8s.io' namespace (Kubernetes containers).",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, tc.handleContainers)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "talos_processes",
		Description: "List running processes on the target nodes.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, tc.handleProcesses)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "talos_health",
		Description: "Check the health of the Talos cluster (etcd, Kubernetes API, node readiness). Waits up to wait_timeout for all checks to pass.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, tc.handleHealth)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "talos_logs",
		Description: "Stream recent service logs from the target nodes. Returns the last tail_lines lines without following.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, tc.handleLogs)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "talos_dmesg",
		Description: "Read kernel ring buffer (dmesg) messages from the target nodes.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, tc.handleDmesg)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "talos_events",
		Description: "Fetch recent Talos runtime events (node lifecycle, service changes, config changes). Returns the last tail_count events.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, tc.handleEvents)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "talos_etcd",
		Description: "Query etcd cluster information. Use subcommand='members' (default) or subcommand='status'.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, tc.handleEtcd)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "talos_list_files",
		Description: "List files and directories on a target node filesystem.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, tc.handleListFiles)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "talos_read_file",
		Description: "Read the contents of a file from a target node filesystem (e.g. /etc/os-release, /etc/machine-config.yaml).",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, tc.handleReadFile)

	// ── Write / mutating tools ───────────────────────────────────────────────
	// Skipped when TALOS_MCP_READ_ONLY=true.

	if !readOnly {
		destructive := boolPtr(true)

		mcp.AddTool(server, &mcp.Tool{
			Name:        "talos_service_action",
			Description: "Start, stop, or restart a Talos service on the target nodes.",
			Annotations: &mcp.ToolAnnotations{DestructiveHint: destructive},
		}, tc.handleServiceAction)

		mcp.AddTool(server, &mcp.Tool{
			Name:        "talos_reboot",
			Description: "Reboot the specified nodes. Requires explicit nodes and confirm=true. Use mode='powercycle' for a full power cycle.",
			Annotations: &mcp.ToolAnnotations{DestructiveHint: destructive},
		}, tc.handleReboot)

		mcp.AddTool(server, &mcp.Tool{
			Name:        "talos_upgrade",
			Description: "Upgrade Talos on the specified nodes. Requires explicit nodes, an installer image reference, and confirm=true.",
			Annotations: &mcp.ToolAnnotations{DestructiveHint: destructive},
		}, tc.handleUpgrade)

		mcp.AddTool(server, &mcp.Tool{
			Name: "talos_patch_config",
			Description: "Apply a machine config patch to the target nodes. " +
				"Defaults to dry_run=true — set dry_run=false to actually apply. " +
				"Patch can be a JSON or YAML strategic merge patch.",
			Annotations: &mcp.ToolAnnotations{DestructiveHint: destructive},
		}, tc.handlePatchConfig)
	}

	if err := server.Run(ctx, &mcp.StdioTransport{}); err != nil {
		log.Printf("server stopped: %v", err)
	}
}

// boolPtr returns a pointer to a bool value.
func boolPtr(b bool) *bool {
	return &b
}

// jsonMarshal marshals v to indented JSON string.
func jsonMarshal(v any) (string, error) {
	out, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal JSON: %w", err)
	}

	return string(out), nil
}
