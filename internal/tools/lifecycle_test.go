package tools

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cosi-project/runtime/api/v1alpha1"
	"github.com/cosi-project/runtime/pkg/resource"
	cosiprotobuf "github.com/cosi-project/runtime/pkg/resource/protobuf"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"

	"github.com/Nosmoht/talos-mcp-server/internal/talos"
)

// safeH returns a Handlers with a mock Talos client.
// The mock panics if any gRPC method is called, so it is safe to use only
// for test cases whose guard logic returns before touching the client.
func safeH() *Handlers {
	return &Handlers{Client: &mockClient{}}
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

// TestHandleServiceAction_NoConfirm verifies the confirm guard rejects calls
// that omit confirm=true, before any other validation runs.
func TestHandleServiceAction_NoConfirm(t *testing.T) {
	h := safeH()
	ctx := context.Background()

	_, _, err := h.HandleServiceAction(ctx, nil, ServiceActionArgs{
		ServiceName: "kubelet",
		Action:      "restart",
		Confirm:     false,
	})
	if err == nil {
		t.Fatal("expected error for missing confirm, got nil")
	}
	if !strings.Contains(err.Error(), "confirm must be explicitly set to true") {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestHandleServiceAction_EmptyServiceName verifies empty service name is rejected.
func TestHandleServiceAction_EmptyServiceName(t *testing.T) {
	h := safeH()
	ctx := context.Background()

	_, _, err := h.HandleServiceAction(ctx, nil, ServiceActionArgs{
		Action:  "restart",
		Confirm: true,
	})
	if err == nil {
		t.Fatal("expected error for empty service_name, got nil")
	}
	if !strings.Contains(err.Error(), "service_name is required") {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestHandleServiceAction_InvalidAction verifies unknown actions are rejected.
func TestHandleServiceAction_InvalidAction(t *testing.T) {
	h := safeH()
	ctx := context.Background()

	_, _, err := h.HandleServiceAction(ctx, nil, ServiceActionArgs{
		ServiceName: "kubelet",
		Action:      "obliterate",
		Confirm:     true,
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

	// Create a valid config file used by test cases that need to pass the file guard.
	validFile := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(validFile, []byte("machine: {}"), 0o600); err != nil {
		t.Fatalf("setup: write temp config: %v", err)
	}

	tests := []struct {
		name    string
		args    ApplyConfigArgs
		wantErr string
	}{
		{
			name:    "empty config_file rejected",
			args:    ApplyConfigArgs{ConfigFile: "", Nodes: []string{"10.0.0.1"}},
			wantErr: "must be an absolute path",
		},
		{
			name:    "multi-node rejected",
			args:    ApplyConfigArgs{ConfigFile: validFile, Nodes: []string{"10.0.0.1", "10.0.0.2"}},
			wantErr: "exactly one target node",
		},
		{
			name: "dry_run=false without confirm rejected",
			args: ApplyConfigArgs{
				ConfigFile: validFile,
				DryRun:     &dryRunFalse,
				Confirm:    false,
			},
			wantErr: "confirm must be explicitly set to true when dry_run is false",
		},
		{
			name: "unknown mode rejected",
			args: ApplyConfigArgs{
				ConfigFile: validFile,
				Mode:       "turbo",
			},
			wantErr: "unknown mode",
		},
		{
			name:    "relative config_file rejected",
			args:    ApplyConfigArgs{ConfigFile: "relative/path/config.yaml"},
			wantErr: "must be an absolute path",
		},
		{
			name:    "path traversal rejected",
			args:    ApplyConfigArgs{ConfigFile: "/tmp/../tmp/config.yaml"},
			wantErr: "must not contain ..",
		},
		{
			name:    "nonexistent config_file rejected",
			args:    ApplyConfigArgs{ConfigFile: "/tmp/talos-test-does-not-exist-abc123.yaml"},
			wantErr: "no such file",
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

// TestHandleApplyConfig_ConfigFile verifies config_file-specific guard checks that
// require real filesystem state (actual files on disk).
func TestHandleApplyConfig_ConfigFile(t *testing.T) {
	h := safeH()
	ctx := context.Background()

	// Oversized file: maxConfigFileSize+1 bytes.
	oversizedFile := filepath.Join(t.TempDir(), "oversized.yaml")
	if err := os.WriteFile(oversizedFile, make([]byte, maxConfigFileSize+1), 0o600); err != nil {
		t.Fatalf("setup: write oversized file: %v", err)
	}

	// Directory: pass a directory path — should be rejected as not a regular file.
	dirPath := t.TempDir()

	// Symlink: points to a valid config file — should be rejected.
	target := filepath.Join(t.TempDir(), "real.yaml")
	if err := os.WriteFile(target, []byte("machine: {}"), 0o600); err != nil {
		t.Fatalf("setup: write symlink target: %v", err)
	}
	symlinkPath := filepath.Join(t.TempDir(), "link.yaml")
	if err := os.Symlink(target, symlinkPath); err != nil {
		t.Fatalf("setup: create symlink: %v", err)
	}

	// Valid file: passes all guards and reaches the (nil) gRPC client.
	validFile := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(validFile, []byte("machine: {}"), 0o600); err != nil {
		t.Fatalf("setup: write valid config: %v", err)
	}

	tests := []struct {
		name    string
		args    ApplyConfigArgs
		wantErr string
	}{
		{
			name:    "oversized file rejected",
			args:    ApplyConfigArgs{ConfigFile: oversizedFile},
			wantErr: "exceeds maximum size",
		},
		{
			name:    "directory rejected",
			args:    ApplyConfigArgs{ConfigFile: dirPath},
			wantErr: "not a regular file",
		},
		{
			name:    "symlink rejected",
			args:    ApplyConfigArgs{ConfigFile: symlinkPath},
			wantErr: "not a regular file",
		},
		{
			name:    "valid file passes guards",
			args:    ApplyConfigArgs{ConfigFile: validFile},
			wantErr: "", // guard passes; mock panics on gRPC call — expect a non-guard error
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.wantErr == "" {
				// Valid case: expect the call to panic or fail at the gRPC layer,
				// not at guard validation. Catching the panic confirms guards passed.
				defer func() {
					if r := recover(); r == nil {
						t.Error("expected panic from mock client on gRPC call, got none")
					}
				}()
				_, _, _ = h.HandleApplyConfig(ctx, nil, tt.args)
				return
			}
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

// TestReadConfigFile exercises the readConfigFile helper directly.
func TestReadConfigFile(t *testing.T) {
	// Valid file: should return content unchanged.
	validDir := t.TempDir()
	validPath := filepath.Join(validDir, "config.yaml")
	content := []byte("machine: {}")
	if err := os.WriteFile(validPath, content, 0o600); err != nil {
		t.Fatalf("setup: %v", err)
	}

	// Oversized file.
	oversizedPath := filepath.Join(t.TempDir(), "big.yaml")
	if err := os.WriteFile(oversizedPath, make([]byte, maxConfigFileSize+1), 0o600); err != nil {
		t.Fatalf("setup: %v", err)
	}

	// Symlink to valid file.
	symlinkPath := filepath.Join(t.TempDir(), "link.yaml")
	if err := os.Symlink(validPath, symlinkPath); err != nil {
		t.Fatalf("setup: %v", err)
	}

	tests := []struct {
		name    string
		path    string
		wantErr string
	}{
		{
			name: "valid file",
			path: validPath,
		},
		{
			name:    "relative path",
			path:    "config.yaml",
			wantErr: "must be an absolute path",
		},
		{
			name:    "path with ..",
			path:    validDir + "/../" + filepath.Base(validDir) + "/config.yaml",
			wantErr: "must not contain ..",
		},
		{
			name:    "nonexistent",
			path:    "/tmp/talos-test-readconfigfile-nonexistent.yaml",
			wantErr: "no such file",
		},
		{
			name:    "directory",
			path:    validDir,
			wantErr: "not a regular file",
		},
		{
			name:    "symlink",
			path:    symlinkPath,
			wantErr: "not a regular file",
		},
		{
			name:    "oversized",
			path:    oversizedPath,
			wantErr: "exceeds maximum size",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := readConfigFile(tt.path)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if string(data) != string(content) {
					t.Errorf("got %q, want %q", data, content)
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

// TestResolveGraceful verifies the graceful default behaviour.
func TestResolveGraceful(t *testing.T) {
	f := false
	tr := true

	tests := []struct {
		name string
		in   *bool
		want bool
	}{
		{"nil means graceful", nil, true},
		{"explicit true is graceful", &tr, true},
		{"explicit false skips graceful", &f, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveGraceful(tt.in)
			if got != tt.want {
				t.Errorf("resolveGraceful(%v) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

// TestHandleReset_Guards verifies that reset is rejected without confirm or without nodes.
func TestHandleReset_Guards(t *testing.T) {
	h := safeH()
	ctx := context.Background()

	tests := []struct {
		name    string
		args    ResetArgs
		wantErr string
	}{
		{
			name:    "no confirm",
			args:    ResetArgs{Nodes: []string{"192.168.2.61"}, Confirm: false},
			wantErr: "confirm must be explicitly set to true",
		},
		{
			name:    "no nodes",
			args:    ResetArgs{Nodes: nil, Confirm: true},
			wantErr: "nodes must be explicitly specified",
		},
		{
			name:    "empty nodes",
			args:    ResetArgs{Nodes: []string{}, Confirm: true},
			wantErr: "nodes must be explicitly specified",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := h.HandleReset(ctx, nil, tt.args)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error %q does not contain %q", err.Error(), tt.wantErr)
			}
		})
	}
}

// --- extractMachineConfigBody tests ---

// simpleResource is a minimal resource.Resource implementation used in tests
// for the non-protobuf code paths of extractMachineConfigBody.
type simpleResource struct {
	md   resource.Metadata
	spec any
}

func (r *simpleResource) Metadata() *resource.Metadata { return &r.md }
func (r *simpleResource) Spec() any                    { return r.spec }

// DeepCopy returns the receiver unchanged — intentional shallow copy,
// safe because these tests only read the resource.
func (r *simpleResource) DeepCopy() resource.Resource { return r }

// newProtoResource builds a *cosiprotobuf.Resource with no annotations and
// the given YAML spec string, simulating a legacy Talos MachineConfig resource.
func newProtoResource(yamlSpec string) *cosiprotobuf.Resource {
	res, err := cosiprotobuf.Unmarshal(&v1alpha1.Resource{
		Metadata: &v1alpha1.Metadata{
			Namespace: "config",
			Type:      "MachineConfigs.config.talos.dev",
			Id:        "v1alpha1",
			Version:   "1",
			Phase:     "running",
		},
		Spec: &v1alpha1.Spec{YamlSpec: yamlSpec},
	})
	if err != nil {
		panic("newProtoResource: " + err.Error())
	}
	return res
}

// newSimpleResource builds a simpleResource with empty annotations.
func newSimpleResource(spec any) *simpleResource {
	md := resource.NewMetadata("config", "MachineConfigs.config.talos.dev", "v1alpha1", resource.VersionUndefined)
	return &simpleResource{md: md, spec: spec}
}

// newAnnotatedResource builds a simpleResource with one annotation set,
// simulating the current Talos MachineConfig format where spec is a YAML string.
func newAnnotatedResource(spec any) *simpleResource {
	md := resource.NewMetadata("config", "MachineConfigs.config.talos.dev", "v1alpha1", resource.VersionUndefined)
	md.Annotations().Set("config.talos.dev/hash", "abc123")
	return &simpleResource{md: md, spec: spec}
}

// TestExtractMachineConfigBody covers both code paths of the function and
// selected error branches.
func TestExtractMachineConfigBody(t *testing.T) {
	const machineConfigYAML = "version: v1alpha1\nmachine:\n  type: controlplane\n"

	t.Run("legacy protobuf path returns yaml spec", func(t *testing.T) {
		res := newProtoResource(machineConfigYAML)

		if !res.Metadata().Annotations().Empty() {
			t.Fatal("fixture must have empty annotations to exercise legacy path")
		}

		got, err := extractMachineConfigBody(res)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if string(got) != machineConfigYAML {
			t.Errorf("got %q, want %q", string(got), machineConfigYAML)
		}
	})

	t.Run("legacy protobuf path with empty yaml spec returns empty bytes", func(t *testing.T) {
		res := newProtoResource("")

		got, err := extractMachineConfigBody(res)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got) != 0 {
			t.Errorf("expected empty bytes for empty yaml spec, got %q", got)
		}
	})

	t.Run("legacy fallback path yaml-marshals spec", func(t *testing.T) {
		// Spec is a plain map — not a *cosiprotobuf.Resource — so the function falls
		// through to yaml.Marshal(mc.Spec()).
		specMap := map[string]any{"version": "v1alpha1", "machine": map[string]any{"type": "controlplane"}}
		res := newSimpleResource(specMap)

		got, err := extractMachineConfigBody(res)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got) == 0 {
			t.Error("expected non-empty YAML output from fallback path")
		}
		if !strings.Contains(string(got), "v1alpha1") {
			t.Errorf("marshaled YAML does not contain expected version key, got: %s", got)
		}
	})

	t.Run("current path unwraps YAML string envelope", func(t *testing.T) {
		// In the current Talos format, Spec() returns a Go string. yaml.Marshal of a
		// Go string produces a quoted YAML scalar; yaml.Unmarshal back into a Go string
		// yields the original value — this is the envelope-unwrap in extractMachineConfigBody.
		res := newAnnotatedResource(machineConfigYAML)

		got, err := extractMachineConfigBody(res)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if string(got) != machineConfigYAML {
			t.Errorf("got %q, want %q", string(got), machineConfigYAML)
		}
	})

	t.Run("current path errors when spec cannot be unmarshaled as string", func(t *testing.T) {
		// If Spec() returns a struct (not a string), yaml.Unmarshal into a string
		// must fail — the function must propagate the error rather than silently
		// return garbage bytes.
		// Note: the pb.Marshal() error branch (lifecycle.go:729) is not covered here
		// because protobuf.Resource.Marshal() always returns nil error (cosi-project/runtime).
		type nonStringSpec struct{ X int }
		res := newAnnotatedResource(nonStringSpec{X: 42})

		_, err := extractMachineConfigBody(res)
		if err == nil {
			t.Error("expected error when spec cannot be unmarshaled as string, got nil")
		}
	})
}

// TestNodePatchMu_SameKeyReturnsSameMutex verifies that repeated calls for the same
// node key return the identical *sync.Mutex (lazy creation + identity guarantee).
func TestNodePatchMu_SameKeyReturnsSameMutex(t *testing.T) {
	h := safeH()
	m1 := h.nodePatchMu("10.0.0.1")
	m2 := h.nodePatchMu("10.0.0.1")
	if m1 != m2 {
		t.Error("expected same mutex instance for same node key, got different pointers")
	}
}

// TestNodePatchMu_DifferentKeysHaveDifferentMutexes verifies per-node isolation:
// two distinct node keys must not share a mutex.
func TestNodePatchMu_DifferentKeysHaveDifferentMutexes(t *testing.T) {
	h := safeH()
	m1 := h.nodePatchMu("10.0.0.1")
	m2 := h.nodePatchMu("10.0.0.2")
	if m1 == m2 {
		t.Error("expected distinct mutex instances for different node keys, got same pointer")
	}
}

// TestNodePatchMu_Serializes verifies that only one goroutine holds the per-node
// mutex at a time. Run with -race to also detect data races on the counter.
func TestNodePatchMu_Serializes(t *testing.T) {
	h := safeH()
	const node = "10.0.0.1"
	const goroutines = 20

	var (
		counter int // protected by the per-node mutex
		maxSeen int
		maxMu   sync.Mutex
		wg      sync.WaitGroup
	)

	for range goroutines {
		wg.Add(1)
		go func() {
			defer wg.Done()
			mu := h.nodePatchMu(node)
			mu.Lock()
			defer mu.Unlock()

			// Increment, yield, then decrement — if two goroutines hold the lock
			// concurrently, counter would reach 2. Under correct serialisation it
			// stays at 1 for the entire critical section.
			counter++
			current := counter
			maxMu.Lock()
			if current > maxSeen {
				maxSeen = current
			}
			maxMu.Unlock()
			counter--
		}()
	}

	wg.Wait()

	if maxSeen != 1 {
		t.Errorf("concurrent lock holders: got max %d, want 1", maxSeen)
	}
}

// TestHandleApplyConfig_InsecureGuards verifies that talos_apply_config rejects
// invalid insecure-mode inputs at the gate (before any dial). The mock client
// panics on ApplyConfiguration, so reaching the gRPC layer would surface as a
// test failure. Each case targets one specific gate.
func TestHandleApplyConfig_InsecureGuards(t *testing.T) {
	ctx := context.Background()

	validFile := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(validFile, []byte("machine: {}"), 0o600); err != nil {
		t.Fatalf("setup: write temp config: %v", err)
	}
	allowlist, err := talos.ParseNodeAllowlist("192.0.2.5,2001:db8::1")
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
		args    ApplyConfigArgs
		wantErr string
	}{
		{
			name: "cert_fingerprint without insecure rejected",
			h:    safeH(),
			args: ApplyConfigArgs{
				ConfigFile:      validFile,
				CertFingerprint: strings.Repeat("ab", 32),
			},
			wantErr: "cert_fingerprint requires insecure=true",
		},
		{
			name: "insecure without enable_insecure rejected",
			h:    safeH(),
			args: ApplyConfigArgs{
				ConfigFile: validFile,
				Insecure:   true,
				Endpoint:   "192.0.2.5",
			},
			wantErr: "TALOS_MCP_ENABLE_INSECURE",
		},
		{
			name: "insecure with nodes mutually exclusive",
			h:    enabledH(),
			args: ApplyConfigArgs{
				ConfigFile: validFile,
				Insecure:   true,
				Endpoint:   "192.0.2.5",
				Nodes:      []string{"192.0.2.6"},
			},
			wantErr: "mutually exclusive",
		},
		{
			name: "insecure empty endpoint",
			h:    enabledH(),
			args: ApplyConfigArgs{
				ConfigFile: validFile,
				Insecure:   true,
			},
			wantErr: "endpoint is required",
		},
		{
			name: "insecure non-IP endpoint",
			h:    enabledH(),
			args: ApplyConfigArgs{
				ConfigFile: validFile,
				Insecure:   true,
				Endpoint:   "node-1.example.com",
			},
			wantErr: "not a bare IP",
		},
		{
			name: "insecure loopback endpoint rejected",
			h:    enabledH(),
			args: ApplyConfigArgs{
				ConfigFile: validFile,
				Insecure:   true,
				Endpoint:   "127.0.0.1",
			},
			wantErr: "loopback",
		},
		{
			name: "insecure link-local IMDS rejected",
			h:    enabledH(),
			args: ApplyConfigArgs{
				ConfigFile: validFile,
				Insecure:   true,
				Endpoint:   "169.254.169.254",
			},
			wantErr: "link-local",
		},
		{
			name: "insecure endpoint not in allowlist",
			h:    enabledH(),
			args: ApplyConfigArgs{
				ConfigFile: validFile,
				Insecure:   true,
				Endpoint:   "192.0.2.99",
			},
			wantErr: "not in TALOS_MCP_INSECURE_ALLOWED_NODES",
		},
		{
			name: "insecure invalid fingerprint",
			h:    enabledH(),
			args: ApplyConfigArgs{
				ConfigFile:      validFile,
				Insecure:        true,
				Endpoint:        "192.0.2.5",
				CertFingerprint: "not-hex-not-64-chars",
			},
			wantErr: "exactly 64 hex",
		},
		{
			name: "insecure confirm required when dry_run=false",
			h:    enabledH(),
			args: func() ApplyConfigArgs {
				dryRunFalse := false
				return ApplyConfigArgs{
					ConfigFile: validFile,
					Insecure:   true,
					Endpoint:   "192.0.2.5",
					DryRun:     &dryRunFalse,
					Confirm:    false,
				}
			}(),
			wantErr: "confirm must be explicitly set to true",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := tt.h.HandleApplyConfig(ctx, nil, tt.args)
			if err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("expected error containing %q, got: %v", tt.wantErr, err)
			}
		})
	}
}

// TestHandleApplyConfig_InsecureBlocklistBypassed verifies that the
// authenticated-path blocklist (TALOS_MCP_BLOCKED_CONFIG_PATHS) does NOT gate
// insecure-mode calls. Bootstrap of a fresh node has no cluster state to
// protect; the blocklist is a post-bootstrap control. This is documented as
// intentional in the design plan and the auditOutcome carries the bypass.
//
// We inject a short context deadline so the test fails fast at the dial step
// (the test endpoint is TEST-NET-1, unreachable by construction). Without the
// deadline, the 30s default dialTimeout would dominate test runtime.
func TestHandleApplyConfig_InsecureBlocklistBypassed(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	validFile := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(validFile, []byte("machine: {}"), 0o600); err != nil {
		t.Fatalf("setup: write temp config: %v", err)
	}
	allowlist, err := talos.ParseNodeAllowlist("192.0.2.5")
	if err != nil {
		t.Fatalf("setup: parse allowlist: %v", err)
	}

	h := &Handlers{
		Client:               &mockClient{},
		EnableInsecure:       true,
		InsecureAllowedNodes: allowlist,
		// Authenticated path would be refused unconditionally here:
		BlockedConfigPaths: []string{"machine.security"},
	}

	dryRunFalse := false
	args := ApplyConfigArgs{
		ConfigFile: validFile,
		Insecure:   true,
		Endpoint:   "192.0.2.5",
		DryRun:     &dryRunFalse,
		Confirm:    true,
	}

	// Reaches the dial step; the test endpoint is unreachable so the expected
	// outcome is a dial/timeout error rather than a blocklist refusal.
	_, _, err = h.HandleApplyConfig(ctx, nil, args)
	if err == nil {
		t.Fatalf("expected dial error, got nil")
	}
	if strings.Contains(err.Error(), "TALOS_MCP_BLOCKED_CONFIG_PATHS") {
		t.Errorf("blocklist refused insecure call, but bypass is documented as intentional: %v", err)
	}
}

// TestHandleReboot_ModeReachesClient: each talos_reboot mode value (RebootArgs.Mode
// schema) reaches the client as the matching machinery reboot mode.
func TestHandleReboot_ModeReachesClient(t *testing.T) {
	t.Parallel()

	tests := []struct {
		mode string
		want machineapi.RebootRequest_Mode
	}{
		{mode: "", want: machineapi.RebootRequest_DEFAULT},
		{mode: "default", want: machineapi.RebootRequest_DEFAULT},
		{mode: "powercycle", want: machineapi.RebootRequest_POWERCYCLE},
		{mode: "force", want: machineapi.RebootRequest_FORCE},
	}

	for _, tc := range tests {
		t.Run("mode="+tc.mode, func(t *testing.T) {
			t.Parallel()

			m := &mockClient{recordReboot: true, gotRebootMode: -1}
			h := safeH()
			h.Client = m

			if _, _, err := h.HandleReboot(context.Background(), nil, RebootArgs{
				Nodes:   []string{insecureTestEndpoint},
				Confirm: true,
				Mode:    tc.mode,
			}); err != nil {
				t.Fatalf("talos_reboot mode=%q: unexpected error %v", tc.mode, err)
			}
			if m.gotRebootMode != tc.want {
				t.Errorf("RebootArgs.Mode schema: mode=%q want %v got %v", tc.mode, tc.want, m.gotRebootMode)
			}
		})
	}
}

// TestHandleUpgrade_RequestReachesClient: talos_upgrade arguments reach the client
// unchanged, an omitted preserve becomes Preserve=true (CLAUDE.md §Safety), and each
// reboot_mode value (UpgradeArgs.RebootMode schema) maps to the machinery enum.
func TestHandleUpgrade_RequestReachesClient(t *testing.T) {
	t.Parallel()

	const image = "ghcr.io/siderolabs/installer:v1.13.0"
	preserveFalse := false

	type upgradeFields struct {
		Image      string
		Preserve   bool
		Stage      bool
		Force      bool
		RebootMode machineapi.UpgradeRequest_RebootMode
	}

	tests := []struct {
		name string
		args UpgradeArgs
		want upgradeFields
	}{
		{
			name: "preserve omitted defaults to true",
			args: UpgradeArgs{Image: image},
			want: upgradeFields{Image: image, Preserve: true, RebootMode: machineapi.UpgradeRequest_DEFAULT},
		},
		{
			name: "preserve false is kept",
			args: UpgradeArgs{Image: image, Preserve: &preserveFalse},
			want: upgradeFields{Image: image, Preserve: false, RebootMode: machineapi.UpgradeRequest_DEFAULT},
		},
		{
			name: "reboot_mode default",
			args: UpgradeArgs{Image: image, RebootMode: "default"},
			want: upgradeFields{Image: image, Preserve: true, RebootMode: machineapi.UpgradeRequest_DEFAULT},
		},
		{
			name: "reboot_mode powercycle with stage and force",
			args: UpgradeArgs{Image: image, RebootMode: "powercycle", Stage: true, Force: true},
			want: upgradeFields{Image: image, Preserve: true, Stage: true, Force: true, RebootMode: machineapi.UpgradeRequest_POWERCYCLE},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			m := &mockClient{recordUpgrade: true}
			h := safeH()
			h.Client = m
			h.SkipVersionCheck = true

			args := tc.args
			args.Nodes = []string{insecureTestEndpoint}
			args.Confirm = true
			if _, _, err := h.HandleUpgrade(context.Background(), nil, args); err != nil {
				t.Fatalf("talos_upgrade %s: unexpected error %v", tc.name, err)
			}
			if m.gotUpgradeReq == nil {
				t.Fatalf("talos_upgrade %s: client never received an upgrade request", tc.name)
			}
			r := m.gotUpgradeReq
			got := upgradeFields{r.GetImage(), r.GetPreserve(), r.GetStage(), r.GetForce(), r.GetRebootMode()}
			if got != tc.want {
				t.Errorf("CLAUDE.md §Safety / UpgradeArgs schema: want %+v got %+v", tc.want, got)
			}
		})
	}
}

// TestHandleApplyConfig_InsecureRequestReachesDialedClient: in maintenance mode
// the config bytes, apply mode and dry-run flag (default true, CLAUDE.md §Safety;
// .claude/rules/insecure-mode.md §Confirm semantics) reach the dialed client.
func TestHandleApplyConfig_InsecureRequestReachesDialedClient(t *testing.T) {
	t.Parallel()

	const configBody = "machine: {}"
	configFile := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configFile, []byte(configBody), 0o600); err != nil {
		t.Fatalf("setup: write temp config: %v", err)
	}
	dryRunFalse := false

	type applyFields struct {
		Data   string
		Mode   machineapi.ApplyConfigurationRequest_Mode
		DryRun bool
	}

	tests := []struct {
		name string
		args ApplyConfigArgs
		want applyFields
	}{
		{
			name: "dry_run and mode omitted",
			args: ApplyConfigArgs{},
			want: applyFields{Data: configBody, Mode: machineapi.ApplyConfigurationRequest_AUTO, DryRun: true},
		},
		{
			name: "no_reboot dry run",
			args: ApplyConfigArgs{Mode: "no_reboot"},
			want: applyFields{Data: configBody, Mode: machineapi.ApplyConfigurationRequest_NO_REBOOT, DryRun: true},
		},
		{
			name: "staged apply with confirm",
			args: ApplyConfigArgs{Mode: "staged", DryRun: &dryRunFalse, Confirm: true},
			want: applyFields{Data: configBody, Mode: machineapi.ApplyConfigurationRequest_STAGED, DryRun: false},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			fake := &fakeMaintenanceClient{}
			h := insecureHandlers(t, &recordingDialer{client: fake})

			args := tc.args
			args.ConfigFile = configFile
			args.Insecure = true
			args.Endpoint = insecureTestEndpoint
			if _, _, err := h.HandleApplyConfig(context.Background(), nil, args); err != nil {
				t.Fatalf("talos_apply_config insecure %s: unexpected error %v", tc.name, err)
			}
			if fake.gotApply == nil {
				t.Fatalf("talos_apply_config insecure %s: dialed client never received ApplyConfiguration", tc.name)
			}
			got := applyFields{string(fake.gotApply.GetData()), fake.gotApply.GetMode(), fake.gotApply.GetDryRun()}
			if got != tc.want {
				t.Errorf("insecure-mode.md §Confirm semantics / ApplyConfigArgs schema: want %+v got %+v", tc.want, got)
			}
		})
	}
}

// TestHandleApplyConfig_InsecureOutcomeAudited: after the dial, a successful
// RPC is audited as ok and a failed RPC as rpc-error and returned as an error
// (.claude/rules/insecure-mode.md §Audit pairing).
func TestHandleApplyConfig_InsecureOutcomeAudited(t *testing.T) {
	configFile := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configFile, []byte("machine: {}"), 0o600); err != nil {
		t.Fatalf("setup: write temp config: %v", err)
	}

	tests := []struct {
		name        string
		rpcErr      error
		wantOutcome string
	}{
		{name: "rpc success", rpcErr: nil, wantOutcome: OutcomeOK},
		{name: "rpc failure", rpcErr: errors.New("maintenance node rejected config"), wantOutcome: OutcomeRPCError},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			logBuf := captureDefaultSlog(t)
			h := insecureHandlers(t, &recordingDialer{client: &fakeMaintenanceClient{rpcErr: tc.rpcErr}})

			_, _, err := h.HandleApplyConfig(context.Background(), nil, ApplyConfigArgs{
				ConfigFile: configFile,
				Insecure:   true,
				Endpoint:   insecureTestEndpoint,
			})
			if (err != nil) != (tc.rpcErr != nil) {
				t.Fatalf("insecure-mode.md §Audit pairing: RPC error %v must surface as handler error, got %v", tc.rpcErr, err)
			}
			if got := auditOutcomeOf(logBuf.String(), "talos_apply_config"); got != tc.wantOutcome {
				t.Errorf("insecure-mode.md §Audit pairing: AUDIT_OUTCOME outcome want %q got %q", tc.wantOutcome, got)
			}
		})
	}
}
