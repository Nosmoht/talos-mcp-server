package tools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"path/filepath"
	"testing"
)

type snapshotStructuredResult struct {
	Path   string `json:"path"`
	Bytes  int64  `json:"bytes"`
	SHA256 string `json:"sha256"`
}

func TestSnapshotResultReportsPathBytesDigest(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "etcd.db")
	digest := sha256.Sum256([]byte(snapshotPayload))
	want := snapshotStructuredResult{
		Path:   dest,
		Bytes:  int64(len(snapshotPayload)),
		SHA256: hex.EncodeToString(digest[:]),
	}

	_, out, err := snapshotHandlers().HandleEtcdSnapshot(context.Background(), nil, EtcdSnapshotArgs{
		Nodes: []string{snapshotNode},
		Path:  dest,
	})
	if err != nil {
		t.Fatalf("spec AC-55: precondition expected the snapshot call to succeed got=%v", err)
	}

	raw, err := json.Marshal(out)
	if err != nil {
		t.Fatalf("spec AC-55: precondition expected a JSON-encodable structured result got=%v", err)
	}
	var got snapshotStructuredResult
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("spec AC-55: precondition expected a JSON object structured result got=%s (%v)", raw, err)
	}
	if got != want {
		t.Errorf("spec AC-55: expected structured result %+v got %+v", want, got)
	}
}
