package tools

import (
	"context"
	"fmt"

	"github.com/cosi-project/runtime/pkg/resource"
	"github.com/cosi-project/runtime/pkg/resource/meta"
	"github.com/cosi-project/runtime/pkg/safe"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Nosmoht/talos-mcp-server/internal/marshal"
	"github.com/Nosmoht/talos-mcp-server/internal/talos"
)

// resourceList is the envelope returned by HandleGetResource. The MCP spec
// requires structuredContent to be a JSON object, so the polymorphic list of
// resources is wrapped in an object with a single "items" array.
type resourceList struct {
	Items []map[string]any `json:"items"`
}

// resourceDefinitionList is the envelope returned by HandleResourceDefinitions.
type resourceDefinitionList struct {
	Items []map[string]any `json:"items"`
}

// getResourceOutputSchema is hand-written rather than reflective: items are
// genuinely polymorphic (MachineStatus, NodeAddress, Route have different
// shapes). The permissive item schema matches Kubernetes' unstructured.Unstructured
// convention for heterogeneous resource lists.
var getResourceOutputSchema = &jsonschema.Schema{
	Type: "object",
	Properties: map[string]*jsonschema.Schema{
		"items": {
			Type:  "array",
			Items: &jsonschema.Schema{Type: "object", AdditionalProperties: &jsonschema.Schema{}},
		},
	},
	Required: []string{"items"},
}

// GetResourceOutputSchema returns the JSON schema for HandleGetResource.
func GetResourceOutputSchema() *jsonschema.Schema { return getResourceOutputSchema }

// resourceDefinitionsOutputSchema: same permissive shape as getResourceOutputSchema,
// since each definition's marshaled map is also built ad-hoc.
var resourceDefinitionsOutputSchema = &jsonschema.Schema{
	Type: "object",
	Properties: map[string]*jsonschema.Schema{
		"items": {
			Type:  "array",
			Items: &jsonschema.Schema{Type: "object", AdditionalProperties: &jsonschema.Schema{}},
		},
	},
	Required: []string{"items"},
}

// ResourceDefinitionsOutputSchema returns the JSON schema for HandleResourceDefinitions.
func ResourceDefinitionsOutputSchema() *jsonschema.Schema { return resourceDefinitionsOutputSchema }

// GetResourceArgs defines input for talos_get.
type GetResourceArgs struct {
	ResourceType string   `json:"resource_type" jsonschema:"Talos resource type, e.g. MachineStatus\\, Member\\, LinkStatus. Use talos_resource_definitions to discover all types."`
	ResourceID   string   `json:"resource_id,omitempty" jsonschema:"Optional specific resource ID. Omit to list all resources of this type."`
	Namespace    string   `json:"namespace,omitempty" jsonschema:"Resource namespace. Omit to use the default namespace for the resource type."`
	Nodes        []string `json:"nodes,omitempty" jsonschema:"Target node IPs or hostnames. Omit to use the default nodes from talosconfig."`
}

// HandleGetResource implements the talos_get tool.
func (h *Handlers) HandleGetResource(ctx context.Context, _ *mcp.CallToolRequest, args GetResourceArgs) (*mcp.CallToolResult, any, error) {
	ctx, cancel := withToolTimeout(ctx)
	defer cancel()

	ctx, err := talos.WithNodes(ctx, args.Nodes, h.AllowedNodes)
	if err != nil {
		return nil, nil, err
	}

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
		r, err := h.Client.COSIState().Get(ctx,
			resource.NewMetadata(ns, resourceType, args.ResourceID, resource.VersionUndefined),
		)
		if err != nil {
			return nil, nil, fmt.Errorf("get resource %s/%s/%s: %w", ns, resourceType, args.ResourceID, err)
		}

		data, err := marshal.Resource(r)
		if err != nil {
			return nil, nil, fmt.Errorf("marshal resource: %w", err)
		}

		results = []map[string]any{data}
	} else {
		// List all resources of this type
		list, err := h.Client.COSIState().List(ctx,
			resource.NewMetadata(ns, resourceType, "", resource.VersionUndefined),
		)
		if err != nil {
			return nil, nil, fmt.Errorf("list resources %s/%s: %w", ns, resourceType, err)
		}

		for _, r := range list.Items {
			data, err := marshal.Resource(r)
			if err != nil {
				return nil, nil, fmt.Errorf("marshal resource: %w", err)
			}

			results = append(results, data)
		}
	}

	if results == nil {
		results = []map[string]any{}
	}

	return jsonResult(resourceList{Items: results})
}

// HandleResourceDefinitions implements the talos_resource_definitions tool.
func (h *Handlers) HandleResourceDefinitions(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
	ctx, cancel := withToolTimeout(ctx)
	defer cancel()

	list, err := safe.StateListAll[*meta.ResourceDefinition](ctx, h.Client.COSIState())
	if err != nil {
		return nil, nil, fmt.Errorf("list resource definitions: %w", err)
	}

	defs := []map[string]any{}

	for rd := range list.All() {
		defs = append(defs, marshal.ResourceDefinition(rd))
	}

	return jsonResult(resourceDefinitionList{Items: defs})
}
