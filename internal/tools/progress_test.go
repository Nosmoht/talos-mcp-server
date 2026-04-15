package tools

import (
	"context"
	"sync"
	"testing"
	"time"

	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// resetStubClient embeds mockClient (whose methods panic) and overrides only
// ResetGenericWithResponse so HandleReset can complete end-to-end in tests.
type resetStubClient struct {
	mockClient
}

func (r *resetStubClient) ResetGenericWithResponse(_ context.Context, _ *machineapi.ResetRequest) (*machineapi.ResetResponse, error) {
	return &machineapi.ResetResponse{}, nil
}

// TestHandleReset_EmitsProgress verifies that talos_reset emits the expected
// pair of MCP progress notifications when the caller supplies a progressToken.
// The test drives HandleReset end-to-end through the go-sdk in-memory transport
// so the assertion exercises the real Session.NotifyProgress path.
func TestHandleReset_EmitsProgress(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	h := &Handlers{Client: &resetStubClient{}}

	server := mcp.NewServer(&mcp.Implementation{Name: "talos-mcp-test"}, nil)
	mcp.AddTool(server, &mcp.Tool{Name: "talos_reset"}, h.HandleReset)

	var (
		mu       sync.Mutex
		messages []string
	)
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client"}, &mcp.ClientOptions{
		ProgressNotificationHandler: func(_ context.Context, req *mcp.ProgressNotificationClientRequest) {
			if req == nil || req.Params == nil {
				return
			}
			mu.Lock()
			messages = append(messages, req.Params.Message)
			mu.Unlock()
		},
	})

	serverT, clientT := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(ctx, serverT, nil)
	if err != nil {
		t.Fatalf("server.Connect: %v", err)
	}
	defer func() { _ = serverSession.Close() }()

	clientSession, err := client.Connect(ctx, clientT, nil)
	if err != nil {
		t.Fatalf("client.Connect: %v", err)
	}
	defer func() { _ = clientSession.Close() }()

	params := &mcp.CallToolParams{
		Name: "talos_reset",
		Arguments: ResetArgs{
			Nodes:   []string{"test-node"},
			Confirm: true,
		},
	}
	params.SetProgressToken("reset-progress-1")

	if _, err := clientSession.CallTool(ctx, params); err != nil {
		t.Fatalf("CallTool: %v", err)
	}

	// Progress notifications are asynchronous relative to the tool response;
	// give the transport a brief grace period to deliver them.
	deadline := time.Now().Add(2 * time.Second)
	for {
		mu.Lock()
		n := len(messages)
		mu.Unlock()
		if n >= 2 || time.Now().After(deadline) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(messages) < 2 {
		t.Fatalf("expected >=2 progress notifications, got %d: %v", len(messages), messages)
	}
	if messages[0] != "Initiating reset" {
		t.Errorf("first message = %q, want %q", messages[0], "Initiating reset")
	}
	if messages[1] != "Reset initiated" {
		t.Errorf("second message = %q, want %q", messages[1], "Reset initiated")
	}
}
