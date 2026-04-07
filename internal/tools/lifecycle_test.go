package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Nosmoht/talos-mcp-server/internal/talos"
)

// safeH returns a Handlers with a nil-embedded Talos client.
// Safe to use only for test cases that return before touching the gRPC client.
func safeH() *Handlers {
	return &Handlers{Client: &talos.Client{}}
}

// TestHandleReboot_Guards verifies that reboot is rejected without confirm or without nodes.
func TestHandleReboot_Guards(t *testing.T) {
	h := safeH()
	ctx := context.Background()

	tests := []struct {
		name    string
		args    RebootArgs
		wantErr string
	}{
		{
			name:    "no confirm",
			args:    RebootArgs{Nodes: []string{"192.168.2.61"}, Confirm: false},
			wantErr: "confirm must be explicitly set to true",
		},
		{
			name:    "no nodes",
			args:    RebootArgs{Nodes: nil, Confirm: true},
			wantErr: "nodes must be explicitly specified",
		},
		{
			name:    "empty nodes",
			args:    RebootArgs{Nodes: []string{}, Confirm: true},
			wantErr: "nodes must be explicitly specified",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := h.HandleReboot(ctx, nil, tt.args)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error %q does not contain %q", err.Error(), tt.wantErr)
			}
		})
	}
}

// TestHandleUpgrade_Guards verifies that upgrade is rejected for missing fields.
func TestHandleUpgrade_Guards(t *testing.T) {
	h := safeH()
	ctx := context.Background()

	tests := []struct {
		name    string
		args    UpgradeArgs
		wantErr string
	}{
		{
			name:    "no confirm",
			args:    UpgradeArgs{Nodes: []string{"192.168.2.61"}, Image: "ghcr.io/siderolabs/installer:v1.12.6", Confirm: false},
			wantErr: "confirm must be explicitly set to true",
		},
		{
			name:    "no nodes",
			args:    UpgradeArgs{Nodes: nil, Image: "ghcr.io/siderolabs/installer:v1.12.6", Confirm: true},
			wantErr: "nodes must be explicitly specified",
		},
		{
			name:    "no image",
			args:    UpgradeArgs{Nodes: []string{"192.168.2.61"}, Image: "", Confirm: true},
			wantErr: "image must be specified",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := h.HandleUpgrade(ctx, nil, tt.args)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error %q does not contain %q", err.Error(), tt.wantErr)
			}
		})
	}
}

// TestHandleServiceAction_InvalidAction verifies unknown actions are rejected.
func TestHandleServiceAction_InvalidAction(t *testing.T) {
	h := safeH()
	ctx := context.Background()

	_, _, err := h.HandleServiceAction(ctx, nil, ServiceActionArgs{
		ServiceName: "kubelet",
		Action:      "obliterate",
	})
	if err == nil {
		t.Fatal("expected error for unknown action, got nil")
	}
	if !strings.Contains(err.Error(), "unknown action") {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestHandlePatchConfig_InvalidMode verifies unknown modes are rejected.
func TestHandlePatchConfig_InvalidMode(t *testing.T) {
	h := safeH()
	ctx := context.Background()

	_, _, err := h.HandlePatchConfig(ctx, nil, PatchConfigArgs{
		Patch: `{}`,
		Mode:  "turbo",
	})
	if err == nil {
		t.Fatal("expected error for unknown mode, got nil")
	}
	if !strings.Contains(err.Error(), "unknown mode") {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestNotifyProgress_NilReq verifies that notifyProgress is a no-op when req is nil.
func TestNotifyProgress_NilReq(_ *testing.T) {
	// Must not panic.
	notifyProgress(context.Background(), nil, "test", 1, 1)
}

// TestNotifyProgress_NoToken verifies that notifyProgress is a no-op when the
// request carries no progress token.
func TestNotifyProgress_NoToken(_ *testing.T) {
	req := &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{},
	}
	// Token is nil — must not panic or call NotifyProgress.
	notifyProgress(context.Background(), req, "test", 1, 1)
}

// TestHandleReboot_InvalidMode verifies that unknown reboot modes are rejected.
func TestHandleReboot_InvalidMode(t *testing.T) {
	h := safeH()
	ctx := context.Background()

	tests := []struct {
		name    string
		args    RebootArgs
		wantErr string
	}{
		{
			name:    "unknown mode",
			args:    RebootArgs{Nodes: []string{"192.168.2.61"}, Mode: "turbo", Confirm: true},
			wantErr: "unknown mode",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := h.HandleReboot(ctx, nil, tt.args)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error %q does not contain %q", err.Error(), tt.wantErr)
			}
		})
	}
}

// TestHandleUpgrade_InvalidRebootMode verifies that unknown reboot_mode values are rejected.
func TestHandleUpgrade_InvalidRebootMode(t *testing.T) {
	h := safeH()
	ctx := context.Background()

	_, _, err := h.HandleUpgrade(ctx, nil, UpgradeArgs{
		Nodes:      []string{"192.168.2.61"},
		Image:      "ghcr.io/siderolabs/installer:v1.12.6",
		Confirm:    true,
		RebootMode: "warp-drive",
	})
	if err == nil {
		t.Fatal("expected error for unknown reboot_mode, got nil")
	}
	if !strings.Contains(err.Error(), "unknown reboot_mode") {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestHandleRollback_Guards verifies that rollback is rejected without confirm or nodes.
func TestHandleRollback_Guards(t *testing.T) {
	h := safeH()
	ctx := context.Background()

	tests := []struct {
		name    string
		args    RollbackArgs
		wantErr string
	}{
		{
			name:    "no confirm",
			args:    RollbackArgs{Nodes: []string{"192.168.2.61"}, Confirm: false},
			wantErr: "confirm must be explicitly set to true",
		},
		{
			name:    "no nodes",
			args:    RollbackArgs{Nodes: nil, Confirm: true},
			wantErr: "nodes must be explicitly specified",
		},
		{
			name:    "empty nodes",
			args:    RollbackArgs{Nodes: []string{}, Confirm: true},
			wantErr: "nodes must be explicitly specified",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := h.HandleRollback(ctx, nil, tt.args)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error %q does not contain %q", err.Error(), tt.wantErr)
			}
		})
	}
}

// TestResolvePreserve verifies the preserve default behaviour.
func TestResolvePreserve(t *testing.T) {
	f := false
	tr := true

	tests := []struct {
		name string
		in   *bool
		want bool
	}{
		{"nil means preserve", nil, true},
		{"explicit true preserves", &tr, true},
		{"explicit false wipes", &f, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolvePreserve(tt.in)
			if got != tt.want {
				t.Errorf("resolvePreserve(%v) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

// TestResolveDryRun verifies the dry_run default behaviour.
func TestResolveDryRun(t *testing.T) {
	f := false
	tr := true

	tests := []struct {
		name string
		in   *bool
		want bool
	}{
		{"nil means dry-run", nil, true},
		{"explicit true is dry-run", &tr, true},
		{"explicit false is live", &f, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveDryRun(tt.in)
			if got != tt.want {
				t.Errorf("resolveDryRun(%v) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}
