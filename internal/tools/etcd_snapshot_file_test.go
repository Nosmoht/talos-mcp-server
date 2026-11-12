package tools

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	"google.golang.org/grpc"

	"github.com/Nosmoht/talos-mcp-server/internal/talos"
)

const (
	snapshotNode           = "192.0.2.30"
	snapshotForeignNode    = "192.0.2.31"
	snapshotPayload        = "etcd-snapshot-payload"
	snapshotExistingBytes  = "bytes that were already on disk"
	snapshotNodeCountText  = "requires exactly one target node"
	snapshotAllowlistText  = "is not in the allowed nodes list"
	snapshotOwnerOnlyPerms = os.FileMode(0o600)
)

type snapshotStreamClient struct {
	*mockClient
}

func (c *snapshotStreamClient) EtcdSnapshot(_ context.Context, _ *machineapi.EtcdSnapshotRequest, _ ...grpc.CallOption) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader(snapshotPayload)), nil
}

func snapshotHandlers() *Handlers {
	return &Handlers{Client: &snapshotStreamClient{mockClient: &mockClient{}}}
}

func seedSnapshotFile(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(snapshotExistingBytes), 0o600); err != nil {
		t.Fatalf("setup: seed %s: %v", path, err)
	}
}

func readSnapshotFile(t *testing.T, path string) []byte {
	t.Helper()
	got, err := os.ReadFile(path) //nolint:gosec // G304: path is under t.TempDir
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return got
}

func TestSnapshotFileIsOwnerOnly(t *testing.T) {
	// Umask is process-wide, so this test stays serial; umask 0 exposes a create mode that relies on the umask.
	prev := syscall.Umask(0)
	t.Cleanup(func() { syscall.Umask(prev) })

	dest := filepath.Join(t.TempDir(), "etcd.db")
	if _, _, err := snapshotHandlers().HandleEtcdSnapshot(context.Background(), nil, EtcdSnapshotArgs{
		Nodes: []string{snapshotNode},
		Path:  dest,
	}); err != nil {
		t.Fatalf("spec AC-1: precondition expected the snapshot call to succeed got=%v", err)
	}

	info, err := os.Lstat(dest)
	if err != nil {
		t.Fatalf("spec AC-1: precondition expected the snapshot file to exist got=%v", err)
	}
	if got := info.Mode().Perm(); got != snapshotOwnerOnlyPerms {
		t.Errorf("spec AC-1: expected file mode %#o got %#o", snapshotOwnerOnlyPerms, got)
	}
}

func TestSnapshotRefusesExistingFile(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "etcd.db")
	seedSnapshotFile(t, dest)

	_, _, err := snapshotHandlers().HandleEtcdSnapshot(context.Background(), nil, EtcdSnapshotArgs{
		Nodes: []string{snapshotNode},
		Path:  dest,
	})
	got := readSnapshotFile(t, dest)
	if err == nil || !bytes.Equal(got, []byte(snapshotExistingBytes)) {
		t.Errorf("spec AC-2: expected an error and unchanged bytes %q got err=%v bytes=%q", snapshotExistingBytes, err, got)
	}
}

func TestSnapshotRefusesSymlinkPath(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target.db")
	seedSnapshotFile(t, target)
	link := filepath.Join(dir, "etcd.db")
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("setup: symlink %s -> %s: %v", link, target, err)
	}

	_, _, err := snapshotHandlers().HandleEtcdSnapshot(context.Background(), nil, EtcdSnapshotArgs{
		Nodes: []string{snapshotNode},
		Path:  link,
	})
	got := readSnapshotFile(t, target)
	if err == nil || !bytes.Equal(got, []byte(snapshotExistingBytes)) {
		t.Errorf("spec AC-3: expected an error and unchanged symlink target bytes %q got err=%v bytes=%q", snapshotExistingBytes, err, got)
	}
}

func TestSnapshotNodeCountGuardPrecedesFileProbe(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "etcd.db")
	seedSnapshotFile(t, dest)

	cases := []struct {
		name  string
		nodes []string
	}{
		{name: "zero nodes", nodes: nil},
		{name: "two nodes", nodes: []string{snapshotNode, snapshotForeignNode}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := safeH().HandleEtcdSnapshot(context.Background(), nil, EtcdSnapshotArgs{
				Nodes: tc.nodes,
				Path:  dest,
			})
			if err == nil || !strings.Contains(err.Error(), snapshotNodeCountText) {
				t.Errorf("spec AC-41: case=%s expected=error containing %q got=%v", tc.name, snapshotNodeCountText, err)
			}
		})
	}
}

func TestSnapshotAllowlistGuardPrecedesFileProbe(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "etcd.db")
	seedSnapshotFile(t, dest)

	allowlist, err := talos.ParseNodeAllowlist(snapshotNode)
	if err != nil {
		t.Fatalf("setup: parse allowlist: %v", err)
	}
	h := safeH()
	h.AllowedNodes = allowlist

	_, _, err = h.HandleEtcdSnapshot(context.Background(), nil, EtcdSnapshotArgs{
		Nodes: []string{snapshotForeignNode},
		Path:  dest,
	})
	if err == nil || !strings.Contains(err.Error(), snapshotAllowlistText) {
		t.Errorf("spec AC-42: expected=error containing %q got=%v", snapshotAllowlistText, err)
	}
}
