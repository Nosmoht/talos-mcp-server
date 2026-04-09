// Package talos wraps the Talos Linux machinery client with helpers for the MCP server.
package talos

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	talosclient "github.com/siderolabs/talos/pkg/machinery/client"
	clientconfig "github.com/siderolabs/talos/pkg/machinery/client/config"

	"github.com/Nosmoht/talos-mcp-server/internal/version"
)

// dialTimeout is the maximum time allowed for the initial gRPC connection
// setup in NewClient. It prevents an indefinite hang when the Talos cluster
// is unreachable at startup.
const dialTimeout = 30 * time.Second

// Client wraps the Talos machinery client.
// Client is safe for concurrent use by multiple goroutines.
type Client struct {
	*talosclient.Client

	versionMu     sync.Mutex
	versionCached *version.TalosVersion
}

// NewClient creates a new Talos client from the default or env-configured talosconfig.
// Auth (mTLS / basic / SideroV1) is handled transparently by the client library.
func NewClient(ctx context.Context) (*Client, error) {
	configPath := os.Getenv("TALOSCONFIG") // empty → library uses default ~/.talos/config

	cfg, err := clientconfig.Open(configPath)
	if err != nil {
		return nil, err
	}

	opts := []talosclient.OptionFunc{
		talosclient.WithConfig(cfg),
		talosclient.WithDefaultGRPCDialOptions(),
	}

	if ctxName := os.Getenv("TALOS_CONTEXT"); ctxName != "" {
		opts = append(opts, talosclient.WithContextName(ctxName))
	}

	if eps := os.Getenv("TALOS_ENDPOINTS"); eps != "" {
		opts = append(opts, talosclient.WithEndpoints(strings.Split(eps, ",")...))
	}

	dialCtx, cancel := context.WithTimeout(ctx, dialTimeout)
	defer cancel()

	c, err := talosclient.New(dialCtx, opts...)
	if err != nil {
		return nil, fmt.Errorf("dial Talos gRPC (timeout %s): %w", dialTimeout, err)
	}

	return &Client{Client: c}, nil
}

// GetClusterVersion fetches and caches the Talos version from the default
// cluster endpoint. Subsequent calls return the cached value. On fetch
// failure the cache is not updated, allowing retry.
//
// This is suitable for informational use (startup log, prompts). For upgrade
// path validation use GetNodeVersion to query a specific target node.
func (c *Client) GetClusterVersion(ctx context.Context) (*version.TalosVersion, error) {
	c.versionMu.Lock()
	defer c.versionMu.Unlock()

	if c.versionCached != nil {
		return c.versionCached, nil
	}

	v, err := c.fetchVersion(ctx)
	if err != nil {
		return nil, err
	}

	c.versionCached = v

	return v, nil
}

// GetNodeVersion fetches the Talos version from a specific node without caching.
// Use this for upgrade path validation to ensure fresh per-node data.
func (c *Client) GetNodeVersion(ctx context.Context, node string) (*version.TalosVersion, error) {
	// nil allowlist: node was already validated by the calling tool handler.
	ctx, err := WithNodes(ctx, []string{node}, nil)
	if err != nil {
		return nil, err
	}

	return c.fetchVersion(ctx)
}

// Ping verifies gRPC connectivity to the default endpoint by issuing a Version
// RPC. Unlike GetClusterVersion, Ping never uses a cached result, making it
// suitable for liveness probes that must detect a lost connection.
func (c *Client) Ping(ctx context.Context) error {
	_, err := c.Version(ctx)
	return err
}

// InvalidateVersionCache clears the cached cluster version.
// Call this after a successful upgrade so the next GetClusterVersion call
// fetches fresh data.
func (c *Client) InvalidateVersionCache() {
	c.versionMu.Lock()
	defer c.versionMu.Unlock()

	c.versionCached = nil
}

// fetchVersion calls the Talos Version gRPC method and parses the first response message.
func (c *Client) fetchVersion(ctx context.Context) (*version.TalosVersion, error) {
	resp, err := c.Version(ctx)
	if err != nil {
		return nil, fmt.Errorf("fetch Talos version: %w", err)
	}

	if len(resp.Messages) == 0 {
		return nil, fmt.Errorf("fetch Talos version: empty response")
	}

	tag := resp.Messages[0].Version.GetTag()

	v, err := version.Parse(tag)
	if err != nil {
		return nil, fmt.Errorf("parse Talos version tag %q: %w", tag, err)
	}

	return &v, nil
}

// WithNodes returns a context targeting the given nodes after validating them
// against allow. If allow is nil, all nodes are permitted. If nodes is empty,
// the context is returned unchanged (uses config default).
// A single node uses WithNode (singular, "node" gRPC metadata key) to enable
// one-to-one proxying, which is required for COSI State methods that do not
// support the one-to-many fan-out used by the "nodes" key.
func WithNodes(ctx context.Context, nodes []string, allow *NodeAllowlist) (context.Context, error) {
	if len(nodes) == 0 {
		return ctx, nil
	}
	if err := allow.CheckNodes(nodes); err != nil {
		return ctx, err
	}
	if len(nodes) == 1 {
		return talosclient.WithNode(ctx, nodes[0]), nil
	}
	return talosclient.WithNodes(ctx, nodes...), nil
}
