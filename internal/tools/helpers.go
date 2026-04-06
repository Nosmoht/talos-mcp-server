// Package tools implements MCP tool handlers for the Talos MCP server.
package tools

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/cosi-project/runtime/pkg/resource"
	"github.com/cosi-project/runtime/pkg/resource/meta"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	yaml "go.yaml.in/yaml/v4"

	"github.com/Nosmoht/talos-mcp-server/internal/talos"
)

// Handlers holds the Talos client and exposes MCP tool handler methods.
type Handlers struct {
	Client *talos.Client
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

// auditLog emits a structured audit log entry for mutating tool invocations.
// Output format: AUDIT timestamp=<RFC3339> tool=<name> args=<json> nodes=<list>
func auditLog(tool string, args any, nodes []string) {
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
}

// resolveDryRun returns true (dry-run mode) unless v is explicitly set to false.
// A nil pointer means the caller did not provide the field, so we default to safe (dry-run).
func resolveDryRun(v *bool) bool {
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
