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
//   - TALOS_MCP_ALLOW_CLUSTER_WIDE: set to "true" to register cluster-wide
//     tools (reserved for Phase D; no tools consume the flag yet)
//   - TALOS_MCP_ENABLE_GEN: set to "true" to register offline gen_* tools
//     (reserved for Phase E; no tools consume the flag yet)
//   - TALOS_MCP_SAFETY_PROFILE: conservative|standard|expert preset that seeds
//     TALOS_MCP_READ_ONLY, TALOS_MCP_ALLOW_CLUSTER_WIDE, TALOS_MCP_ENABLE_GEN,
//     TALOS_MCP_SKIP_VERSION_CHECK and TALOS_MCP_ENABLE_INSECURE. Individual
//     flags override the profile. When unset, each flag defaults to its own
//     env var (backwards-compatible).
//   - TALOS_MCP_HTTP_ADDR: if set (e.g. ":8080"), serve HTTP instead of stdio
//   - TALOS_MCP_AUTH_TOKEN: required bearer token when HTTP mode is active
//   - TALOS_MCP_ALLOWED_NODES: comma-separated list of permitted node IPs, hostnames,
//     and CIDR ranges (e.g. "10.0.0.1,10.0.0.2" or "10.0.0.0/24"). When set, any tool
//     call targeting a node not in this list is rejected. Unset or empty allows all nodes.
//   - TALOS_MCP_ALLOWED_PATHS: comma-separated path prefixes allowed for talos_read_file
//     and talos_list_files (e.g. "/etc,/proc"). Unset or empty allows all paths.
//   - TALOS_MCP_BLOCKED_CONFIG_PATHS: comma-separated dot-path prefixes that
//     talos_patch_config refuses to modify (e.g. "machine.security,cluster.etcd").
//     Unset or empty disables the blocklist (all paths allowed).
//   - TALOS_MCP_SKIP_VERSION_CHECK: set to "true" to bypass upgrade path validation
//   - TALOS_MCP_ENABLE_INSECURE: set to "true" to unlock maintenance-mode
//     operations (insecure=true on talos_apply_config / talos_get / talos_version
//     and the talos_meta tool). Bypasses mTLS — the transport is TLS-encrypted
//     but the client presents no cert and (by default) does not verify the server.
//     MUST be paired with TALOS_MCP_INSECURE_ALLOWED_NODES; main.go log.Fatalf-s
//     if the allowlist is unset or over-permissive.
//   - TALOS_MCP_INSECURE_ALLOWED_NODES: comma-separated list of permitted
//     maintenance-mode endpoint IPs / hostnames / CIDR ranges. Required when
//     TALOS_MCP_ENABLE_INSECURE=true. Hard refused: 0.0.0.0/0, ::/0, IPv4 mask
//     <16, IPv6 mask <48. Broad-but-accepted (IPv4 <24, IPv6 <64) emits a warning.
//   - TALOS_MCP_META_PRIVILEGED_KEYS: comma-separated META keys (decimal or
//     0x-prefixed hex; bare leading zeros rejected) to unlock for talos_meta
//     write/delete actions. UserReserved1/2/3 are always permitted; this var
//     only widens the safelist for privileged keys like StateEncryptionConfig.
//     Empty default = no privileged keys.
//   - TALOS_MCP_RATE_LIMIT: HTTP mode requests/second (float, default 10)
//   - TALOS_MCP_RATE_BURST: HTTP mode burst capacity (int, default 20)
//   - TALOS_MCP_MAX_BODY_SIZE: HTTP mode max POST body bytes (int, default 4194304)
//   - TALOS_MCP_MAX_CONCURRENT: HTTP mode max concurrent POST handlers (int, default 20)
//   - TALOS_MCP_SUBSCRIPTION_RATE: minimum interval between delivered
//     resources/updated notifications per (session, URI) (duration, default 1s)
//   - TALOS_MCP_SUBSCRIPTION_BURST: initial notification burst per (session, URI)
//     (int, default 3)
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
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Nosmoht/talos-mcp-server/internal/completions"
	"github.com/Nosmoht/talos-mcp-server/internal/config"
	"github.com/Nosmoht/talos-mcp-server/internal/prompts"
	"github.com/Nosmoht/talos-mcp-server/internal/resources"
	"github.com/Nosmoht/talos-mcp-server/internal/subscriptions"
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

	cfg, err := loadStartupConfig()
	if err != nil {
		stop()
		log.Fatalf("%v", err) //nolint:gocritic // exitAfterDefer: stop() called explicitly above
	}

	tc, err := talos.NewClient(ctx)
	if err != nil {
		stop()
		log.Fatalf("failed to create Talos client: %v", err) //nolint:gocritic // exitAfterDefer: stop() called explicitly above
	}
	defer tc.Close() //nolint:errcheck

	slog.Info("talos-mcp started", "version", version, "commit", commit, "date", date, "read_only", cfg.safety.ReadOnly) //nolint:gosec // G706 false positive: version/commit/date are build-time ldflags constants injected by GoReleaser, not runtime user input
	slog.Info("safety profile", cfg.safety.LogFields()...)
	logClusterVersion(ctx, tc)
	cfg.logGuards()

	h := &tools.Handlers{
		Client:               tc,
		AllowedNodes:         cfg.allowedNodes,
		AllowedPaths:         cfg.allowedPaths,
		SkipVersionCheck:     cfg.safety.SkipVersionCheck,
		BlockedConfigPaths:   cfg.blockedConfigPaths,
		EnableInsecure:       cfg.safety.EnableInsecure,
		InsecureAllowedNodes: cfg.insecureAllowedNodes,
		MetaPrivilegedKeys:   cfg.metaPrivilegedKeys,
	}

	subRate, subBurst, err := parseSubscriptionLimits(os.Getenv("TALOS_MCP_SUBSCRIPTION_RATE"), os.Getenv("TALOS_MCP_SUBSCRIPTION_BURST"))
	if err != nil {
		stop()
		log.Fatalf("%v", err) //nolint:gocritic // exitAfterDefer: stop() called explicitly above
	}
	server, subMgr := newServer(h, serverConfig{
		ReadOnly:           cfg.safety.ReadOnly,
		SubscriptionRate:   subRate,
		SubscriptionBurst:  subBurst,
		ResourceClient:     tc,
		SubscriptionSource: tc,
	})
	defer subMgr.Shutdown()

	if err := runServer(ctx, server, cfg.httpAddr, cfg.authToken, newHTTPTransportConfig(), tc.Ping); err != nil {
		slog.Error("server stopped with error", "error", err)
	}
}

