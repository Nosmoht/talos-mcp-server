package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestHandleHealth_InvalidTimeout(t *testing.T) {
	h := safeH()
	ctx := context.Background()

	tests := []struct {
		name        string
		waitTimeout string
		wantErr     string
	}{
		{
			name:        "invalid duration",
			waitTimeout: "not-a-duration",
			wantErr:     "parse wait_timeout",
		},
		{
			name:        "bare number without unit",
			waitTimeout: "120",
			wantErr:     "parse wait_timeout",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := HealthArgs{WaitTimeout: tt.waitTimeout}
			_, _, err := h.HandleHealth(ctx, &mcp.CallToolRequest{}, args)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error %q does not contain %q", err.Error(), tt.wantErr)
			}
		})
	}
}
