package tools

import (
	"context"
	"fmt"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	clusterapi "github.com/siderolabs/talos/pkg/machinery/api/cluster"
	commonapi "github.com/siderolabs/talos/pkg/machinery/api/common"
	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"

	"github.com/Nosmoht/talos-mcp-server/internal/talos"
)

const (
	defaultContainerNamespace      = "k8s.io"
	defaultHealthWaitTimeout       = 2 * time.Minute
	defaultHealthWaitTimeoutBuffer = 10 * time.Second
)

// healthResult is the structured output for talos_health.
type healthResult struct {
	Messages []string `json:"messages"`
}

// Schemas for talos_version, talos_services, talos_containers, talos_processes
// use permissive object schemas because the underlying responses are Talos SDK
// protobuf types. Reflective derivation over protobuf-generated structs with
// embedded protoimpl.MessageState is noisy and risks runtime-validation
// mismatches against actual marshaled payloads. Permissive schemas meet the
// MCP spec's object-only constraint without guessing the exact shape.
var (
	versionOutputSchema    = permissiveObjectSchema()
	servicesOutputSchema   = permissiveObjectSchema()
	containersOutputSchema = permissiveObjectSchema()
	processesOutputSchema  = permissiveObjectSchema()
	healthOutputSchema     = mustDeriveSchema[healthResult]()
)

// VersionOutputSchema returns the JSON schema for HandleVersion.
func VersionOutputSchema() *jsonschema.Schema { return versionOutputSchema }

// ServicesOutputSchema returns the JSON schema for HandleServices.
func ServicesOutputSchema() *jsonschema.Schema { return servicesOutputSchema }

// ContainersOutputSchema returns the JSON schema for HandleContainers.
func ContainersOutputSchema() *jsonschema.Schema { return containersOutputSchema }

// ProcessesOutputSchema returns the JSON schema for HandleProcesses.
func ProcessesOutputSchema() *jsonschema.Schema { return processesOutputSchema }

// HealthOutputSchema returns the JSON schema for HandleHealth.
func HealthOutputSchema() *jsonschema.Schema { return healthOutputSchema }

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

// VersionArgs extends NodesOnlyArgs with maintenance-mode insecure-connection
// fields. The new fields are additive (omitempty) — strict MCP clients that
// validate "no unknown fields" may need a schema refresh.
type VersionArgs struct {
	Nodes           []string `json:"nodes,omitempty" jsonschema:"Authenticated mode: target node IPs or hostnames. Omit to use the default nodes from talosconfig. Mutually exclusive with endpoint."`
	Insecure        bool     `json:"insecure,omitempty" jsonschema:"Maintenance-mode insecure connection (bypasses mTLS). Requires endpoint and TALOS_MCP_ENABLE_INSECURE=true."`
	Endpoint        string   `json:"endpoint,omitempty" jsonschema:"Required when insecure=true: bare IPv4 or IPv6 address of the maintenance-mode node."`
	CertFingerprint string   `json:"cert_fingerprint,omitempty" jsonschema:"Optional SHA-256 server cert fingerprint (hex; 64 chars after stripping colons/whitespace) for TOFU pinning. Only valid when insecure=true."`
}

// HandleVersion implements the talos_version tool.
func (h *Handlers) HandleVersion(ctx context.Context, _ *mcp.CallToolRequest, args VersionArgs) (*mcp.CallToolResult, any, error) {
	h.auditLog("talos_version", versionAuditArgs(args), args.Nodes)

	var (
		outcome string
		err     error
	)
	defer func() { h.auditOutcome("talos_version", outcome, err) }()

	if err = fingerprintRequiresInsecure("talos_version", args.CertFingerprint, args.Insecure); err != nil {
		outcome = OutcomeRefusedFPWithoutInsec
		return nil, nil, err
	}

	ctx, cancel := withToolTimeout(ctx)
	defer cancel()

	if args.Insecure {
		var resp *machineapi.VersionResponse
		if resp, outcome, err = h.versionInsecure(ctx, args); err != nil {
			return nil, nil, err
		}
		return jsonResult(resp)
	}

	var rs []talos.NodeResult[*machineapi.VersionResponse]
	if rs, outcome, err = h.versionAuthenticated(ctx, args); err != nil {
		return nil, nil, err
	}
	return unaryResult(rs)
}

func versionAuditArgs(args VersionArgs) any {
	return struct {
		Nodes             []string `json:"nodes,omitempty"`
		Insecure          bool     `json:"insecure,omitempty"`
		Endpoint          string   `json:"endpoint,omitempty"`
		FingerprintPinned bool     `json:"fingerprint_pinned,omitempty"`
	}{
		Nodes:             args.Nodes,
		Insecure:          args.Insecure,
		Endpoint:          args.Endpoint,
		FingerprintPinned: args.CertFingerprint != "",
	}
}

func (h *Handlers) versionInsecure(ctx context.Context, args VersionArgs) (*machineapi.VersionResponse, string, error) {
	canonicalEndpoint, fp, gateOutcome, err := h.canonicalizeAndCheckInsecure(args.Endpoint, args.CertFingerprint, len(args.Nodes) > 0)
	if err != nil {
		return nil, gateOutcome, err
	}

	client, err := h.dialInsecure(ctx, canonicalEndpoint, fp)
	if err != nil {
		return nil, OutcomeDialError, err
	}
	defer func() { _ = client.Close() }()

	resp, err := client.Version(ctx)
	if err != nil {
		return nil, OutcomeRPCError, fmt.Errorf("version (insecure): %w", err)
	}

	return resp, OutcomeOK, nil
}

