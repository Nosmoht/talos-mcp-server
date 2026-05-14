package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/Nosmoht/talos-mcp-server/internal/talos"
)

// TestHandleGetResource_InsecureGuards verifies the maintenance-mode gate
// rejections in HandleGetResource — before any dial attempt. The mock client
// panics on COSIState() / ResolveResourceKind, so reaching the gRPC layer
// surfaces as a test failure.
func TestHandleGetResource_InsecureGuards(t *testing.T) {
	ctx := context.Background()

	allowlist, err := talos.ParseNodeAllowlist("192.0.2.5")
	if err != nil {
		t.Fatalf("setup: parse allowlist: %v", err)
	}

	enabledH := func() *Handlers {
		return &Handlers{
			Client:               &mockClient{},
			EnableInsecure:       true,
			InsecureAllowedNodes: allowlist,
		}
	}

	tests := []struct {
		name    string
		h       *Handlers
		args    GetResourceArgs
		wantErr string
	}{
		{
			name: "cert_fingerprint without insecure rejected",
			h:    safeH(),
			args: GetResourceArgs{
				ResourceType:    "MachineStatus",
				CertFingerprint: strings.Repeat("ab", 32),
			},
			wantErr: "cert_fingerprint requires insecure=true",
		},
		{
			name: "insecure without enable",
			h:    safeH(),
			args: GetResourceArgs{
				ResourceType: "MachineStatus",
				Insecure:     true,
				Endpoint:     "192.0.2.5",
			},
			wantErr: "TALOS_MCP_ENABLE_INSECURE",
		},
		{
			name: "insecure with nodes mutually exclusive",
			h:    enabledH(),
			args: GetResourceArgs{
				ResourceType: "MachineStatus",
				Insecure:     true,
				Endpoint:     "192.0.2.5",
				Nodes:        []string{"192.0.2.6"},
			},
			wantErr: "mutually exclusive",
		},
		{
			name: "insecure non-IP endpoint",
			h:    enabledH(),
			args: GetResourceArgs{
				ResourceType: "MachineStatus",
				Insecure:     true,
				Endpoint:     "node.example.com",
			},
			wantErr: "not a bare IP",
		},
		{
			name: "insecure endpoint not in allowlist",
			h:    enabledH(),
			args: GetResourceArgs{
				ResourceType: "MachineStatus",
				Insecure:     true,
				Endpoint:     "192.0.2.99",
			},
			wantErr: "not in TALOS_MCP_INSECURE_ALLOWED_NODES",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := tt.h.HandleGetResource(ctx, nil, tt.args)
			if err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("expected error containing %q, got: %v", tt.wantErr, err)
			}
		})
	}
}
