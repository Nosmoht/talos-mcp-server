package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	clusterapi "github.com/siderolabs/talos/pkg/machinery/api/cluster"
	commonapi "github.com/siderolabs/talos/pkg/machinery/api/common"

	"github.com/Nosmoht/talos-mcp-server/internal/talos"
)

// ContainersArgs defines input for talos_containers.
type ContainersArgs struct {
	Namespace string   `json:"namespace,omitempty" jsonschema:"Container namespace. Defaults to 'k8s.io' for Kubernetes containers."`
	Nodes     []string `json:"nodes,omitempty" jsonschema:"Target node IPs or hostnames. Omit to use the default nodes from talosconfig."`
}

// HealthArgs defines input for talos_health.
type HealthArgs struct {
	WaitTimeout       string   `json:"wait_timeout,omitempty" jsonschema:"How long to wait for cluster health (e.g. '2m'\\, '30s'). Defaults to 2 minutes."`
	Nodes             []string `json:"nodes,omitempty" jsonschema:"Target node IPs or hostnames. Omit to use the default nodes from talosconfig."`
	ControlPlaneNodes []string `json:"control_plane_nodes,omitempty" jsonschema:"Explicit list of control plane node IPs. Overrides auto-detection from cluster discovery. Use when discovery is misconfigured or nodes have not yet joined."`
	WorkerNodes       []string `json:"worker_nodes,omitempty" jsonschema:"Explicit list of worker node IPs. Overrides auto-detection from cluster discovery. Use when discovery is misconfigured or nodes have not yet joined."`
}

// HandleVersion implements the talos_version tool.
func (h *Handlers) HandleVersion(ctx context.Context, _ *mcp.CallToolRequest, args NodesOnlyArgs) (*mcp.CallToolResult, any, error) {
	ctx = talos.WithNodes(ctx, args.Nodes)

	resp, err := h.Client.Version(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("version: %w", err)
	}

	out, err := json.MarshalIndent(resp, "", "  ")
	if err != nil {
		return nil, nil, fmt.Errorf("marshal JSON: %w", err)
	}

	return textResult(string(out)), nil, nil
}

// HandleServices implements the talos_services tool.
func (h *Handlers) HandleServices(ctx context.Context, _ *mcp.CallToolRequest, args NodesOnlyArgs) (*mcp.CallToolResult, any, error) {
	ctx = talos.WithNodes(ctx, args.Nodes)

	resp, err := h.Client.ServiceList(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("service list: %w", err)
	}

	out, err := json.MarshalIndent(resp, "", "  ")
	if err != nil {
		return nil, nil, fmt.Errorf("marshal JSON: %w", err)
	}

	return textResult(string(out)), nil, nil
}

// HandleContainers implements the talos_containers tool.
func (h *Handlers) HandleContainers(ctx context.Context, _ *mcp.CallToolRequest, args ContainersArgs) (*mcp.CallToolResult, any, error) {
	ctx = talos.WithNodes(ctx, args.Nodes)

	ns := args.Namespace
	if ns == "" {
		ns = "k8s.io"
	}

	resp, err := h.Client.Containers(ctx, ns, commonapi.ContainerDriver_CONTAINERD)
	if err != nil {
		return nil, nil, fmt.Errorf("containers: %w", err)
	}

	out, err := json.MarshalIndent(resp, "", "  ")
	if err != nil {
		return nil, nil, fmt.Errorf("marshal JSON: %w", err)
	}

	return textResult(string(out)), nil, nil
}

// HandleProcesses implements the talos_processes tool.
func (h *Handlers) HandleProcesses(ctx context.Context, _ *mcp.CallToolRequest, args NodesOnlyArgs) (*mcp.CallToolResult, any, error) {
	ctx = talos.WithNodes(ctx, args.Nodes)

	resp, err := h.Client.Processes(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("processes: %w", err)
	}

	out, err := json.MarshalIndent(resp, "", "  ")
	if err != nil {
		return nil, nil, fmt.Errorf("marshal JSON: %w", err)
	}

	return textResult(string(out)), nil, nil
}

// HandleHealth implements the talos_health tool.
func (h *Handlers) HandleHealth(ctx context.Context, req *mcp.CallToolRequest, args HealthArgs) (*mcp.CallToolResult, any, error) {
	ctx = talos.WithNodes(ctx, args.Nodes)

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

	stream, err := h.Client.ClusterHealthCheck(timeoutCtx, waitTimeout, &clusterapi.ClusterInfo{
		ControlPlaneNodes: args.ControlPlaneNodes,
		WorkerNodes:       args.WorkerNodes,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("health check: %w", err)
	}

	var messages []string

	var i float64

	for {
		msg, err := stream.Recv()
		if err != nil {
			break
		}

		i++

		progressMsg := msg.GetMessage()
		if progressMsg == "" {
			progressMsg = "checking cluster health"
		}

		notifyProgress(ctx, req, progressMsg, i, 0)

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