func (h *Handlers) versionAuthenticated(ctx context.Context, args VersionArgs) ([]talos.NodeResult[*machineapi.VersionResponse], string, error) {
	targets, err := talos.CheckTargets(args.Nodes, h.AllowedNodes)
	if err != nil {
		return nil, OutcomeRefusedAllowlist, err
	}

	rs, out := talos.FanOut(ctx, targets, func(c context.Context) (*machineapi.VersionResponse, error) {
		return h.Client.Version(c)
	})
	if _, err := out.FailClosed(); err != nil {
		return nil, OutcomeRPCError, fmt.Errorf("version: %w", err)
	}

	return rs, OutcomeOK, nil
}

// HandleServices implements the talos_services tool.
func (h *Handlers) HandleServices(ctx context.Context, _ *mcp.CallToolRequest, args NodesOnlyArgs) (*mcp.CallToolResult, any, error) {
	ctx, cancel := withToolTimeout(ctx)
	defer cancel()

	targets, err := talos.CheckTargets(args.Nodes, h.AllowedNodes)
	if err != nil {
		return nil, nil, err
	}

	rs, out := talos.FanOut(ctx, targets, func(c context.Context) (*machineapi.ServiceListResponse, error) {
		return h.Client.ServiceList(c)
	})
	if _, err := out.FailClosed(); err != nil {
		return nil, nil, fmt.Errorf("service list: %w", err)
	}

	return unaryResult(rs)
}

// HandleContainers implements the talos_containers tool.
func (h *Handlers) HandleContainers(ctx context.Context, _ *mcp.CallToolRequest, args ContainersArgs) (*mcp.CallToolResult, any, error) {
	ctx, cancel := withToolTimeout(ctx)
	defer cancel()

	targets, err := talos.CheckTargets(args.Nodes, h.AllowedNodes)
	if err != nil {
		return nil, nil, err
	}

	ns := args.Namespace
	if ns == "" {
		ns = defaultContainerNamespace
	}

	rs, out := talos.FanOut(ctx, targets, func(c context.Context) (*machineapi.ContainersResponse, error) {
		return h.Client.Containers(c, ns, commonapi.ContainerDriver_CONTAINERD)
	})
	if _, err := out.FailClosed(); err != nil {
		return nil, nil, fmt.Errorf("containers: %w", err)
	}

	return unaryResult(rs)
}

// HandleProcesses implements the talos_processes tool.
func (h *Handlers) HandleProcesses(ctx context.Context, _ *mcp.CallToolRequest, args NodesOnlyArgs) (*mcp.CallToolResult, any, error) {
	ctx, cancel := withToolTimeout(ctx)
	defer cancel()

	targets, err := talos.CheckTargets(args.Nodes, h.AllowedNodes)
	if err != nil {
		return nil, nil, err
	}

	rs, out := talos.FanOut(ctx, targets, func(c context.Context) (*machineapi.ProcessesResponse, error) {
		return h.Client.Processes(c)
	})
	if _, err := out.FailClosed(); err != nil {
		return nil, nil, fmt.Errorf("processes: %w", err)
	}

	return unaryResult(rs)
}

// HandleHealth implements the talos_health tool.
func (h *Handlers) HandleHealth(ctx context.Context, req *mcp.CallToolRequest, args HealthArgs) (*mcp.CallToolResult, any, error) {
	if err := h.AllowedNodes.CheckNodes(args.ControlPlaneNodes); err != nil {
		return nil, nil, fmt.Errorf("control_plane_nodes: %w", err)
	}
	if err := h.AllowedNodes.CheckNodes(args.WorkerNodes); err != nil {
		return nil, nil, fmt.Errorf("worker_nodes: %w", err)
	}

	targets, err := talos.CheckTargets(args.Nodes, h.AllowedNodes)
	if err != nil {
		return nil, nil, err
	}

	waitTimeout, err := parseHealthWaitTimeout(args.WaitTimeout)
	if err != nil {
		return nil, nil, err
	}

	timeoutCtx, cancel := context.WithTimeout(ctx, waitTimeout+defaultHealthWaitTimeoutBuffer)
	defer cancel()

	messages := []string{}

	var i float64

	out := talos.StreamEach(timeoutCtx, targets,
		func(c context.Context) (clusterapi.ClusterService_HealthCheckClient, error) {
			return h.Client.ClusterHealthCheck(c, waitTimeout, &clusterapi.ClusterInfo{
				ControlPlaneNodes: args.ControlPlaneNodes,
				WorkerNodes:       args.WorkerNodes,
			})
		},
		func(_ string, msg *clusterapi.HealthCheckProgress) bool {
			i++

			progressMsg := msg.GetMessage()
			if progressMsg == "" {
				progressMsg = "checking cluster health"
			}

			notifyProgress(ctx, req, progressMsg, i, 0)

			if msg.GetMessage() != "" {
				messages = append(messages, msg.GetMessage())
			}

			return false
		},
	)

	openErr, streamErr := out.FailClosed()
	if openErr != nil {
		return nil, nil, fmt.Errorf("health check: %w", openErr)
	}

	// Propagate stream errors — this is the safety-critical path.
	// A failed health check (e.g., etcd unhealthy, node not ready) arrives as
	// a gRPC error from the final Recv(), not as io.EOF. Swallowing it would
	// make HandleHealth always report success, undermining its role as a gate
	// for upgrades and config patches. With several nodes any node's failure
	// fails the call.
	if streamErr != nil {
		return nil, nil, fmt.Errorf("health check failed: %w", streamErr)
	}

	return jsonResult(healthResult{Messages: messages})
}

func parseHealthWaitTimeout(s string) (time.Duration, error) {
	if s == "" {
		return defaultHealthWaitTimeout, nil
	}

	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("parse wait_timeout %q: %w", s, err)
	}

	return d, nil
}
