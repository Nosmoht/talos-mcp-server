package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/cosi-project/runtime/pkg/resource"
	"github.com/cosi-project/runtime/pkg/resource/meta"
	"github.com/cosi-project/runtime/pkg/safe"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Nosmoht/talos-mcp-server/internal/talos"
)

// GetResourceArgs defines input for talos_get.
type GetResourceArgs struct {
	ResourceType string   `json:"resource_type" jsonschema:"Talos resource type, e.g. MachineStatus\\, Member\\, LinkStatus. Use talos_resource_definitions to discover all types."`
	ResourceID   string   `json:"resource_id,omitempty" jsonschema:"Optional specific resource ID. Omit to list all resources of this type."`
	Namespace    string   `json:"namespace,omitempty" jsonschema:"Resource namespace. Omit to use the default namespace for the resource type."`
	Nodes        []string `json:"nodes,omitempty" jsonschema:"Target node IPs or hostnames. Omit to use the default nodes from talosconfig."`
}

// HandleGetResource implements the talos_get tool.
func (h *Handlers) HandleGetResource(ctx context.Context, _ *mcp.CallToolRequest, args GetResourceArgs) (*mcp.CallToolResult, any, error) {
	ctx = talos.WithNodes(ctx, args.Nodes)

	ns := resource.Namespace(args.Namespace)

	// Resolve the resource type (handles aliases like "ms" → "MachineStatus")
	rd, err := h.Client.ResolveResourceKind(ctx, &ns, args.ResourceType)
	if err != nil {
		return nil, nil, fmt.Errorf("resolve resource kind %q: %w", args.ResourceType, err)
	}

	resourceType := rd.TypedSpec().Type

	var results []map[string]any

	if args.ResourceID != "" {
		// Get a single specific resource
		r, err := h.Client.COSI.Get(ctx,
			resource.NewMetadata(ns, resourceType, args.ResourceID, resource.VersionUndefined),
		)
		if err != nil {
			return nil, nil, fmt.Errorf("get resource %s/%s/%s: %w", ns, resourceType, args.ResourceID, err)
		}

		data, err := marshalResource(r)
		if err != nil {
			return nil, nil, fmt.Errorf("marshal resource: %w", err)
		}

		results = []map[string]any{data}
	} else {
		// List all resources of this type
		list, err := h.Client.COSI.List(ctx,
			resource.NewMetadata(ns, resourceType, "", resource.VersionUndefined),
		)
		if err != nil {
			return nil, nil, fmt.Errorf("list resources %s/%s: %w", ns, resourceType, err)
		}

		for _, r := range list.Items {
			data, err := marshalResource(r)
			if err != nil {
				return nil, nil, fmt.Errorf("marshal resource: %w", err)
			}

			results = append(results, data)
		}
	}

	out, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		return nil, nil, fmt.Errorf("marshal JSON: %w", err)
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: string(out)},
		},
	}, nil, nil
}

// HandleResourceDefinitions implements the talos_resource_definitions tool.
func (h *Handlers) HandleResourceDefinitions(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
	list, err := safe.StateListAll[*meta.ResourceDefinition](ctx, h.Client.COSI)
	if err != nil {
		return nil, nil, fmt.Errorf("list resource definitions: %w", err)
	}

	var defs []map[string]any

	for rd := range list.All() {
		defs = append(defs, marshalResourceDefinition(rd))
	}

	out, err := json.MarshalIndent(defs, "", "  ")
	if err != nil {
		return nil, nil, fmt.Errorf("marshal JSON: %w", err)
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: string(out)},
		},
	}, nil, nil
}
