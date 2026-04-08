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
//   - TALOS_MCP_HTTP_ADDR: if set (e.g. ":8080"), serve HTTP instead of stdio
//   - TALOS_MCP_AUTH_TOKEN: required bearer token when HTTP mode is active
package main

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Nosmoht/talos-mcp-server/internal/prompts"
	"github.com/Nosmoht/talos-mcp-server/internal/resources"
	"github.com/Nosmoht/talos-mcp-server/internal/talos"
	"github.com/Nosmoht/talos-mcp-server/internal/tools"
	talosversion "github.com/Nosmoht/talos-mcp-server/internal/version"
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
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	readOnly := os.Getenv("TALOS_MCP_READ_ONLY") == "true"
	httpAddr := os.Getenv("TALOS_MCP_HTTP_ADDR")
	authToken := os.Getenv("TALOS_MCP_AUTH_TOKEN")
	os.Unsetenv("TALOS_MCP_AUTH_TOKEN") //nolint:errcheck // remove token from /proc/<pid>/environ

	if err := validateHTTPConfig(httpAddr, authToken); err != nil {
		stop()
		log.Fatalf("%v", err) //nolint:gocritic // exitAfterDefer: stop() called explicitly above
	}

	tc, err := talos.NewClient(ctx)
	if err != nil {
		stop()
		log.Fatalf("failed to create Talos client: %v", err) //nolint:gocritic // exitAfterDefer: stop() called explicitly above
	}
	defer tc.Close() //nolint:errcheck

	log.Printf("talos-mcp version=%q commit=%q date=%q read_only=%v", version, commit, date, readOnly) //nolint:gosec // G706 false positive: version/commit/date are build-time ldflags constants injected by GoReleaser, not runtime user input

	// Best-effort cluster version compatibility check. Non-fatal — the server
	// starts regardless and operators can set TALOS_MCP_SKIP_VERSION_CHECK=true
	// to suppress validation warnings.
	if cv, err := tc.GetClusterVersion(ctx); err != nil {
		log.Printf("WARNING: could not detect cluster Talos version: %v", err)
	} else if !cv.InSupportedRange() {
		log.Printf("WARNING: cluster Talos version %s is outside the tested range (%s – %s); some features may not work correctly",
			cv, talosversion.MinSupported, talosversion.MaxTested)
	} else {
		log.Printf("cluster Talos version: %s (supported)", cv)
	}

	h := &tools.Handlers{Client: tc}

	serverOpts := &mcp.ServerOptions{
		Instructions: "Talos Linux cluster management server. " +
			"MCP Resources: read talos://cluster/resource-definitions to discover COSI resource types, " +
			"then read talos://{node}/resource/{namespace}/{type} to list them or " +
			"talos://{node}/resource/{namespace}/{type}/{id} to get a specific one. " +
			"Tools: use talos_get for node-targeted queries with aliases and namespace auto-resolution. " +
			"All tools accept an optional 'nodes' field to target specific node IPs; " +
			"omit it to use the active context from talosconfig. " +
			"Destructive tools (talos_reboot, talos_upgrade) require confirm=true and explicit nodes.",
	}

	if httpAddr == "" {
		// Stdio is single-session: wire per-session MCP log notifications.
		// HTTP mode omits this — multiple concurrent sessions would race on the
		// shared atomic.Pointer[slog.Logger], misdirecting notifications.
		serverOpts.InitializedHandler = func(initCtx context.Context, req *mcp.InitializedRequest) {
			logger := slog.New(mcp.NewLoggingHandler(req.Session, &mcp.LoggingHandlerOptions{
				LoggerName: "talos-mcp",
			}))
			h.SetLogger(logger)

			// Forward version compatibility warning to the connected MCP client.
			if cv, err := tc.GetClusterVersion(initCtx); err == nil && !cv.InSupportedRange() {
				logger.Warn("cluster Talos version is outside the tested range; some features may not work correctly",
					"version", cv.String(),
					"min_supported", talosversion.MinSupported.String(),
					"max_tested", talosversion.MaxTested.String(),
				)
			}
		}
	}

	server := mcp.NewServer(&mcp.Implementation{
		Name:    "talos",
		Version: version,
	}, serverOpts)

	// All tools operate on a specific configured Talos cluster (closed world).
	closedWorld := boolPtr(false)

	// ── Read-only tools ──────────────────────────────────────────────────────

	mcp.AddTool(server, &mcp.Tool{
		Name:        "talos_resource_definitions",
		Description: "List all available Talos resource types with their aliases. Call this first to discover what resources can be queried with talos_get.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: closedWorld},
	}, h.HandleResourceDefinitions)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "talos_get",
		Description: "Get Talos resources by type. Use talos_resource_definitions to discover available types. Examples: MachineStatus, Member, NodeAddress, LinkStatus, Route, Service, Extension.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: closedWorld},
	}, h.HandleGetResource)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "talos_version",
		Description: "Get Talos version information from the target nodes.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: closedWorld},
	}, h.HandleVersion)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "talos_services",
		Description: "List all Talos services and their current state (running, stopped, health status).",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: closedWorld},
	}, h.HandleServices)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "talos_containers",
		Description: "List containers running in the specified namespace. Defaults to the 'k8s.io' namespace (Kubernetes containers).",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: closedWorld},
	}, h.HandleContainers)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "talos_processes",
		Description: "List running processes on the target nodes.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: closedWorld},
	}, h.HandleProcesses)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "talos_health",
		Description: "Check the health of the Talos cluster (etcd, Kubernetes API, node readiness). Waits up to wait_timeout for all checks to pass.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: closedWorld},
	}, h.HandleHealth)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "talos_logs",
		Description: "Stream recent service logs from the target nodes. Returns the last tail_lines lines without following.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: closedWorld},
	}, h.HandleLogs)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "talos_dmesg",
		Description: "Read kernel ring buffer (dmesg) messages from the target nodes.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: closedWorld},
	}, h.HandleDmesg)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "talos_events",
		Description: "Fetch recent Talos runtime events (node lifecycle, service changes, config changes). Returns the last tail_count events.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: closedWorld},
	}, h.HandleEvents)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "talos_etcd",
		Description: "Query etcd cluster information. Use subcommand='members' (default) or subcommand='status'.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: closedWorld},
	}, h.HandleEtcd)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "talos_list_files",
		Description: "List files and directories on a target node filesystem.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: closedWorld},
	}, h.HandleListFiles)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "talos_read_file",
		Description: "Read the contents of a file from a target node filesystem (e.g. /etc/os-release, /etc/machine-config.yaml).",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: closedWorld},
	}, h.HandleReadFile)

	// ── Write / mutating tools ───────────────────────────────────────────────
	// Skipped when TALOS_MCP_READ_ONLY=true.

	if !readOnly {
		destructive := boolPtr(true)

		mcp.AddTool(server, &mcp.Tool{
			Name: "talos_service_action",
			Description: "Start, stop, or restart a Talos service on the target nodes. " +
				"NOTE: restarting 'etcd' is not supported by the Talos API and will return an error; " +
				"use talos_reboot or the investigate-etcd prompt to recover etcd.",
			Annotations: &mcp.ToolAnnotations{DestructiveHint: destructive, OpenWorldHint: closedWorld},
		}, h.HandleServiceAction)

		mcp.AddTool(server, &mcp.Tool{
			Name: "talos_reboot",
			Description: "Reboot the specified nodes. Requires explicit nodes and confirm=true. " +
				"All listed nodes are rebooted simultaneously — reboot one node at a time to avoid a full cluster outage. " +
				"Use mode='powercycle' for a full power cycle or mode='force' to skip graceful shutdown on stuck nodes. " +
				"Set wait=true to block until all node(s) complete reboot and are back up (verified via boot ID change). " +
				"Use timeout to control max wait time (default: '5m').",
			Annotations: &mcp.ToolAnnotations{DestructiveHint: destructive, OpenWorldHint: closedWorld},
		}, h.HandleReboot)

		mcp.AddTool(server, &mcp.Tool{
			Name: "talos_upgrade",
			Description: "Upgrade Talos on the specified nodes. Requires explicit nodes, an installer image reference, and confirm=true. " +
				"Set preserve=true (default) to keep the EPHEMERAL partition intact. " +
				"Use stage=true to defer the upgrade to the next reboot. " +
				"Use reboot_mode='powercycle' for a full power cycle after upgrade. " +
				"Use talos_health after upgrade to verify cluster state.",
			Annotations: &mcp.ToolAnnotations{DestructiveHint: destructive, OpenWorldHint: closedWorld},
		}, h.HandleUpgrade)

		mcp.AddTool(server, &mcp.Tool{
			Name: "talos_rollback",
			Description: "Roll back the last Talos upgrade on the specified nodes, reverting to the previous boot asset. " +
				"Requires explicit nodes and confirm=true. " +
				"Only works if the previous installation is still intact (i.e. no second upgrade was performed). " +
				"Use talos_health after rollback to verify cluster state.",
			Annotations: &mcp.ToolAnnotations{DestructiveHint: destructive, OpenWorldHint: closedWorld},
		}, h.HandleRollback)

		mcp.AddTool(server, &mcp.Tool{
			Name: "talos_patch_config",
			Description: "Apply a machine config patch to the target nodes. " +
				"Defaults to dry_run=true — set dry_run=false to actually apply. " +
				"Requires confirm=true when dry_run=false. " +
				"Patch can be a JSON or YAML strategic merge patch.",
			Annotations: &mcp.ToolAnnotations{DestructiveHint: destructive, OpenWorldHint: closedWorld},
		}, h.HandlePatchConfig)
	}

	resources.Register(server, tc)
	prompts.Register(server, readOnly)

	if err := runServer(ctx, server, httpAddr, authToken); err != nil {
		log.Printf("server stopped: %v", err)
	}
}