type startupConfig struct {
	safety               *config.SafetyProfile
	httpAddr             string
	authToken            string
	allowedNodes         *talos.NodeAllowlist
	allowedPaths         []string
	blockedConfigPaths   []string
	insecureAllowedNodes *talos.NodeAllowlist
	insecureBroadEntries []string
	metaPrivilegedKeys   map[uint8]struct{}
}

func loadStartupConfig() (*startupConfig, error) {
	safety, err := config.LoadSafetyProfile()
	if err != nil {
		return nil, err
	}

	cfg := &startupConfig{
		safety:    safety,
		httpAddr:  os.Getenv("TALOS_MCP_HTTP_ADDR"),
		authToken: os.Getenv("TALOS_MCP_AUTH_TOKEN"),
	}
	os.Unsetenv("TALOS_MCP_AUTH_TOKEN") //nolint:errcheck // remove token from /proc/<pid>/environ

	if cfg.allowedNodes, err = talos.ParseNodeAllowlist(os.Getenv("TALOS_MCP_ALLOWED_NODES")); err != nil {
		return nil, fmt.Errorf("invalid TALOS_MCP_ALLOWED_NODES: %w", err)
	}

	cfg.allowedPaths = tools.ParseAllowedPaths(os.Getenv("TALOS_MCP_ALLOWED_PATHS"))
	cfg.blockedConfigPaths = splitNonEmpty(os.Getenv("TALOS_MCP_BLOCKED_CONFIG_PATHS"), ",")

	if safety.EnableInsecure {
		if cfg.insecureAllowedNodes, cfg.insecureBroadEntries, err = loadInsecureAllowlist(); err != nil {
			return nil, err
		}
	}

	if cfg.metaPrivilegedKeys, err = talos.ParseMetaPrivilegedKeys(os.Getenv("TALOS_MCP_META_PRIVILEGED_KEYS")); err != nil {
		return nil, err
	}

	if err := validateHTTPConfig(cfg.httpAddr, cfg.authToken); err != nil {
		return nil, err
	}

	return cfg, nil
}

