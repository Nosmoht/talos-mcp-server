package tools

import (
	"context"
	"strings"
	"testing"
)

// TestHandleValidate_Guards verifies input validation before any config parsing.
func TestHandleValidate_Guards(t *testing.T) {
	h := safeH()
	ctx := context.Background()

	tests := []struct {
		name    string
		args    ValidateArgs
		wantErr string
	}{
		{
			name:    "empty config",
			args:    ValidateArgs{Config: ""},
			wantErr: "config must be specified",
		},
		{
			name:    "unknown mode",
			args:    ValidateArgs{Config: "version: v1alpha1", Mode: "baremetal"},
			wantErr: "unknown mode",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := h.HandleValidate(ctx, nil, tt.args)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error %q does not contain %q", err.Error(), tt.wantErr)
			}
		})
	}
}

// TestParseValidateMode verifies all supported mode strings and the default.
func TestParseValidateMode(t *testing.T) {
	tests := []struct {
		mode            string
		wantName        string
		wantInstall     bool
		wantInContainer bool
		wantErr         bool
	}{
		{mode: "", wantName: "metal", wantInstall: true, wantInContainer: false},
		{mode: "metal", wantName: "metal", wantInstall: true, wantInContainer: false},
		{mode: "cloud", wantName: "cloud", wantInstall: false, wantInContainer: false},
		{mode: "container", wantName: "container", wantInstall: false, wantInContainer: true},
		{mode: "unknown", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.mode, func(t *testing.T) {
			m, err := parseValidateMode(tt.mode)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if m.String() != tt.wantName {
				t.Errorf("String() = %q, want %q", m.String(), tt.wantName)
			}
			if m.RequiresInstall() != tt.wantInstall {
				t.Errorf("RequiresInstall() = %v, want %v", m.RequiresInstall(), tt.wantInstall)
			}
			if m.InContainer() != tt.wantInContainer {
				t.Errorf("InContainer() = %v, want %v", m.InContainer(), tt.wantInContainer)
			}
		})
	}
}

// TestHandleValidate_InvalidConfig verifies that a malformed config returns a parse error.
func TestHandleValidate_InvalidConfig(t *testing.T) {
	h := safeH()
	ctx := context.Background()

	// Not valid YAML/Talos config
	_, _, err := h.HandleValidate(ctx, nil, ValidateArgs{Config: "{{not valid yaml::"})
	if err == nil {
		t.Fatal("expected error for invalid YAML, got nil")
	}
	if !strings.Contains(err.Error(), "parse config") {
		t.Errorf("error %q does not contain 'parse config'", err.Error())
	}
}
