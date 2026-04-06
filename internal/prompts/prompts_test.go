package prompts

import (
	"context"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// makeReq builds a GetPromptRequest for testing.
func makeReq(name string, args map[string]string) *mcp.GetPromptRequest {
	return &mcp.GetPromptRequest{
		Params: &mcp.GetPromptParams{
			Name:      name,
			Arguments: args,
		},
	}
}

func TestHandleDiagnoseNode(t *testing.T) {
	tests := []struct {
		name    string
		args    map[string]string
		wantErr string
		wantIn  string // substring expected in successful result
	}{
		{"missing node", map[string]string{}, `requires argument "node"`, ""},
		{"empty node", map[string]string{"node": ""}, `requires argument "node"`, ""},
		{"valid", map[string]string{"node": "10.0.0.1"}, "", "10.0.0.1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := handleDiagnoseNode(context.Background(), makeReq("diagnose-node", tt.args))
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %v, want containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.wantIn != "" && !strings.Contains(res.Messages[0].Content.(*mcp.TextContent).Text, tt.wantIn) {
				t.Errorf("result does not contain %q", tt.wantIn)
			}
		})
	}
}

func TestHandlePreUpgrade(t *testing.T) {
	tests := []struct {
		name    string
		args    map[string]string
		wantErr string
		wantIn  string
	}{
		{"missing target_version", map[string]string{}, `requires argument "target_version"`, ""},
		{"empty target_version", map[string]string{"target_version": ""}, `requires argument "target_version"`, ""},
		{"valid no nodes", map[string]string{"target_version": "v1.9.0"}, "", "v1.9.0"},
		{"valid with nodes", map[string]string{"target_version": "v1.9.0", "nodes": "10.0.0.1,10.0.0.2"}, "", "10.0.0.1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := handlePreUpgrade(context.Background(), makeReq("pre-upgrade-checklist", tt.args))
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %v, want containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.wantIn != "" && !strings.Contains(res.Messages[0].Content.(*mcp.TextContent).Text, tt.wantIn) {
				t.Errorf("result does not contain %q", tt.wantIn)
			}
		})
	}
}

func TestHandleInvestigateEtcd(t *testing.T) {
	tests := []struct {
		name    string
		args    map[string]string
		wantErr string
		wantIn  string
	}{
		{"no args — valid", map[string]string{}, "", "active talosconfig"},
		{"with node", map[string]string{"node": "10.0.0.1"}, "", "10.0.0.1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := handleInvestigateEtcd(context.Background(), makeReq("investigate-etcd", tt.args))
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %v, want containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.wantIn != "" && !strings.Contains(res.Messages[0].Content.(*mcp.TextContent).Text, tt.wantIn) {
				t.Errorf("result does not contain %q", tt.wantIn)
			}
		})
	}
}

func TestHandleDebugService(t *testing.T) {
	tests := []struct {
		name    string
		args    map[string]string
		wantErr string
		wantIn  string
	}{
		{"missing service", map[string]string{"node": "10.0.0.1"}, `requires argument "service"`, ""},
		{"missing node", map[string]string{"service": "kubelet"}, `requires argument "node"`, ""},
		{"invalid tail_lines non-numeric", map[string]string{"service": "kubelet", "node": "10.0.0.1", "tail_lines": "abc"}, "tail_lines must be a positive integer", ""},
		{"invalid tail_lines zero", map[string]string{"service": "kubelet", "node": "10.0.0.1", "tail_lines": "0"}, "tail_lines must be a positive integer", ""},
		{"invalid tail_lines negative", map[string]string{"service": "kubelet", "node": "10.0.0.1", "tail_lines": "-5"}, "tail_lines must be a positive integer", ""},
		{"valid defaults", map[string]string{"service": "kubelet", "node": "10.0.0.1"}, "", "kubelet"},
		{"valid custom tail_lines", map[string]string{"service": "etcd", "node": "10.0.0.1", "tail_lines": "500"}, "", "500"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := handleDebugService(context.Background(), makeReq("debug-service", tt.args))
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %v, want containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.wantIn != "" && !strings.Contains(res.Messages[0].Content.(*mcp.TextContent).Text, tt.wantIn) {
				t.Errorf("result does not contain %q", tt.wantIn)
			}
		})
	}
}

func TestHandleApplyConfig(t *testing.T) {
	tests := []struct {
		name    string
		args    map[string]string
		wantErr string
		wantIn  string
	}{
		{"missing patch", map[string]string{"node": "10.0.0.1"}, `requires argument "patch"`, ""},
		{"missing node", map[string]string{"patch": "{}"}, `requires argument "node"`, ""},
		{"invalid mode", map[string]string{"patch": "{}", "node": "10.0.0.1", "mode": "bad"}, `unknown mode "bad"`, ""},
		{"valid defaults", map[string]string{"patch": `{"machine":{}}`, "node": "10.0.0.1"}, "", "10.0.0.1"},
		{"valid mode staged", map[string]string{"patch": `{"machine":{}}`, "node": "10.0.0.1", "mode": "staged"}, "", "staged"},
		{"valid mode reboot", map[string]string{"patch": `{"machine":{}}`, "node": "10.0.0.1", "mode": "reboot"}, "", "reboot"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := handleApplyConfig(context.Background(), makeReq("apply-config", tt.args))
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %v, want containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.wantIn != "" && !strings.Contains(res.Messages[0].Content.(*mcp.TextContent).Text, tt.wantIn) {
				t.Errorf("result does not contain %q", tt.wantIn)
			}
		})
	}
}

func TestRegister_NoPanic(t *testing.T) {
	server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.0.0"}, nil)
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Register panicked: %v", r)
		}
	}()
	Register(server, false) // readOnly=false registers all 5
	Register(server, true)  // readOnly=true skips apply-config (will replace existing)
}
