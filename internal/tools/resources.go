package tools

import (
	"context"
	"fmt"

	"github.com/cosi-project/runtime/pkg/resource"
	"github.com/cosi-project/runtime/pkg/resource/meta"
	"github.com/cosi-project/runtime/pkg/safe"
	"github.com/cosi-project/runtime/pkg/state"
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
	ResourceType    string   `json:"resource_type" jsonschema:"Talos resource type, e.g. MachineStatus\\, Member\\, LinkStatus. Use talos_resource_definitions to discover all types."`
	ResourceID      string   `json:"resource_id,omitempty" jsonschema:"Optional specific resource ID. Omit to list all resources of this type."`
	Namespace       string   `json:"namespace,omitempty" jsonschema:"Resource namespace. Omit to use the default namespace for the resource type."`
	Nodes           []string `json:"nodes,omitempty" jsonschema:"Authenticated mode: exactly one target node IP or hostname (COSI reads do not support multiple nodes). Omit to use the default node from talosconfig. Mutually exclusive with endpoint."`
	Insecure        bool     `json:"insecure,omitempty" jsonschema:"Maintenance-mode insecure connection (bypasses mTLS). Requires endpoint and TALOS_MCP_ENABLE_INSECURE=true. Maintenance mode exposes a restricted COSI resource set — types that only exist post-config will surface 'not found' errors verbatim."`
	Endpoint        string   `json:"endpoint,omitempty" jsonschema:"Required when insecure=true: bare IPv4 or IPv6 address of the maintenance-mode node."`
	CertFingerprint string   `json:"cert_fingerprint,omitempty" jsonschema:"Optional SHA-256 server cert fingerprint (hex; 64 chars after stripping colons/whitespace) for TOFU pinning. Only valid when insecure=true."`
}

// HandleGetResource implements the talos_get tool.
func (h *Handlers) HandleGetResource(ctx context.Context, _ *mcp.CallToolRequest, args GetResourceArgs) (*mcp.CallToolResult, any, error) {
	h.auditLog("talos_get", getResourceAuditArgs(args), args.Nodes)

	var (
		items   []map[string]any
		outcome string
		err     error
	)
	defer func() { h.auditOutcome("talos_get", outcome, err) }()

	if err = fingerprintRequiresInsecure("talos_get", args.CertFingerprint, args.Insecure); err != nil {
		outcome = OutcomeRefusedFPWithoutInsec
		return nil, nil, err
	}

	ctx, cancel := withToolTimeout(ctx)
	defer cancel()

	if args.Insecure {
		items, outcome, err = h.getResourceInsecure(ctx, args)
	} else {
		items, outcome, err = h.getResourceAuthenticated(ctx, args)
	}
	if err != nil {
		return nil, nil, err
	}

	return jsonResult(resourceList{Items: items})
}

func getResourceAuditArgs(args GetResourceArgs) any {
	return struct {
		ResourceType      string   `json:"resource_type"`
		ResourceID        string   `json:"resource_id,omitempty"`
		Namespace         string   `json:"namespace,omitempty"`
		Nodes             []string `json:"nodes,omitempty"`
		Insecure          bool     `json:"insecure,omitempty"`
		Endpoint          string   `json:"endpoint,omitempty"`
		FingerprintPinned bool     `json:"fingerprint_pinned,omitempty"`
	}{
		ResourceType:      args.ResourceType,
		ResourceID:        args.ResourceID,
		Namespace:         args.Namespace,
		Nodes:             args.Nodes,
		Insecure:          args.Insecure,
		Endpoint:          args.Endpoint,
		FingerprintPinned: args.CertFingerprint != "",
	}
}

func (h *Handlers) getResourceInsecure(ctx context.Context, args GetResourceArgs) ([]map[string]any, string, error) {
	canonicalEndpoint, fp, gateOutcome, err := h.canonicalizeAndCheckInsecure(args.Endpoint, args.CertFingerprint, len(args.Nodes) > 0)
	if err != nil {
		return nil, gateOutcome, err
	}

	client, err := h.dialInsecure(ctx, canonicalEndpoint, fp)
	if err != nil {
		return nil, OutcomeDialError, err
	}
	defer func() { _ = client.Close() }()

	return getResourceFromCOSI(ctx, args, client.COSIState(), client.ResolveResourceKind)
}

func (h *Handlers) getResourceAuthenticated(ctx context.Context, args GetResourceArgs) ([]map[string]any, string, error) {
	if len(args.Nodes) > 1 {
		return nil, OutcomeRefusedArgs, fmt.Errorf("talos_get targets exactly one node (got %d); query each node individually", len(args.Nodes))
	}

	ctx, err := talos.WithSingleNode(ctx, args.Nodes, h.AllowedNodes)
	if err != nil {
		return nil, OutcomeRefusedAllowlist, err
	}

	return getResourceFromCOSI(ctx, args, h.Client.COSIState(), h.Client.ResolveResourceKind)
}

func getResourceFromCOSI(
	ctx context.Context,
	args GetResourceArgs,
	st state.State,
	resolveKind func(context.Context, *resource.Namespace, resource.Type) (*meta.ResourceDefinition, error),
) ([]map[string]any, string, error) {
	ns := resource.Namespace(args.Namespace)

	rd, err := resolveKind(ctx, &ns, args.ResourceType)
	if err != nil {
		return nil, OutcomeRPCError, fmt.Errorf("resolve resource kind %q: %w", args.ResourceType, err)
	}

	results, _, err := marshal.ReadResources(ctx, st, ns, rd.TypedSpec().Type, args.ResourceID, 0)
	if err != nil {
		return nil, OutcomeRPCError, err
	}

	if results == nil {
		results = []map[string]any{}
	}

	return results, OutcomeOK, nil
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
