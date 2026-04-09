package tools

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	clusterapi "github.com/siderolabs/talos/pkg/machinery/api/cluster"
	commonapi "github.com/siderolabs/talos/pkg/machinery/api/common"

	"github.com/Nosmoht/talos-mcp-server/internal/talos"
)

const (
	defaultContainerNamespace      = "k8s.io"
	defaultHealthWaitTimeout       = 2 * time.Minute
	defaultHealthWaitTimeoutBuffer = 10 * time.Second
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
	ctx, cancel := withToolTimeout(ctx)
	defer cancel()

	ctx, err := talos.WithNodes(ctx, args.Nodes, h.AllowedNodes)
	if err != nil {
		return nil, nil, err
	}

	resp, err := h.Client.Version(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("version: %w", err)
	}

	return jsonResult(resp)
}

// HandleServices implements the talos_services tool.
func (h *Handlers) HandleServices(ctx context.Context, _ *mcp.CallToolRequest, args NodesOnlyArgs) (*mcp.CallToolResult, any, error) {
	ctx, cancel := withToolTimeout(ctx)
	defer cancel()

	ctx, err := talos.WithNodes(ctx, args.Nodes, h.AllowedNodes)
	if err != nil {
		return nil, nil, err
	}

	resp, err := h.Client.ServiceList(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("service list: %w", err)
	}

	return jsonResult(resp)
}

// HandleContainers implements the talos_containers tool.
func (h *Handlers) HandleContainers(ctx context.Context, _ *mcp.CallToolRequest, args ContainersArgs) (*mcp.CallToolResult, any, error) {
	ctx, cancel := withToolTimeout(ctx)
	defer cancel()

	ctx, err := talos.WithNodes(ctx, args.Nodes, h.AllowedNodes)
	if err != nil {
		return nil, nil, err
	}

	ns := args.Namespace
	if ns == "" {
		ns = defaultContainerNamespace
	}

	resp, err := h.Client.Containers(ctx, ns, commonapi.ContainerDriver_CONTAINERD)
	if err != nil {
		return nil, nil, fmt.Errorf("containers: %w", err)
	}

	return jsonResult(resp)
}

// HandleProcesses implements the talos_processes tool.
func (h *Handlers) HandleProcesses(ctx context.Context, _ *mcp.CallToolRequest, args NodesOnlyArgs) (*mcp.CallToolResult, any, error) {
	ctx, cancel := withToolTimeout(ctx)
	defer cancel()

	ctx, err := talos.WithNodes(ctx, args.Nodes, h.AllowedNodes)
	if err != nil {
		return nil, nil, err
	}

	resp, err := h.Client.Processes(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("processes: %w", err)
	}

	return jsonResult(resp)
}

// HandleHealth implements the talos_health tool.
func (h *Handlers) HandleHealth(ctx context.Context, req *mcp.CallToolRequest, args HealthArgs) (*mcp.CallToolResult, any, error) {
	if err := h.AllowedNodes.CheckNodes(args.ControlPlaneNodes); err != nil {
		return nil, nil, fmt.Errorf("control_plane_nodes: %w", err)
	}
	if err := h.AllowedNodes.CheckNodes(args.WorkerNodes); err != nil {
		return nil, nil, fmt.Errorf("worker_nodes: %w", err)
	}

	ctx, err := talos.WithNodes(ctx, args.Nodes, h.AllowedNodes)
	if err != nil {
		return nil, nil, err
	}

	waitTimeout := defaultHealthWaitTimeout

	if args.WaitTimeout != "" {
		d, err := time.ParseDuration(args.WaitTimeout)
		if err != nil {
			return nil, nil, fmt.Errorf("parse wait_timeout %q: %w", args.WaitTimeout, err)
		}

		waitTimeout = d
	}

	var timeoutCtx context.Context

	var cancel context.CancelFunc

	timeoutCtx, cancel = context.WithTimeout(ctx, waitTimeout+defaultHealthWaitTimeoutBuffer)
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

	var streamErr error

	for {
		msg, err := stream.Recv()
		if err != nil {
			if !errors.Is(err, io.EOF) {
				streamErr = err
			}

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

	// Propagate stream errors — this is the safety-critical path.
	// A failed health check (e.g., etcd unhealthy, node not ready) arrives as
	// a gRPC error from the final Recv(), not as io.EOF. Swallowing it would
	// make HandleHealth always report success, undermining its role as a gate
	// for upgrades and config patches.
	if streamErr != nil {
		return nil, nil, fmt.Errorf("health check failed: %w", streamErr)
	}

	type healthResult struct {
		Messages []string `json:"messages"`
	}

	return jsonResult(healthResult{Messages: messages})
}
