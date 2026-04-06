package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
)

// EtcdArgs defines input for talos_etcd.
type EtcdArgs struct {
	Subcommand string   `json:"subcommand" jsonschema:"Etcd subcommand: 'members' or 'status'."`
	Nodes      []string `json:"nodes,omitempty" jsonschema:"Target node IPs or hostnames. Omit to use the default nodes from talosconfig."`
}

// handleEtcd implements the talos_etcd tool.
func (tc *TalosClient) handleEtcd(ctx context.Context, _ *mcp.CallToolRequest, args EtcdArgs) (*mcp.CallToolResult, any, error) {
	ctx = withNodes(ctx, args.Nodes)

	switch args.Subcommand {
	case "", "members":
		resp, err := tc.client.EtcdMemberList(ctx, &machineapi.EtcdMemberListRequest{})
		if err != nil {
			return nil, nil, fmt.Errorf("etcd member list: %w", err)
		}

		out, err := json.MarshalIndent(resp, "", "  ")
		if err != nil {
			return nil, nil, fmt.Errorf("marshal JSON: %w", err)
		}

		return textResult(string(out)), nil, nil

	case "status":
		resp, err := tc.client.EtcdStatus(ctx)
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
