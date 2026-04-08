// Package tools implements MCP tool handlers for the Talos MCP server.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"log/slog"
	"strings"
	"sync/atomic"
	"time"

	"github.com/cosi-project/runtime/pkg/resource"
	"github.com/cosi-project/runtime/pkg/resource/meta"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	yaml "go.yaml.in/yaml/v4"

	"github.com/Nosmoht/talos-mcp-server/internal/talos"
)

// Handlers holds the Talos client and exposes MCP tool handler methods.
type Handlers struct {
	Client       *talos.Client
	AllowedNodes *talos.NodeAllowlist
	// logger is the active slog.Logger for MCP log notifications.
	// It is swapped atomically per session in stdio mode.
	logger atomic.Pointer[slog.Logger]
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

// jsonMarshal marshals v to indented JSON string.
func jsonMarshal(v any) (string, error) {
	out, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal JSON: %w", err)
	}

	return string(out), nil
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

	log.Printf("AUDIT timestamp=%s tool=%s nodes=%s args=%s",
		time.Now().UTC().Format(time.RFC3339),
		tool,
		nodeList,
		argsJSON,
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
	log.Printf("ERROR tool=%s error=%v", tool, err)

	if l := h.logger.Load(); l != nil {
		l.Error("tool error", "tool", tool, "error", err.Error())
	}
}

// mcpLogWarn forwards a warning to the MCP client at WARN level.
// Takes an explicit msg parameter (unlike mcpLogError) because warnings
// carry additional context alongside the underlying error.
// Best-effort — silently dropped if no logger is set or delivery fails.
func (h *Handlers) mcpLogWarn(tool string, msg string, err error) {
	log.Printf("WARN tool=%s msg=%s error=%v", tool, msg, err)

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

// MarshalResource converts a COSI resource to a JSON-serializable map.
// It uses the same path as talosctl get --output json:
//
//	resource.MarshalYAML → yaml.Marshal → yaml.Unmarshal → map[string]any
func MarshalResource(r resource.Resource) (map[string]any, error) {
	out, err := resource.MarshalYAML(r)
	if err != nil {
		return nil, err
	}

	yamlBytes, err := yaml.Marshal(out)
	if err != nil {
		return nil, err
	}

	var data map[string]any

	if err = yaml.Unmarshal(yamlBytes, &data); err != nil {
		return nil, err
	}

	return data, nil
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
		log.Printf("progress notification: %v", err)
	}
}

// MarshalResourceDefinition converts a ResourceDefinition to a compact summary map.
func MarshalResourceDefinition(rd *meta.ResourceDefinition) map[string]any {
	spec := rd.TypedSpec()

	return map[string]any{
		"type":              spec.Type,
		"display_type":      spec.DisplayType,
		"default_namespace": spec.DefaultNamespace,
		"aliases":           spec.Aliases,
		"printColumns":      spec.PrintColumns,
	}
}
