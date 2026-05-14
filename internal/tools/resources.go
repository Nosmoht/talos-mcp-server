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
	Nodes           []string `json:"nodes,omitempty" jsonschema:"Authenticated mode: target node IPs or hostnames. Omit to use the default nodes from talosconfig. Mutually exclusive with endpoint."`
	Insecure        bool     `json:"insecure,omitempty" jsonschema:"Maintenance-mode insecure connection (bypasses mTLS). Requires endpoint and TALOS_MCP_ENABLE_INSECURE=true. Maintenance mode exposes a restricted COSI resource set — types that only exist post-config will surface 'not found' errors verbatim."`
	Endpoint        string   `json:"endpoint,omitempty" jsonschema:"Required when insecure=true: bare IPv4 or IPv6 address of the maintenance-mode node."`
	CertFingerprint string   `json:"cert_fingerprint,omitempty" jsonschema:"Optional SHA-256 server cert fingerprint (hex; 64 chars after stripping colons/whitespace) for TOFU pinning. Only valid when insecure=true."`
}

// HandleGetResource implements the talos_get tool.
func (h *Handlers) HandleGetResource(ctx context.Context, _ *mcp.CallToolRequest, args GetResourceArgs) (*mcp.CallToolResult, any, error) {
	h.auditLog("talos_get", struct {
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
	}, args.Nodes)

	var (
		outcome  = OutcomeOK
		finalErr error
	)
	defer func() { h.auditOutcome("talos_get", outcome, finalErr) }()

	if args.CertFingerprint != "" && !args.Insecure {
		outcome = OutcomeRefusedFPWithoutInsec
		finalErr = fmt.Errorf("talos_get refused: cert_fingerprint requires insecure=true")
		return nil, nil, finalErr
	}

	ctx, cancel := withToolTimeout(ctx)
	defer cancel()

	if args.Insecure {
		canonicalEndpoint, fp, gateOutcome, err := h.canonicalizeAndCheckInsecure(args.Endpoint, args.CertFingerprint, len(args.Nodes) > 0)
		if err != nil {
			outcome = gateOutcome
			finalErr = err
			return nil, nil, err
		}
		client, err := h.dialInsecure(ctx, canonicalEndpoint, fp)
		if err != nil {
			outcome = OutcomeDialError
			finalErr = err
			return nil, nil, err
		}
		defer func() { _ = client.Close() }()
		return h.getResourceFromCOSI(ctx, args, client.COSI, client.ResolveResourceKind, &outcome, &finalErr)
	}

	ctx, err := talos.WithNodes(ctx, args.Nodes, h.AllowedNodes)
	if err != nil {
		outcome = OutcomeRefusedAllowlist
		finalErr = err
		return nil, nil, err
	}

	return h.getResourceFromCOSI(ctx, args, h.Client.COSIState(), h.Client.ResolveResourceKind, &outcome, &finalErr)
}

// getResourceFromCOSI runs the COSI Get/List against the supplied state and
// resolver. Used by both the authenticated and insecure paths so the resource
// query logic stays in one place.
func (h *Handlers) getResourceFromCOSI(
	ctx context.Context,
	args GetResourceArgs,
	st state.State,
	resolveKind func(context.Context, *resource.Namespace, resource.Type) (*meta.ResourceDefinition, error),
	outcome *string,
	finalErr *error,
) (*mcp.CallToolResult, any, error) {
	ns := resource.Namespace(args.Namespace)

	rd, err := resolveKind(ctx, &ns, args.ResourceType)
	if err != nil {
		*outcome = OutcomeRPCError
		*finalErr = fmt.Errorf("resolve resource kind %q: %w", args.ResourceType, err)
		return nil, nil, *finalErr
	}

	resourceType := rd.TypedSpec().Type

	var results []map[string]any

	if args.ResourceID != "" {
		r, err := st.Get(ctx, resource.NewMetadata(ns, resourceType, args.ResourceID, resource.VersionUndefined))
		if err != nil {
			*outcome = OutcomeRPCError
			*finalErr = fmt.Errorf("get resource %s/%s/%s: %w", ns, resourceType, args.ResourceID, err)
			return nil, nil, *finalErr
		}
		data, err := marshal.Resource(r)
		if err != nil {
			*outcome = OutcomeRPCError
			*finalErr = fmt.Errorf("marshal resource: %w", err)
			return nil, nil, *finalErr
		}
		results = []map[string]any{data}
	} else {
		list, err := st.List(ctx, resource.NewMetadata(ns, resourceType, "", resource.VersionUndefined))
		if err != nil {
			*outcome = OutcomeRPCError
			*finalErr = fmt.Errorf("list resources %s/%s: %w", ns, resourceType, err)
			return nil, nil, *finalErr
		}
		for _, r := range list.Items {
			data, err := marshal.Resource(r)
			if err != nil {
				*outcome = OutcomeRPCError
				*finalErr = fmt.Errorf("marshal resource: %w", err)
				return nil, nil, *finalErr
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
