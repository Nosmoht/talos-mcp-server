package tools

import (
	"context"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	"google.golang.org/grpc"
)

const (
	foreignSymlinkText = "owned by another user"
	symlinkLoopText    = "too many symlinks"
	foreignLinkName    = "foreign"
)

type countingSnapshotClient struct {
	*mockClient

	calls int
}

func (c *countingSnapshotClient) EtcdSnapshot(context.Context, *machineapi.EtcdSnapshotRequest, ...grpc.CallOption) (io.ReadCloser, error) {
	c.calls++
	return io.NopCloser(strings.NewReader(snapshotPayload)), nil
}

// treatAsForeign makes every symlink named foreignLinkName look owned by another non-root user.
func treatAsForeign(t *testing.T) {
	t.Helper()
	prev := symlinkOwner
	symlinkOwner = func(info fs.FileInfo) (uint32, bool) {
		owner, ok := prev(info)
		if info.Name() == foreignLinkName {
			return owner + 1, ok
		}
		return owner, ok
	}
	t.Cleanup(func() { symlinkOwner = prev })
}

func mkdir(t *testing.T, path string) string {
	t.Helper()
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatalf("setup: mkdir %s: %v", path, err)
	}
	return path
}

func symlink(t *testing.T, target, link string) string {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("setup: symlink %s -> %s: %v", link, target, err)
	}
	return link
}

func assertDirEmpty(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	if len(entries) != 0 {
		t.Errorf("symlink guard: expected nothing written to %s got %d entries", dir, len(entries))
	}
}

func snapshotTo(dest string) (*countingSnapshotClient, error) {
	client := &countingSnapshotClient{mockClient: &mockClient{}}
	h := &Handlers{Client: client}
	_, _, err := h.HandleEtcdSnapshot(context.Background(), nil, EtcdSnapshotArgs{Nodes: []string{snapshotNode}, Path: dest})
	return client, err
}

// Destinations whose directory passes through another user's symlink are refused before the snapshot RPC.
func TestGivenForeignSymlinkInDestinationDirWhenSnapshottingThenItIsRefusedBeforeRPC(t *testing.T) {
	cases := []struct {
		name  string
		build func(t *testing.T, base, target string) string
	}{
		{"parent is foreign", func(t *testing.T, base, target string) string {
			t.Helper()
			return filepath.Join(symlink(t, target, filepath.Join(base, foreignLinkName)), "etcd.db")
		}},
		{"own link targets through foreign", func(t *testing.T, base, target string) string {
			t.Helper()
			symlink(t, filepath.Dir(target), filepath.Join(base, foreignLinkName))
			own := symlink(t, filepath.Join(base, foreignLinkName, filepath.Base(target)), filepath.Join(base, "own"))
			return filepath.Join(own, "etcd.db")
		}},
		{"own relative link targets through foreign", func(t *testing.T, base, target string) string {
			t.Helper()
			symlink(t, filepath.Dir(target), filepath.Join(base, foreignLinkName))
			own := symlink(t, filepath.Join(foreignLinkName, filepath.Base(target)), filepath.Join(base, "own"))
			return filepath.Join(own, "etcd.db")
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			treatAsForeign(t)
			base := t.TempDir()
			target := mkdir(t, filepath.Join(base, "real", "target"))

			client, err := snapshotTo(tc.build(t, base, target))
			if err == nil || !strings.Contains(err.Error(), foreignSymlinkText) {
				t.Errorf("symlink guard: expected error containing %q got %v", foreignSymlinkText, err)
			}
			if client.calls != 0 {
				t.Errorf("symlink guard: expected no snapshot RPC got %d", client.calls)
			}
			assertDirEmpty(t, target)
		})
	}
}

func TestGivenForeignParentSymlinkWhenWritingSecretFileThenItIsRefused(t *testing.T) {
	treatAsForeign(t)
	base := t.TempDir()
	target := mkdir(t, filepath.Join(base, "target"))
	dest := filepath.Join(symlink(t, target, filepath.Join(base, foreignLinkName)), "etcd.db")

	_, err := WriteSecretFile(dest, strings.NewReader(snapshotPayload))
	if err == nil || !strings.Contains(err.Error(), foreignSymlinkText) {
		t.Errorf("symlink guard: expected error containing %q got %v", foreignSymlinkText, err)
	}
	assertDirEmpty(t, target)
}

// The server user's own symlinks, absolute or relative with "..", are followed to the directory they name.
func TestGivenOwnSymlinkInDestinationDirWhenSnapshottingThenFileLandsInTarget(t *testing.T) {
	cases := []struct {
		name   string
		target func(base string) string
	}{
		{"absolute target", func(base string) string { return filepath.Join(base, "real", "target") }},
		{"relative target with ..", func(string) string { return filepath.Join("..", "real", "target") }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			base := t.TempDir()
			target := mkdir(t, filepath.Join(base, "real", "target"))
			links := mkdir(t, filepath.Join(base, "links"))
			dest := filepath.Join(symlink(t, tc.target(base), filepath.Join(links, "own")), "etcd.db")

			if _, err := snapshotTo(dest); err != nil {
				t.Fatalf("symlink guard: expected own symlink to be followed got %v", err)
			}
			if got := string(readSnapshotFile(t, filepath.Join(target, "etcd.db"))); got != snapshotPayload {
				t.Errorf("symlink guard: expected snapshot payload %q in target got %q", snapshotPayload, got)
			}
		})
	}
}

func TestGivenSymlinkLoopInDestinationDirWhenSnapshottingThenItIsRefused(t *testing.T) {
	base := t.TempDir()
	a := filepath.Join(base, "a")
	symlink(t, a, filepath.Join(base, "b"))
	symlink(t, filepath.Join(base, "b"), a)

	client, err := snapshotTo(filepath.Join(a, "etcd.db"))
	if err == nil || !strings.Contains(err.Error(), symlinkLoopText) {
		t.Errorf("symlink guard: expected error containing %q got %v", symlinkLoopText, err)
	}
	if client.calls != 0 {
		t.Errorf("symlink guard: expected no snapshot RPC got %d", client.calls)
	}
}
