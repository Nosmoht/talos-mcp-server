package tools

import (
	"context"
	"strings"
	"testing"
)

// TestHandleEtcdSnapshot_Guards verifies input validation before any gRPC call.
func TestHandleEtcdSnapshot_Guards(t *testing.T) {
	h := safeH()
	ctx := context.Background()

	tests := []struct {
		name    string
		args    EtcdSnapshotArgs
		wantErr string
	}{
		{
			name:    "empty path",
			args:    EtcdSnapshotArgs{Nodes: []string{"192.168.2.61"}, Path: ""},
			wantErr: "path must be specified",
		},
		{
			name:    "relative path",
			args:    EtcdSnapshotArgs{Nodes: []string{"192.168.2.61"}, Path: "etcd.db"},
			wantErr: "path must be absolute",
		},
		{
			name:    "path with dotdot",
			args:    EtcdSnapshotArgs{Nodes: []string{"192.168.2.61"}, Path: "/tmp/../etc/cron.d/evil"},
			wantErr: "path must not contain ..",
		},
		{
			name:    "no nodes",
			args:    EtcdSnapshotArgs{Nodes: nil, Path: "/tmp/etcd.db"},
			wantErr: "requires exactly one target node",
		},
		{
			name:    "too many nodes",
			args:    EtcdSnapshotArgs{Nodes: []string{"192.168.2.61", "192.168.2.62"}, Path: "/tmp/etcd.db"},
			wantErr: "requires exactly one target node",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := h.HandleEtcdSnapshot(ctx, nil, tt.args)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error %q does not contain %q", err.Error(), tt.wantErr)
			}
		})
	}
}