func loadInsecureAllowlist() (*talos.NodeAllowlist, []string, error) {
	rawAllowlist := os.Getenv("TALOS_MCP_INSECURE_ALLOWED_NODES")

	inspection, err := config.CheckInsecureAllowlist(rawAllowlist)
	if err != nil {
		return nil, nil, err
	}

	allowlist, err := talos.ParseNodeAllowlist(rawAllowlist)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid TALOS_MCP_INSECURE_ALLOWED_NODES: %w", err)
	}

	if allowlist == nil || allowlist.Len() == 0 {
		return nil, nil, errors.New("TALOS_MCP_INSECURE_ALLOWED_NODES yielded no entries: cannot enable insecure mode")
	}

	return allowlist, inspection.BroadEntries, nil
}

func logClusterVersion(ctx context.Context, tc *talos.Client) {
	cv, err := tc.GetClusterVersion(ctx)
	switch {
	case err != nil:
		slog.Warn("could not detect cluster Talos version", "error", err)
	case !cv.InSupportedRange():
		slog.Warn("cluster Talos version is outside the tested range; some features may not work correctly",
			"version", cv.String(),
			"min_supported", talosversion.MinSupported.String(),
			"max_tested", talosversion.MaxTested.String(),
		)
	default:
		slog.Info("cluster Talos version", "version", cv.String(), "status", "supported")
	}
}

func (cfg *startupConfig) logGuards() {
	if cfg.allowedNodes != nil {
		slog.Info("node allowlist active", "entries", cfg.allowedNodes.Len()) //nolint:gosec // G706: entry count is an integer, not user-controlled string input
	} else {
		slog.Info("node allowlist disabled")
	}

	if len(cfg.allowedPaths) > 0 {
		slog.Info("path allowlist active", "entries", len(cfg.allowedPaths)) //nolint:gosec // G706: entry count is an integer, not user-controlled string input
		slog.Warn("path allowlist is defense-in-depth only: the prefix check runs on the MCP server host and does not resolve symlinks on the remote node, so a symlink under an allowed prefix that points elsewhere is not detected")
	} else {
		slog.Info("path allowlist disabled")
	}

	if cfg.safety.SkipVersionCheck {
		slog.Warn("version check disabled (TALOS_MCP_SKIP_VERSION_CHECK=true)")
	}

	if len(cfg.blockedConfigPaths) > 0 {
		slog.Info("config path blocklist active", "entries", len(cfg.blockedConfigPaths)) //nolint:gosec // G706: entry count is an integer, not user-controlled string input
	} else {
		slog.Info("config path blocklist disabled")
	}

	cfg.logInsecureMode()

	if len(cfg.metaPrivilegedKeys) > 0 {
		//nolint:gosec // G706: message is a constant; field is an integer count, not user input
		slog.Warn("META privileged-key safelist active — these keys are writable beyond UserReserved1/2/3",
			"key_count", len(cfg.metaPrivilegedKeys),
		)
	}
}

func (cfg *startupConfig) logInsecureMode() {
	if !cfg.safety.EnableInsecure {
		slog.Info("maintenance-mode (insecure) operations disabled")
		return
	}

	//nolint:gosec // G706: message is a constant; fields are integer count and operator-supplied CIDR, not user input
	slog.Warn("maintenance-mode (insecure) operations ENABLED — bypasses mTLS; ensure the allowlist is tight",
		"allowlist_entries", cfg.insecureAllowedNodes.Len(),
	)
	for _, broad := range cfg.insecureBroadEntries {
		//nolint:gosec // G706: message is a constant; cidr is operator-supplied config, not user input
		slog.Warn("insecure allowlist entry is broader than recommended", "cidr", broad)
	}
}

// serverConfig carries the inputs of newServer that do not live on tools.Handlers.
type serverConfig struct {
	ReadOnly           bool
	SubscriptionRate   time.Duration
	SubscriptionBurst  int
	ResourceClient     resources.Client
	SubscriptionSource subscriptions.Source
}

