package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"

	"github.com/Nosmoht/talos-mcp-server/internal/talos"
)

// EtcdArgs defines input for talos_etcd.
type EtcdArgs struct {
	Subcommand string   `json:"subcommand" jsonschema:"Etcd subcommand: 'members' or 'status'."`
	Nodes      []string `json:"nodes,omitempty" jsonschema:"Target node IPs or hostnames. Omit to use the default nodes from talosconfig."`
}

// HandleEtcd implements the talos_etcd tool.
func (h *Handlers) HandleEtcd(ctx context.Context, _ *mcp.CallToolRequest, args EtcdArgs) (*mcp.CallToolResult, any, error) {
	ctx, cancel := withToolTimeout(ctx)
	defer cancel()

	ctx = talos.WithNodes(ctx, args.Nodes)

	switch args.Subcommand {
	case "", "members":
		resp, err := h.Client.EtcdMemberList(ctx, &machineapi.EtcdMemberListRequest{})
		if err != nil {
			return nil, nil, fmt.Errorf("etcd member list: %w", err)
		}

		out, err := json.MarshalIndent(resp, "", "  ")
		if err != nil {
			return nil, nil, fmt.Errorf("marshal JSON: %w", err)
		}

		return textResult(string(out)), nil, nil

	case "status":
		resp, err := h.Client.EtcdStatus(ctx)
		if err != nil {
			return nil, nil, fmt.Errorf("etcd status: %w", err)
		}

		out, err := json.MarshalIndent(resp, "", "  ")
		if err != nil {
			return nil, nil, fmt.Errorf("marshal JSON: %w", err)
		}

		return textResult(string(out)), nil, nil

	default:
		return nil, nil, fmt.Errorf("unknown etcd subcommand %q: must be 'members' or 'status'", args.Subcommand)
	}
}
