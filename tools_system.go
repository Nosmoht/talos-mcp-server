package main

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	clusterapi "github.com/siderolabs/talos/pkg/machinery/api/cluster"
	commonapi "github.com/siderolabs/talos/pkg/machinery/api/common"
)

// NodesOnlyArgs is a common base for tools that only target nodes.
type NodesOnlyArgs struct {
	Nodes []string `json:"nodes,omitempty" jsonschema:"Target node IPs or hostnames. Omit to use the default nodes from talosconfig."`
}

// handleVersion implements the talos_version tool.
func (tc *TalosClient) handleVersion(ctx context.Context, _ *mcp.CallToolRequest, args NodesOnlyArgs) (*mcp.CallToolResult, any, error) {
	ctx = withNodes(ctx, args.Nodes)

	resp, err := tc.client.Version(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("version: %w", err)
	}

	out, err := json.MarshalIndent(resp, "", "  ")
	if err != nil {
		return nil, nil, fmt.Errorf("marshal JSON: %w", err)
	}

	return textResult(string(out)), nil, nil
}

// handleServices implements the talos_services tool.
func (tc *TalosClient) handleServices(ctx context.Context, _ *mcp.CallToolRequest, args NodesOnlyArgs) (*mcp.CallToolResult, any, error) {
	ctx = withNodes(ctx, args.Nodes)

	resp, err := tc.client.ServiceList(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("service list: %w", err)
	}

	out, err := json.MarshalIndent(resp, "", "  ")
	if err != nil {
		return nil, nil, fmt.Errorf("marshal JSON: %w", err)
	}

	return textResult(string(out)), nil, nil
}

// ContainersArgs defines input for talos_containers.
type ContainersArgs struct {
	Namespace string   `json:"namespace,omitempty" jsonschema:"Container namespace. Defaults to 'k8s.io' for Kubernetes containers."`
	Nodes     []string `json:"nodes,omitempty" jsonschema:"Target node IPs or hostnames. Omit to use the default nodes from talosconfig."`
}

// handleContainers implements the talos_containers tool.
func (tc *TalosClient) handleContainers(ctx context.Context, _ *mcp.CallToolRequest, args ContainersArgs) (*mcp.CallToolResult, any, error) {
	ctx = withNodes(ctx, args.Nodes)

	ns := args.Namespace
	if ns == "" {
		ns = "k8s.io"
	}

	resp, err := tc.client.Containers(ctx, ns, commonapi.ContainerDriver_CONTAINERD)
	if err != nil {
		return nil, nil, fmt.Errorf("containers: %w", err)
	}

	out, err := json.MarshalIndent(resp, "", "  ")
	if err != nil {
		return nil, nil, fmt.Errorf("marshal JSON: %w", err)
	}

	return textResult(string(out)), nil, nil
}

// handleProcesses implements the talos_processes tool.
func (tc *TalosClient) handleProcesses(ctx context.Context, _ *mcp.CallToolRequest, args NodesOnlyArgs) (*mcp.CallToolResult, any, error) {
	ctx = withNodes(ctx, args.Nodes)

	resp, err := tc.client.Processes(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("processes: %w", err)
	}

	out, err := json.MarshalIndent(resp, "", "  ")
	if err != nil {
		return nil, nil, fmt.Errorf("marshal JSON: %w", err)
	}

	return textResult(string(out)), nil, nil
}

// HealthArgs defines input for talos_health.
type HealthArgs struct {
	WaitTimeout string   `json:"wait_timeout,omitempty" jsonschema:"How long to wait for cluster health (e.g. '2m'\\, '30s'). Defaults to 2 minutes."`
	Nodes       []string `json:"nodes,omitempty" jsonschema:"Target node IPs or hostnames. Omit to use the default nodes from talosconfig."`
}

// handleHealth implements the talos_health tool.
func (tc *TalosClient) handleHealth(ctx context.Context, _ *mcp.CallToolRequest, args HealthArgs) (*mcp.CallToolResult, any, error) {
	ctx = withNodes(ctx, args.Nodes)

	waitTimeout := 2 * time.Minute

	if args.WaitTimeout != "" {
		d, err := time.ParseDuration(args.WaitTimeout)
		if err != nil {
			return nil, nil, fmt.Errorf("parse wait_timeout %q: %w", args.WaitTimeout, err)
		}

		waitTimeout = d
	}

	var timeoutCtx context.Context

	var cancel context.CancelFunc

	timeoutCtx, cancel = context.WithTimeout(ctx, waitTimeout+10*time.Second)
	defer cancel()

	stream, err := tc.client.ClusterHealthCheck(timeoutCtx, waitTimeout, &clusterapi.ClusterInfo{})
	if err != nil {
		return nil, nil, fmt.Errorf("health check: %w", err)
	}

	var messages []string

	for {
		msg, err := stream.Recv()
		if err != nil {
			break
		}

		if msg.GetMessage() != "" {
			messages = append(messages, msg.GetMessage())
		}
	}

	type healthResult struct {
		Messages []string `json:"messages"`
	}

	out, err := json.MarshalIndent(healthResult{Messages: messages}, "", "  ")
	if err != nil {
		return nil, nil, fmt.Errorf("marshal JSON: %w", err)
	}

	return textResult(string(out)), nil, nil
}

// textResult constructs a simple text MCP CallToolResult.
func textResult(text string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: text},
		},
	}
}
