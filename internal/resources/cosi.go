package resources

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/cosi-project/runtime/pkg/resource"
	"github.com/cosi-project/runtime/pkg/state"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	talosclient "github.com/siderolabs/talos/pkg/machinery/client"
	"google.golang.org/grpc/codes"

	"github.com/Nosmoht/talos-mcp-server/internal/marshal"
	"github.com/Nosmoht/talos-mcp-server/internal/talos"
)

const maxListResults = 100

func (h *Handlers) handleCOSIResource(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
	node, ns, resType, resID, err := ParseCOSIURI(req.Params.URI)
	if err != nil {
		return nil, fmt.Errorf("parse resource URI %q: %w", req.Params.URI, err)
	}

	ctx, err = talos.WithSingleNode(ctx, []string{node}, h.AllowedNodes)
	if err != nil {
		return nil, fmt.Errorf("node not allowed: %w", err)
	}

	namespace := resource.Namespace(ns)
	rd, err := h.Client.ResolveResourceKind(ctx, &namespace, resType)
	if talosclient.StatusCode(err) == codes.NotFound {
		return nil, mcp.ResourceNotFoundError(req.Params.URI)
	}

	if err != nil {
		return nil, fmt.Errorf("resolve resource type %q: %w", resType, err)
	}

	items, truncated, err := marshal.ReadResources(ctx, h.Client.COSIState(), namespace, rd.TypedSpec().Type, resID, maxListResults)
	if resID != "" && state.IsNotFoundError(err) {
		return nil, mcp.ResourceNotFoundError(req.Params.URI)
	}
	if err != nil {
		return nil, err
	}

	type response struct {
		Items     []map[string]any `json:"items"`
		Truncated bool             `json:"truncated,omitempty"`
	}
	payload := response{Items: items, Truncated: truncated}

	out, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal JSON: %w", err)
	}

	return &mcp.ReadResourceResult{
		Contents: []*mcp.ResourceContents{{
			URI:  req.Params.URI,
			Text: string(out),
		}},
	}, nil
}
