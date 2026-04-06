package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/cosi-project/runtime/pkg/resource"
	"github.com/cosi-project/runtime/pkg/resource/meta"
	"github.com/cosi-project/runtime/pkg/safe"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// GetResourceArgs defines input for talos_get.
type GetResourceArgs struct {
	ResourceType string   `json:"resource_type" jsonschema:"Talos resource type, e.g. MachineStatus\\, Member\\, LinkStatus. Use talos_resource_definitions to discover all types."`
	ResourceID   string   `json:"resource_id,omitempty" jsonschema:"Optional specific resource ID. Omit to list all resources of this type."`
	Namespace    string   `json:"namespace,omitempty" jsonschema:"Resource namespace. Omit to use the default namespace for the resource type."`
	Nodes        []string `json:"nodes,omitempty" jsonschema:"Target node IPs or hostnames. Omit to use the default nodes from talosconfig."`
}

// handleGetResource implements the talos_get tool.
func (tc *TalosClient) handleGetResource(ctx context.Context, _ *mcp.CallToolRequest, args GetResourceArgs) (*mcp.CallToolResult, any, error) {
	ctx = withNodes(ctx, args.Nodes)

	ns := resource.Namespace(args.Namespace)

	// Resolve the resource type (handles aliases like "ms" → "MachineStatus")
	rd, err := tc.client.ResolveResourceKind(ctx, &ns, args.ResourceType)
	if err != nil {
		return nil, nil, fmt.Errorf("resolve resource kind %q: %w", args.ResourceType, err)
	}

	resourceType := rd.TypedSpec().Type

	var results []map[string]any

	if args.ResourceID != "" {
		// Get a single specific resource
		r, err := tc.client.COSI.Get(ctx,
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
		list, err := tc.client.COSI.List(ctx,
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

// handleResourceDefinitions implements the talos_resource_definitions tool.
func (tc *TalosClient) handleResourceDefinitions(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
	list, err := safe.StateListAll[*meta.ResourceDefinition](ctx, tc.client.COSI)
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
