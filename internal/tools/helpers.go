// Package tools implements MCP tool handlers for the Talos MCP server.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	talosclient "github.com/siderolabs/talos/pkg/machinery/client"

	"github.com/Nosmoht/talos-mcp-server/internal/talos"
)

// Handlers holds the Talos client and exposes MCP tool handler methods.
type Handlers struct {
	Client           talos.ClientInterface
	AllowedNodes     *talos.NodeAllowlist
	AllowedPaths     []string // set once at startup; read-only afterward
	SkipVersionCheck bool     // set once at startup; read-only afterward
	// BlockedConfigPaths is the list of dot-separated config path prefixes that
	// talos_patch_config refuses to modify. Empty means no restriction.
	// Set once at startup; read-only afterward.
	BlockedConfigPaths []string
	// EnableInsecure unlocks maintenance-mode operations (insecure=true) on
	// tools that accept the flag. Bypasses mTLS — every insecure call also
	// requires an entry in InsecureAllowedNodes. Set once at startup.
	EnableInsecure bool
	// InsecureAllowedNodes restricts the set of endpoints that maintenance-mode
	// tool calls may target. main.go enforces a permissiveness ceiling (no
	// 0.0.0.0/0, no /<16 IPv4, no /<48 IPv6) before constructing the allowlist.
	// Nil only when EnableInsecure is false. Set once at startup.
	InsecureAllowedNodes *talos.NodeAllowlist
	// MetaPrivilegedKeys is the set of META keys (beyond UserReserved1/2/3)
	// that talos_meta is allowed to write or delete. Empty means only the
	// reserved keys are writable. Set once at startup.
	MetaPrivilegedKeys map[uint8]struct{}
	// patchMu serialises concurrent HandlePatchConfig calls on a per-node basis.
	// In HTTP multi-session mode two agents could otherwise both fetch the current
	// config, each merge their own patch, and the second apply would silently
	// overwrite the first (read-modify-write race). The map is populated lazily
	// on first use and is bounded by the number of distinct patch targets.
	patchMu sync.Map // map[string]*sync.Mutex
	// logger is the active slog.Logger for MCP log notifications.
	// It is swapped atomically per session in stdio mode.
	logger atomic.Pointer[slog.Logger]
}

// nodePatchMu returns (or lazily creates) the per-node mutex that serialises
// concurrent config-patch operations. key is the resolved node identifier or
// "<default>" when no explicit node was provided.
func (h *Handlers) nodePatchMu(key string) *sync.Mutex {
	mu, _ := h.patchMu.LoadOrStore(key, &sync.Mutex{})
	return mu.(*sync.Mutex) //nolint:forcetypeassert // only *sync.Mutex is ever stored
}

// SetLogger replaces the active logger used for MCP log notifications.
// In stdio mode this is called once per session from InitializedHandler.
func (h *Handlers) SetLogger(l *slog.Logger) {
	h.logger.Store(l)
}

// defaultToolTimeout is the maximum time a read-only tool call may take before
// being cancelled. This prevents indefinite hangs when a Talos node is
// unresponsive. Tools with their own timeout logic (HandleHealth, HandleEvents)
// do not use this.
const defaultToolTimeout = 30 * time.Second

// withToolTimeout returns a context with defaultToolTimeout applied.
// The caller must defer cancel().
func withToolTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, defaultToolTimeout)
}

// NodesOnlyArgs is a common base for tools that only target nodes.
type NodesOnlyArgs struct {
	Nodes []string `json:"nodes,omitempty" jsonschema:"Target node IPs or hostnames. Omit to use the default nodes from talosconfig."`
}

// textResult constructs a simple text MCP CallToolResult.
func textResult(text string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: text},
		},
	}
}

// jsonResult returns v as the structured output of the tool call.
// The go-sdk auto-populates StructuredContent from the second return value and
// renders a JSON-text Content fallback for clients that don't consume
// structured output. See go-sdk mcp.CallToolResult.StructuredContent.
func jsonResult(v any) (*mcp.CallToolResult, any, error) {
	return nil, v, nil
}

// jsonWithTextResult returns v as structured output and humanText as a
// human-readable Content block. Use for tools whose output is fundamentally
// prose (logs, file contents) but benefits from a machine-readable schema.
// The MCP spec's dual-content pattern:
// https://modelcontextprotocol.io/specification/draft/server/tools#structured-content.
func jsonWithTextResult(v any, humanText string) (*mcp.CallToolResult, any, error) {
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: humanText}},
	}, v, nil
}

