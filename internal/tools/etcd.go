package tools

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"

	"github.com/Nosmoht/talos-mcp-server/internal/talos"
)

// etcdSnapshotResult is the structured output for talos_etcd_snapshot.
type etcdSnapshotResult struct {
	Path  string `json:"path"`
	Bytes int64  `json:"bytes"`
}

// etcdOutputSchema is permissive: the handler returns one of two Talos
// protobuf types (member list or status) depending on the subcommand, and
// reflective derivation over protoimpl-embedded structs is noisy.
var (
	etcdOutputSchema         = permissiveObjectSchema()
	etcdSnapshotOutputSchema = mustDeriveSchema[etcdSnapshotResult]()
)

// EtcdOutputSchema returns the JSON schema for HandleEtcd.
func EtcdOutputSchema() *jsonschema.Schema { return etcdOutputSchema }

// EtcdSnapshotOutputSchema returns the JSON schema for HandleEtcdSnapshot.
func EtcdSnapshotOutputSchema() *jsonschema.Schema { return etcdSnapshotOutputSchema }

// EtcdArgs defines input for talos_etcd.
type EtcdArgs struct {
	Subcommand string   `json:"subcommand" jsonschema:"Etcd subcommand: 'members' or 'status'."`
	Nodes      []string `json:"nodes,omitempty" jsonschema:"Target node IPs or hostnames. Omit to use the default nodes from talosconfig."`
}

// HandleEtcd implements the talos_etcd tool.
func (h *Handlers) HandleEtcd(ctx context.Context, _ *mcp.CallToolRequest, args EtcdArgs) (*mcp.CallToolResult, any, error) {
	ctx, cancel := withToolTimeout(ctx)
	defer cancel()

	ctx, err := talos.WithNodes(ctx, args.Nodes, h.AllowedNodes)
	if err != nil {
		return nil, nil, err
	}

	switch args.Subcommand {
	case "", "members":
		resp, err := h.Client.EtcdMemberList(ctx, &machineapi.EtcdMemberListRequest{})
		if err != nil {
			return nil, nil, fmt.Errorf("etcd member list: %w", err)
		}

		return jsonResult(resp)

	case "status":
		resp, err := h.Client.EtcdStatus(ctx)
		if err != nil {
			return nil, nil, fmt.Errorf("etcd status: %w", err)
		}

		return jsonResult(resp)

	default:
		return nil, nil, fmt.Errorf("unknown etcd subcommand %q: must be 'members' or 'status'", args.Subcommand)
	}
}

// etcdSnapshotTimeout is the maximum time allowed for streaming an etcd snapshot.
// Etcd databases can range from a few MB to hundreds of MB; 5 minutes is
// generous for any reasonable cluster size on a local network.
const etcdSnapshotTimeout = 5 * time.Minute

// EtcdSnapshotArgs defines input for talos_etcd_snapshot.
type EtcdSnapshotArgs struct {
	Nodes []string `json:"nodes" jsonschema:"Target control plane node (exactly one required). Etcd snapshots must be taken from a single control plane node."`
	Path  string   `json:"path" jsonschema:"Absolute local file path to write the snapshot (e.g. /tmp/etcd-snapshot.db). The parent directory must exist."`
}

// HandleEtcdSnapshot implements the talos_etcd_snapshot tool.
func (h *Handlers) HandleEtcdSnapshot(ctx context.Context, req *mcp.CallToolRequest, args EtcdSnapshotArgs) (*mcp.CallToolResult, any, error) {
	if args.Path == "" {
		return nil, nil, fmt.Errorf("path must be specified")
	}

	if !filepath.IsAbs(args.Path) {
		return nil, nil, fmt.Errorf("path must be absolute (got %q)", args.Path)
	}

	if cleaned := filepath.Clean(args.Path); cleaned != args.Path {
		return nil, nil, fmt.Errorf("path must not contain .. or redundant separators (got %q)", args.Path)
	}

	if len(args.Nodes) != 1 {
		return nil, nil, fmt.Errorf("talos_etcd_snapshot requires exactly one target node (got %d); specify a single control plane node", len(args.Nodes))
	}

	ctx, err := talos.WithNodes(ctx, args.Nodes, h.AllowedNodes)
	if err != nil {
		return nil, nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, etcdSnapshotTimeout)
	defer cancel()

	notifyProgress(ctx, req, "Requesting etcd snapshot", 1, 3)

	rc, err := h.Client.EtcdSnapshot(ctx, &machineapi.EtcdSnapshotRequest{})
	if err != nil {
		return nil, nil, fmt.Errorf("etcd snapshot: %w", err)
	}
	defer rc.Close() //nolint:errcheck

	notifyProgress(ctx, req, "Streaming snapshot to disk", 2, 3)

	f, err := os.Create(args.Path)
	if err != nil {
		return nil, nil, fmt.Errorf("create snapshot file: %w", err)
	}
	defer f.Close() //nolint:errcheck // cleanup-only; error handled by explicit Close below

	n, err := io.Copy(f, rc)
	if err != nil {
		os.Remove(args.Path) //nolint:errcheck,gosec // best-effort cleanup of partial file
		return nil, nil, fmt.Errorf("stream snapshot: %w", err)
	}

	if err := f.Sync(); err != nil {
		return nil, nil, fmt.Errorf("sync snapshot file: %w", err)
	}

	if err := f.Close(); err != nil {
		return nil, nil, fmt.Errorf("close snapshot file: %w", err)
	}

	notifyProgress(ctx, req, "Snapshot complete", 3, 3)

	return jsonResult(etcdSnapshotResult{Path: args.Path, Bytes: n})
}
