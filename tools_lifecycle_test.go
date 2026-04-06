package main

import (
	"context"
	"strings"
	"testing"
)

// safeTC returns a TalosClient with a nil inner client.
// Safe to use only for test cases that return before touching tc.client.
func safeTC() *TalosClient {
	return &TalosClient{}
}

// TestHandleReboot_Guards verifies that reboot is rejected without confirm or without nodes.
func TestHandleReboot_Guards(t *testing.T) {
	tc := safeTC()
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
			_, _, err := tc.handleReboot(ctx, nil, tt.args)
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
	tc := safeTC()
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
			_, _, err := tc.handleUpgrade(ctx, nil, tt.args)
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
	tc := safeTC()
	ctx := context.Background()

	_, _, err := tc.handleServiceAction(ctx, nil, ServiceActionArgs{
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
	tc := safeTC()
	ctx := context.Background()

	_, _, err := tc.handlePatchConfig(ctx, nil, PatchConfigArgs{
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