// mustDeriveSchema derives a JSON schema for type T or panics. Intended for
// package-level var initialisation; the output_schema_test.go ensures every
// accessor resolves in CI so panics cannot reach production.
func mustDeriveSchema[T any]() *jsonschema.Schema {
	s, err := jsonschema.For[T](nil)
	if err != nil {
		panic(fmt.Sprintf("derive schema for %T: %v", *new(T), err))
	}
	return s
}

// permissiveObjectSchema returns a schema that accepts any JSON object.
// Used for tools whose output shape is dictated by upstream Talos protobufs
// (whose reflective schema is noisy) or is genuinely polymorphic.
func permissiveObjectSchema() *jsonschema.Schema {
	return &jsonschema.Schema{
		Type:                 "object",
		AdditionalProperties: &jsonschema.Schema{},
	}
}

// auditLog emits a structured audit record at INFO level.
// Always writes to the server-side log; additionally forwards to the MCP
// client via notifications/message when a session logger is installed.
// MCP delivery is best-effort — errors are silently dropped per slog contract.
func (h *Handlers) auditLog(tool string, args any, nodes []string) {
	argsJSON, err := json.Marshal(args)
	if err != nil {
		argsJSON = []byte("<marshal error>")
	}

	nodeList := strings.Join(nodes, ",")
	if nodeList == "" {
		nodeList = "<default>"
	}

	slog.Default().Info("AUDIT",
		"tool", tool,
		"nodes", nodeList,
		"args", string(argsJSON),
	)

	if l := h.logger.Load(); l != nil {
		l.Info("tool invoked",
			"tool", tool,
			"nodes", nodeList,
			"args", string(argsJSON),
		)
	}
}

// mcpLogError forwards an operational error to the MCP client at ERROR level.
// Only called after guard checks pass (not for validation errors).
// Best-effort — silently dropped if no logger is set or delivery fails.
func (h *Handlers) mcpLogError(tool string, err error) {
	slog.Default().Error("tool error", "tool", tool, "error", err)

	if l := h.logger.Load(); l != nil {
		l.Error("tool error", "tool", tool, "error", err.Error())
	}
}

// mcpLogWarn forwards a warning to the MCP client at WARN level.
// Takes an explicit msg parameter (unlike mcpLogError) because warnings
// carry additional context alongside the underlying error.
// Best-effort — silently dropped if no logger is set or delivery fails.
func (h *Handlers) mcpLogWarn(tool string, msg string, err error) {
	slog.Default().Warn("tool warning", "tool", tool, "msg", msg, "error", err)

	if l := h.logger.Load(); l != nil {
		l.Warn(msg, "tool", tool, "error", err.Error())
	}
}

// resolveDryRun returns true (dry-run mode) unless v is explicitly set to false.
// A nil pointer means the caller did not provide the field, so we default to safe (dry-run).
func resolveDryRun(v *bool) bool {
	return v == nil || *v
}

// resolvePreserve returns true (preserve EPHEMERAL partition) unless v is explicitly set to false.
// Defaults to true — diverges from talosctl (which defaults to false) — because AI agents that
// omit the field should not accidentally wipe user data.
func resolvePreserve(v *bool) bool {
	return v == nil || *v
}

// resolveGraceful returns true (stop services gracefully) unless v is explicitly set to false.
// Defaults to true — AI agents that omit the field should not skip graceful drain.
func resolveGraceful(v *bool) bool {
	return v == nil || *v
}

// Outcome values for auditOutcome — every insecure-mode handler exit path
// records exactly one of these strings, paired with the auditLog entry that
// was emitted at handler entry. Defenders can join (audit-line, outcome-line)
// by tool + nodes + a per-call correlation field.
const (
	OutcomeOK                     = "ok"
	OutcomeRefusedEnable          = "refused:enable-insecure-unset"
	OutcomeRefusedConfirm         = "refused:confirm-required"
	OutcomeRefusedNodesExclusive  = "refused:nodes-and-endpoint-exclusive"
	OutcomeRefusedFPWithoutInsec  = "refused:fingerprint-requires-insecure"
	OutcomeRefusedEndpointEmpty   = "refused:endpoint-empty"
	OutcomeRefusedEndpointInvalid = "refused:endpoint-not-canonical-ip"
	OutcomeRefusedAllowlist       = "refused:endpoint-not-in-allowlist"
	OutcomeRefusedFingerprint     = "refused:fingerprint-invalid"
	OutcomeRefusedMetaKey         = "refused:meta-key-not-privileged"
	OutcomeDialError              = "dial-error"
	OutcomeRPCError               = "rpc-error"
	OutcomeRefusedArgs            = "refused:args-invalid"
)

