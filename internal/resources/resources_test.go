package resources

import (
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestParseCOSIURI(t *testing.T) {
	tests := []struct {
		name     string
		uri      string
		wantNode string
		wantNS   string
		wantType string
		wantID   string
		wantErr  bool
	}{
		{
			name:     "list - valid 2-segment path",
			uri:      "talos://10.0.0.1/resource/runtime/MachineStatus",
			wantNode: "10.0.0.1",
			wantNS:   "runtime",
			wantType: "MachineStatus",
		},
		{
			name:     "get - valid 3-segment path",
			uri:      "talos://10.0.0.1/resource/runtime/MachineStatuses.runtime.talos.dev/talos-cp-1",
			wantNode: "10.0.0.1",
			wantNS:   "runtime",
			wantType: "MachineStatuses.runtime.talos.dev",
			wantID:   "talos-cp-1",
		},
		{
			name:     "network namespace",
			uri:      "talos://192.168.1.10/resource/network/LinkStatuses.net.talos.dev/eth0",
			wantNode: "192.168.1.10",
			wantNS:   "network",
			wantType: "LinkStatuses.net.talos.dev",
			wantID:   "eth0",
		},
		{
			name:     "alias type",
			uri:      "talos://node1/resource/runtime/ms",
			wantNode: "node1",
			wantNS:   "runtime",
			wantType: "ms",
		},
		{
			name:     "ip with port - list",
			uri:      "talos://10.0.0.1:50000/resource/runtime/MachineStatus",
			wantNode: "10.0.0.1",
			wantNS:   "runtime",
			wantType: "MachineStatus",
		},
		{
			name:     "hostname with port - get",
			uri:      "talos://node1:50000/resource/network/LinkStatus/eth0",
			wantNode: "node1",
			wantNS:   "network",
			wantType: "LinkStatus",
			wantID:   "eth0",
		},
		{
			name:    "wrong scheme",
			uri:     "http://10.0.0.1/resource/runtime/MachineStatus",
			wantErr: true,
		},
		{
			name:    "missing node",
			uri:     "talos:///resource/runtime/MachineStatus",
			wantErr: true,
		},
		{
			name:    "missing type segment",
			uri:     "talos://10.0.0.1/resource/runtime",
			wantErr: true,
		},
		{
			name:    "wrong path prefix",
			uri:     "talos://10.0.0.1/resources/runtime/MachineStatus",
			wantErr: true,
		},
		{
			name:    "empty string",
			uri:     "",
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			node, ns, typ, id, err := parseCOSIURI(tc.uri)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("parseCOSIURI(%q) = nil error, want error", tc.uri)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseCOSIURI(%q) error: %v", tc.uri, err)
			}
			if node != tc.wantNode {
				t.Errorf("node = %q, want %q", node, tc.wantNode)
			}
			if ns != tc.wantNS {
				t.Errorf("namespace = %q, want %q", ns, tc.wantNS)
			}
			if typ != tc.wantType {
				t.Errorf("type = %q, want %q", typ, tc.wantType)
			}
			if id != tc.wantID {
				t.Errorf("id = %q, want %q", id, tc.wantID)
			}
		})
	}
}

func TestRegister_NoPanic(t *testing.T) {
	server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.0.0"}, nil)
	// Register with nil client — we only validate that URI templates are valid (no panic)
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Register panicked: %v", r)
		}
	}()
	Register(server, nil)
}
