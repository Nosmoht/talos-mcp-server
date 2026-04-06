package resources

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/cosi-project/runtime/pkg/resource"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Nosmoht/talos-mcp-server/internal/talos"
	"github.com/Nosmoht/talos-mcp-server/internal/tools"
)

const maxListResults = 100

func (h *Handlers) handleCOSIResource(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
	node, ns, resType, resID, err := parseCOSIURI(req.Params.URI)
	if err != nil {
		return nil, fmt.Errorf("parse resource URI %q: %w", req.Params.URI, err)
	}

	ctx = talos.WithNodes(ctx, []string{node})

	namespace := resource.Namespace(ns)
	rd, err := h.Client.ResolveResourceKind(ctx, &namespace, resType)
	if err != nil {
		return nil, mcp.ResourceNotFoundError(req.Params.URI)
	}

	resolvedType := rd.TypedSpec().Type
	var results []map[string]any
	truncated := false

	if resID != "" {
		r, err := h.Client.COSI.Get(ctx,
			resource.NewMetadata(namespace, resolvedType, resID, resource.VersionUndefined),
		)
		if err != nil {
			return nil, mcp.ResourceNotFoundError(req.Params.URI)
		}

		data, err := tools.MarshalResource(r)
		if err != nil {
			return nil, fmt.Errorf("marshal resource: %w", err)
		}
		results = []map[string]any{data}
	} else {
		list, err := h.Client.COSI.List(ctx,
			resource.NewMetadata(namespace, resolvedType, "", resource.VersionUndefined),
		)
		if err != nil {
			return nil, fmt.Errorf("list resources %s/%s: %w", ns, resolvedType, err)
		}

		for _, r := range list.Items {
			if len(results) >= maxListResults {
				truncated = true
				break
			}
			data, err := tools.MarshalResource(r)
			if err != nil {
				return nil, fmt.Errorf("marshal resource: %w", err)
			}
			results = append(results, data)
		}
	}

	// Resources wrap results in an envelope to convey truncation state.
	// Unlike the talos_get tool (which returns a raw array), resources may
	// be capped at maxListResults items; the truncated field signals this.
	type response struct {
		Items     []map[string]any `json:"items"`
		Truncated bool             `json:"truncated,omitempty"`
	}
	payload := response{Items: results, Truncated: truncated}

	out, err := json.MarshalIndent(payload, "", "  ")
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
