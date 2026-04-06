// Package talos wraps the Talos Linux machinery client with helpers for the MCP server.
package talos

import (
	"context"
	"os"
	"strings"

	talosclient "github.com/siderolabs/talos/pkg/machinery/client"
	clientconfig "github.com/siderolabs/talos/pkg/machinery/client/config"
)

// Client wraps the Talos machinery client.
// Client is safe for concurrent use by multiple goroutines.
type Client struct {
	*talosclient.Client
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

	c, err := talosclient.New(ctx, opts...)
	if err != nil {
		return nil, err
	}

	return &Client{c}, nil
}

// WithNodes returns a context targeting the given nodes.
// If nodes is empty, the context is returned unchanged (uses config default).
func WithNodes(ctx context.Context, nodes []string) context.Context {
	if len(nodes) == 0 {
		return ctx
	}

	return talosclient.WithNodes(ctx, nodes...)
}