// auditOutcome emits a paired log line for every auditLog call, recording
// the final outcome (success, refused-at-gate, dial-error, rpc-error) so an
// IR responder can distinguish reconnaissance from action. Callers use:
//
//	defer func() { h.auditOutcome("talos_apply_config", outcome, returnErr) }()
//
// outcome is set by the handler to one of the Outcome* constants above
// before each return.
func (h *Handlers) auditOutcome(tool, outcome string, err error) {
	errStr := ""
	if err != nil {
		errStr = err.Error()
	}
	slog.Default().Info("AUDIT_OUTCOME",
		"tool", tool,
		"outcome", outcome,
		"error", errStr,
	)
	if l := h.logger.Load(); l != nil {
		l.Info("tool outcome",
			"tool", tool,
			"outcome", outcome,
			"error", errStr,
		)
	}
}

// canonicalizeAndCheckInsecure validates the common gating for any
// maintenance-mode tool call. It returns the canonical endpoint string used
// uniformly for allowlist match, lock-key derivation, and gRPC dial; the
// decoded fingerprint bytes (nil when no fingerprint was supplied); and the
// outcome code identifying which gate failed (empty when all gates pass).
//
// The validation order is deliberate: consistency checks before format checks
// (so an LLM that sets cert_fingerprint without insecure=true gets the
// semantically-correct error, not a hex-format error).
func (h *Handlers) canonicalizeAndCheckInsecure(endpoint, certFingerprint string, nodesProvided bool) (canonicalEndpoint string, fp []byte, outcome string, err error) {
	if !h.EnableInsecure {
		return "", nil, OutcomeRefusedEnable, fmt.Errorf("insecure mode refused: TALOS_MCP_ENABLE_INSECURE must be set to true at server startup")
	}
	if nodesProvided {
		return "", nil, OutcomeRefusedNodesExclusive, fmt.Errorf("insecure mode refused: nodes[] and endpoint are mutually exclusive — use endpoint only when insecure=true")
	}
	if endpoint == "" {
		return "", nil, OutcomeRefusedEndpointEmpty, fmt.Errorf("insecure mode refused: endpoint is required when insecure=true")
	}
	canon, err := talos.CanonicalIP(endpoint)
	if err != nil {
		return "", nil, OutcomeRefusedEndpointInvalid, fmt.Errorf("insecure mode refused: %w", err)
	}
	if h.InsecureAllowedNodes != nil {
		if err := h.InsecureAllowedNodes.CheckNode(canon); err != nil {
			return "", nil, OutcomeRefusedAllowlist, fmt.Errorf("insecure mode refused: endpoint %q is not in TALOS_MCP_INSECURE_ALLOWED_NODES", canon)
		}
	}
	if certFingerprint != "" {
		fpBytes, err := talos.ParseFingerprint(certFingerprint)
		if err != nil {
			return "", nil, OutcomeRefusedFingerprint, fmt.Errorf("insecure mode refused: %w", err)
		}
		fp = fpBytes
	}
	return canon, fp, "", nil
}

// dialInsecure builds a one-shot maintenance-mode client. Errors are returned
// to the caller verbatim; the caller is responsible for emitting auditOutcome
// with OutcomeDialError on failure.
//
// The caller MUST Close() the returned client when done — the per-call factory
// shape is intentional. Maintenance-mode endpoints are short-lived and each
// call targets a different fresh node IP, so a shared pool would buy nothing.
func (h *Handlers) dialInsecure(ctx context.Context, canonicalEndpoint string, fp []byte) (*talosclient.Client, error) {
	return talos.NewInsecureClient(ctx, canonicalEndpoint, fp)
}

// notifyProgress sends a progress notification to the client if the request
// carries a progress token. It is a no-op when req is nil or when no token is
// present, so callers do not need to guard every call site.
func notifyProgress(ctx context.Context, req *mcp.CallToolRequest, message string, progress, total float64) {
	if req == nil {
		return
	}
	token := req.Params.GetProgressToken()
	if token == nil {
		return
	}
	if err := req.Session.NotifyProgress(ctx, &mcp.ProgressNotificationParams{
		ProgressToken: token,
		Message:       message,
		Progress:      progress,
		Total:         total,
	}); err != nil {
		slog.Default().Warn("progress notification failed", "error", err)
	}
}
