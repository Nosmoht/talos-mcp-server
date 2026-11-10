package resources

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Predicate (AC-7): resources/read on a talos:// URI that names no existing
// resource fails with JSON-RPC code Invalid Params (-32602, MCP SEP-2164).
func TestGivenUnresolvableTalosResource_WhenRead_ThenInvalidParamsCodeIsReturned(t *testing.T) {
	h := newCOSIHandlers(t, []string{"seeded-a"})
	server := mcp.NewServer(&mcp.Implementation{Name: "not-found-code-test", Version: "0.0.0"}, nil)
	Register(server, h.Client, h.AllowedNodes)

	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	go func() {
		_ = server.Run(ctx, serverTransport)
	}()

	client := mcp.NewClient(&mcp.Implementation{Name: "not-found-code-client", Version: "0.0.0"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("setup: client connect: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })

	cases := []struct {
		id  string
		uri string
	}{
		{id: "unknown_resource_kind", uri: "talos://" + testNode + "/resource/runtime/NoSuchKind"},
		{id: "unknown_resource_id", uri: "talos://" + testNode + "/resource/runtime/MetaKey/absent"},
	}
	for _, tc := range cases {
		t.Run(tc.id, func(t *testing.T) {
			_, err := session.ReadResource(ctx, &mcp.ReadResourceParams{URI: tc.uri})
			var rpcErr *jsonrpc.Error
			if !errors.As(err, &rpcErr) {
				t.Fatalf("AC-7: case=%s expected=JSON-RPC error code %d got=%v", tc.id, jsonrpc.CodeInvalidParams, err)
			}
			if rpcErr.Code != jsonrpc.CodeInvalidParams {
				t.Errorf("AC-7: case=%s expected=code %d got=%d (%s)", tc.id, jsonrpc.CodeInvalidParams, rpcErr.Code, rpcErr.Message)
			}
		})
	}
}