// newServer builds the MCP server with all registrations and the subscription
// manager. Production and tests share it, so fixtures cannot drift from the
// production wiring — same drift class as newStreamableHTTPOptions.
func newServer(h *tools.Handlers, cfg serverConfig) (*mcp.Server, *subscriptions.Manager) {
	subMgr := subscriptions.NewManager(cfg.SubscriptionSource, h.AllowedNodes, cfg.SubscriptionRate, cfg.SubscriptionBurst)

	serverOpts := &mcp.ServerOptions{
		Instructions: "Talos Linux cluster management. Choose a tool by what you need:\n" +
			"- cluster/node health → talos_health\n" +
			"- service state → talos_services; a service's logs → talos_logs\n" +
			"- Talos versions → talos_version\n" +
			"- etcd members or status → talos_etcd\n" +
			"- kernel messages → talos_dmesg; runtime/lifecycle events → talos_events\n" +
			"- containers → talos_containers; host processes → talos_processes\n" +
			"- files on a node → talos_list_files / talos_read_file\n" +
			"- anything else (network, MachineStatus, …) → talos_get; talos_resource_definitions lists the types\n" +
			"Each tool's description documents its guard parameters and node scope. " +
			"The talos:// MCP Resources mirror talos_get/talos_version for clients that prefer resources — the tools are the simpler path.",
		CompletionHandler:  completions.NewHandler(h.AllowedNodes),
		SubscribeHandler:   subMgr.Subscribe,
		UnsubscribeHandler: subMgr.Unsubscribe,
		// A nil Capabilities makes the SDK advertise logging; the other capabilities are still inferred from the registrations.
		Capabilities: &mcp.ServerCapabilities{},
		// Fires only for initialize-based sessions; 2026-07-28 subscriptions end through the SDK's subscriptions/listen deferred unsubscribe.
		InitializedHandler: func(_ context.Context, req *mcp.InitializedRequest) {
			go subMgr.SuperviseSession(req.Session)
		},
	}

	server := mcp.NewServer(&mcp.Implementation{
		Name:    "talos",
		Version: version,
	}, serverOpts)

	// BindServer must happen-before any session can issue resources/subscribe.
	// Sessions start only after newServer returns; binding here is sufficient.
	subMgr.BindServer(server)

	tools.Register(server, h, cfg.ReadOnly)
	resources.Register(server, cfg.ResourceClient, h.AllowedNodes)
	prompts.Register(server, cfg.ReadOnly)

	return server, subMgr
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

// buildHTTPMux constructs the HTTP request mux for HTTP transport mode:
//
//   - GET /healthz — unauthenticated liveness probe; 200 OK when gRPC is reachable, 503 otherwise
//   - /* — full middleware chain: RateLimit → LimitRequestBody → auth → LimitConcurrency → mcpHandler
//
// Rate limiting and body limiting run before auth to prevent unauthenticated floods from consuming
// auth resources. Concurrency limiting runs after auth so only authenticated requests consume slots.
// The /healthz path is intentionally outside the auth chain so orchestrators (k8s, Nomad, etc.) can
// probe liveness without a bearer token.
func buildHTTPMux(mcpHandler http.Handler, token string, hc httpTransportConfig, healthProbe func(context.Context) error) http.Handler {
	mux := http.NewServeMux()

	// /healthz is unauthenticated — no bearer token required.
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		probeCtx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		if err := healthProbe(probeCtx); err != nil {
			slog.Error("healthz probe failed", "error", err)
			http.Error(w, "unhealthy", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})

	// All other paths go through the full authenticated middleware chain.
	verifier := buildTokenVerifier(token)
	handler := LimitConcurrency(hc.sem)(mcpHandler)
	handler = auth.RequireBearerToken(verifier, nil)(handler)
	handler = LimitRequestBody(hc.maxBody)(handler)
	handler = RateLimit(hc.limiter)(handler)
	mux.Handle("/", handler)

	return mux
}

// newStreamableHTTPOptions returns the shared StreamableHTTPOptions used by runServer
// in production and by buildTestHandler / buildMuxForTest in tests. Co-locating the
// security-relevant fields keeps test fixtures from drifting away from production —
// see issue #179 and PR #177.
//
// DisableLocalhostProtection allows proxied requests whose Host header differs from
// the bind address (e.g. behind nginx/Caddy/Tailscale).
//
// Cross-origin protection is intentionally NOT set on this struct. go-sdk v1.6.0
// deprecated the StreamableHTTPOptions.CrossOriginProtection field, which v1.8.0 still
// carries; the SDK directs callers to wrap the handler with the stdlib middleware instead.
// newProtectedMCPHandler applies that wrapper. The cross-origin layer is orthogonal
// to DisableLocalhostProtection (cross-origin non-safe-method denial vs. DNS rebinding
// at the Host-header layer).
func newStreamableHTTPOptions(logger *slog.Logger) *mcp.StreamableHTTPOptions {
	return &mcp.StreamableHTTPOptions{
		DisableLocalhostProtection: true,
		Logger:                     logger,
	}
}

// newProtectedMCPHandler wires the streamable HTTP handler together with the
// stdlib cross-origin protection middleware. Consolidating both steps here
// removes drift between production (runServer) and tests (buildTestHandler,
// buildMuxForTest) — the same drift class that motivated newStreamableHTTPOptions.
// See PR #177 and issue #179 for the regression this guards against.
//
// The wrapper form replaces the StreamableHTTPOptions.CrossOriginProtection field,
// which go-sdk v1.6.0 deprecated.
func newProtectedMCPHandler(server *mcp.Server, logger *slog.Logger) http.Handler {
	mcpHandler := mcp.NewStreamableHTTPHandler(func(_ *http.Request) *mcp.Server {
		return server
	}, newStreamableHTTPOptions(logger))
	protection := http.NewCrossOriginProtection()
	return protection.Handler(mcpHandler)
}

// runServer starts the server in either stdio or HTTP mode.
// healthProbe is called on each /healthz request to verify gRPC connectivity;
// it is only used in HTTP mode and must be non-nil when addr is non-empty.
func runServer(ctx context.Context, server *mcp.Server, addr, token string, hc httpTransportConfig, healthProbe func(context.Context) error) error {
	if addr == "" {
		// stdio mode — unchanged behaviour
		return server.Run(ctx, &mcp.StdioTransport{})
	}

	protectedHandler := newProtectedMCPHandler(server, slog.Default())

	httpSrv := &http.Server{
		Addr:              addr,
		Handler:           buildHTTPMux(protectedHandler, token, hc, healthProbe),
		ReadHeaderTimeout: 30 * time.Second,
	}

	// Shutdown on context cancellation.
	go func() { //nolint:gosec // G118: intentional — shutdown uses a fresh background context, not the cancelled one
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second) //nolint:contextcheck
		defer cancel()
		_ = httpSrv.Shutdown(shutdownCtx)
	}()

	slog.Warn("DisableLocalhostProtection is active — DNS rebinding protection disabled; ensure a reverse proxy with Origin header preservation is in front of this server")
	slog.Info("HTTP transport listening", "addr", addr) //nolint:gosec // G706: addr is operator-supplied config, not user input
	if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("HTTP server: %w", err)
	}
	return nil
}

func parseSubscriptionLimits(rateStr, burstStr string) (time.Duration, int, error) {
	rate := time.Second
	if rateStr != "" {
		d, err := time.ParseDuration(rateStr)
		if err != nil {
			return 0, 0, fmt.Errorf("invalid TALOS_MCP_SUBSCRIPTION_RATE: %w", err)
		}
		if d <= 0 {
			return 0, 0, fmt.Errorf("TALOS_MCP_SUBSCRIPTION_RATE must be > 0, got %s", rateStr)
		}
		rate = d
	}
	burst := 3
	if burstStr != "" {
		n, err := strconv.Atoi(burstStr)
		if err != nil {
			return 0, 0, fmt.Errorf("invalid TALOS_MCP_SUBSCRIPTION_BURST: %w", err)
		}
		if n <= 0 {
			return 0, 0, fmt.Errorf("TALOS_MCP_SUBSCRIPTION_BURST must be > 0, got %d", n)
		}
		burst = n
	}
	return rate, burst, nil
}

func splitNonEmpty(s, sep string) []string {
	if s == "" {
		return nil
	}
	var out []string
	for _, p := range strings.Split(s, sep) {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}