// boolPtr returns a pointer to a bool value.
func boolPtr(b bool) *bool {
	return &b
}

// validateHTTPConfig returns an error if HTTP mode is requested without an auth token.
func validateHTTPConfig(addr, token string) error {
	if addr != "" && token == "" {
		return fmt.Errorf("TALOS_MCP_AUTH_TOKEN must be set when TALOS_MCP_HTTP_ADDR is configured")
	}
	return nil
}

// buildTokenVerifier constructs a bearer token verifier using constant-time comparison.
// TokenInfo.Expiration is set to a far-future value to satisfy the SDK's non-zero contract.
func buildTokenVerifier(token string) auth.TokenVerifier {
	tokenBytes := []byte(token)
	return func(_ context.Context, incoming string, _ *http.Request) (*auth.TokenInfo, error) {
		if subtle.ConstantTimeCompare([]byte(incoming), tokenBytes) != 1 {
			return nil, fmt.Errorf("%w: bearer token mismatch", auth.ErrInvalidToken)
		}
		return &auth.TokenInfo{
			Expiration: time.Now().Add(365 * 24 * time.Hour),
		}, nil
	}
}

// runServer starts the server in either stdio or HTTP mode.
func runServer(ctx context.Context, server *mcp.Server, addr, token string) error {
	if addr == "" {
		// stdio mode — unchanged behaviour
		return server.Run(ctx, &mcp.StdioTransport{})
	}

	// HTTP mode
	mcpHandler := mcp.NewStreamableHTTPHandler(func(_ *http.Request) *mcp.Server {
		return server
	}, &mcp.StreamableHTTPOptions{
		// DisableLocalhostProtection allows proxied requests whose Host header
		// differs from the bind address (e.g. behind nginx/Caddy/Tailscale).
		DisableLocalhostProtection: true,
		Logger:                     slog.Default(),
	})

	verifier := buildTokenVerifier(token)
	authedHandler := auth.RequireBearerToken(verifier, nil)(mcpHandler)

	httpSrv := &http.Server{
		Addr:              addr,
		Handler:           authedHandler,
		ReadHeaderTimeout: 30 * time.Second,
	}

	// Shutdown on context cancellation.
	go func() { //nolint:gosec // G118: intentional — shutdown uses a fresh background context, not the cancelled one
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second) //nolint:contextcheck
		defer cancel()
		_ = httpSrv.Shutdown(shutdownCtx)
	}()

	log.Printf("HTTP transport listening on %s", addr) //nolint:gosec // G706: addr is operator-supplied config, not user input
	if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("HTTP server: %w", err)
	}
	return nil
}
