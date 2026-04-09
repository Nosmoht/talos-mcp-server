package resources

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/cosi-project/runtime/pkg/resource/meta"
	"github.com/cosi-project/runtime/pkg/safe"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Nosmoht/talos-mcp-server/internal/marshal"
)

func (h *Handlers) handleVersion(ctx context.Context, _ *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
	resp, err := h.Client.Version(ctx)
	if err != nil {
		return nil, fmt.Errorf("get version: %w", err)
	}

	out, err := json.Marshal(resp)
	if err != nil {
		return nil, fmt.Errorf("marshal version: %w", err)
	}

	return &mcp.ReadResourceResult{
		Contents: []*mcp.ResourceContents{{
			URI:  "talos://cluster/version",
			Text: string(out),
		}},
	}, nil
}

func (h *Handlers) handleResourceDefinitions(ctx context.Context, _ *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
	list, err := safe.StateListAll[*meta.ResourceDefinition](ctx, h.Client.COSI)
	if err != nil {
		return nil, fmt.Errorf("list resource definitions: %w", err)
	}

	var defs []map[string]any
	for rd := range list.All() {
		defs = append(defs, marshal.ResourceDefinition(rd))
	}

	out, err := json.Marshal(defs)
	if err != nil {
		return nil, fmt.Errorf("marshal JSON: %w", err)
	}

	return &mcp.ReadResourceResult{
		Contents: []*mcp.ResourceContents{{
			URI:  "talos://cluster/resource-definitions",
			Text: string(out),
		}},
	}, nil
}
