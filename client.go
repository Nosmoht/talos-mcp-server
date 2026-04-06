// Package main implements the Talos MCP server.
package main

import (
	"context"
	"os"
	"strings"

	"github.com/cosi-project/runtime/pkg/resource"
	"github.com/cosi-project/runtime/pkg/resource/meta"
	yaml "go.yaml.in/yaml/v4"

	talosclient "github.com/siderolabs/talos/pkg/machinery/client"
	clientconfig "github.com/siderolabs/talos/pkg/machinery/client/config"
)

// TalosClient wraps the Talos machinery client with MCP-friendly helpers.
type TalosClient struct {
	client *talosclient.Client
}

// NewTalosClient creates a new Talos client from the default or env-configured talosconfig.
// Auth (mTLS / basic / SideroV1) is handled transparently by the client library.
func NewTalosClient(ctx context.Context) (*TalosClient, error) {
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

	return &TalosClient{client: c}, nil
}

// Close releases the underlying gRPC connection.
func (tc *TalosClient) Close() error {
	return tc.client.Close()
}

// withNodes returns a context targeting the given nodes.
// If nodes is empty, the context is returned unchanged (uses config default).
func withNodes(ctx context.Context, nodes []string) context.Context {
	if len(nodes) == 0 {
		return ctx
	}

	return talosclient.WithNodes(ctx, nodes...)
}

// marshalResource converts a COSI resource to a JSON-serializable map.
// It uses the same path as talosctl get --output json:
//
//	resource.MarshalYAML → yaml.Marshal → yaml.Unmarshal → map[string]any
func marshalResource(r resource.Resource) (map[string]any, error) {
	out, err := resource.MarshalYAML(r)
	if err != nil {
		return nil, err
	}

	yamlBytes, err := yaml.Marshal(out)
	if err != nil {
		return nil, err
	}

	var data map[string]any

	if err = yaml.Unmarshal(yamlBytes, &data); err != nil {
		return nil, err
	}

	return data, nil
}

// marshalResourceDefinition converts a ResourceDefinition to a compact summary map.
func marshalResourceDefinition(rd *meta.ResourceDefinition) map[string]any {
	spec := rd.TypedSpec()

	return map[string]any{
		"type":              spec.Type,
		"display_type":      spec.DisplayType,
		"default_namespace": spec.DefaultNamespace,
		"aliases":           spec.Aliases,
		"printColumns":      spec.PrintColumns,
	}
}
