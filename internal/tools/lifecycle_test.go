package tools

import (
	"context"
	"strings"
	"testing"
	"time"

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

// TestHandlePatchConfig_ConfirmGuard verifies that non-dry-run patches require
// explicit confirmation, while dry-run patches do not.
func TestHandlePatchConfig_ConfirmGuard(t *testing.T) {
	h := safeH()
	ctx := context.Background()
	dryRunFalse := false
	dryRunTrue := true

	tests := []struct {
		name    string
		args    PatchConfigArgs
		wantErr string
	}{
		{
			name: "dry_run=false without confirm is rejected",
			args: PatchConfigArgs{
				Patch:   `{}`,
				DryRun:  &dryRunFalse,
				Confirm: false,
			},
			wantErr: "confirm must be explicitly set to true when dry_run is false",
		},
		{
			name: "dry_run=false with confirm=true is accepted",
			args: PatchConfigArgs{
				Patch:   `{}`,
				DryRun:  &dryRunFalse,
				Confirm: true,
			},
			// No confirm error — will fail later on gRPC call (nil client),
			// which is expected and proves the guard passed.
			wantErr: "",
		},
		{
			name: "dry_run=true without confirm is accepted",
			args: PatchConfigArgs{
				Patch:   `{}`,
				DryRun:  &dryRunTrue,
				Confirm: false,
			},
			wantErr: "",
		},
		{
			name: "dry_run omitted (defaults to true) without confirm is accepted",
			args: PatchConfigArgs{
				Patch:   `{}`,
				Confirm: false,
			},
			wantErr: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := h.HandlePatchConfig(ctx, nil, tt.args)
			if tt.wantErr == "" {
				// Guard should pass; any error is from downstream (nil client), not the guard.
				if err != nil && strings.Contains(err.Error(), "confirm") {
					t.Errorf("unexpected confirm guard error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("expected error containing %q, got: %v", tt.wantErr, err)
			}
		})
	}
}

// TestHandlePatchConfig_MultiNodeRejected verifies that more than one node is rejected.
func TestHandlePatchConfig_MultiNodeRejected(t *testing.T) {
	h := safeH()
	ctx := context.Background()

	_, _, err := h.HandlePatchConfig(ctx, nil, PatchConfigArgs{
		Patch: `{}`,
		Nodes: []string{"10.0.0.1", "10.0.0.2"},
	})
	if err == nil {
		t.Fatal("expected error for multi-node, got nil")
	}
	if !strings.Contains(err.Error(), "exactly one target node") {
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

// TestParseRebootTimeout verifies default, valid, and invalid duration strings.
func TestParseRebootTimeout(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    time.Duration
		wantErr bool
	}{
		{"empty uses default", "", 5 * time.Minute, false},
		{"valid 10m", "10m", 10 * time.Minute, false},
		{"valid 30s", "30s", 30 * time.Second, false},
		{"invalid string", "not-a-duration", 0, true},
		{"invalid number only", "300", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseRebootTimeout(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("parseRebootTimeout(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

// TestHandleReboot_InvalidTimeout verifies that wait=true with a bad timeout
// string is rejected before the reboot is issued.
func TestHandleReboot_InvalidTimeout(t *testing.T) {
	h := safeH()
	ctx := context.Background()

	_, _, err := h.HandleReboot(ctx, nil, RebootArgs{
		Nodes:   []string{"192.168.2.61"},
		Confirm: true,
		Wait:    true,
		Timeout: "not-a-duration",
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "invalid timeout") {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestHandleReboot_WaitFalseIgnoresTimeout verifies that the fire-and-forget path
// does not parse or validate the timeout field. The call uses an invalid mode so
// it returns a "unknown mode" error before reaching the gRPC layer — proving that
// if an "invalid timeout" error were generated, it would appear first.
func TestHandleReboot_WaitFalseIgnoresTimeout(t *testing.T) {
	h := safeH()
	ctx := context.Background()

	_, _, err := h.HandleReboot(ctx, nil, RebootArgs{
		Nodes:   []string{"192.168.2.61"},
		Confirm: true,
		Wait:    false,
		Timeout: "not-a-duration",
		Mode:    "bad-mode",
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if strings.Contains(err.Error(), "invalid timeout") {
		t.Errorf("wait=false should not validate timeout, but got: %v", err)
	}
	if !strings.Contains(err.Error(), "unknown mode") {
		t.Errorf("expected 'unknown mode' error, got: %v", err)
	}
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

// TestHandleApplyConfig_Guards verifies that talos_apply_config rejects invalid inputs
// before reaching the gRPC layer.
func TestHandleApplyConfig_Guards(t *testing.T) {
	h := safeH()
	ctx := context.Background()
	dryRunFalse := false

	tests := []struct {
		name    string
		args    ApplyConfigArgs
		wantErr string
	}{
		{
			name:    "empty config rejected",
			args:    ApplyConfigArgs{Config: "", Nodes: []string{"10.0.0.1"}},
			wantErr: "requires a non-empty config",
		},
		{
			name:    "multi-node rejected",
			args:    ApplyConfigArgs{Config: "machine: {}", Nodes: []string{"10.0.0.1", "10.0.0.2"}},
			wantErr: "exactly one target node",
		},
		{
			name: "dry_run=false without confirm rejected",
			args: ApplyConfigArgs{
				Config:  "machine: {}",
				DryRun:  &dryRunFalse,
				Confirm: false,
			},
			wantErr: "confirm must be explicitly set to true when dry_run is false",
		},
		{
			name: "unknown mode rejected",
			args: ApplyConfigArgs{
				Config: "machine: {}",
				Mode:   "turbo",
			},
			wantErr: "unknown mode",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := h.HandleApplyConfig(ctx, nil, tt.args)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("expected error containing %q, got: %v", tt.wantErr, err)
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
