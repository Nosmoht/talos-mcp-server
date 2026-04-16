package tools

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestWriteSecretFile_HappyPath(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "secret.pem")
	payload := []byte("-----BEGIN FAKE KEY-----\npayload\n-----END FAKE KEY-----\n")

	got, err := WriteSecretFile(dest, strings.NewReader(string(payload)))
	if err != nil {
		t.Fatalf("WriteSecretFile: %v", err)
	}
	if got.Path != dest {
		t.Errorf("Path = %q, want %q", got.Path, dest)
	}
	if got.Bytes != int64(len(payload)) {
		t.Errorf("Bytes = %d, want %d", got.Bytes, len(payload))
	}
	wantSHA := hex.EncodeToString(sha256Sum(payload))
	if got.SHA256 != wantSHA {
		t.Errorf("SHA256 = %q, want %q", got.SHA256, wantSHA)
	}

	info, err := os.Stat(dest)
	if err != nil {
		t.Fatalf("stat result: %v", err)
	}
	if runtime.GOOS != "windows" {
		if mode := info.Mode().Perm(); mode != 0o600 {
			t.Errorf("mode = %o, want 0600", mode)
		}
	}

	actual, err := os.ReadFile(dest) //nolint:gosec // G304: dest is t.TempDir-scoped test fixture
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(actual) != string(payload) {
		t.Errorf("payload mismatch: got %q, want %q", actual, payload)
	}
}

func TestWriteSecretFile_EmptyPayload(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "empty")
	got, err := WriteSecretFile(dest, strings.NewReader(""))
	if err != nil {
		t.Fatalf("WriteSecretFile: %v", err)
	}
	if got.Bytes != 0 {
		t.Errorf("Bytes = %d, want 0", got.Bytes)
	}
	// sha256 of empty input is a well-known constant.
	if got.SHA256 != "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855" {
		t.Errorf("SHA256 = %q, want e3b0... empty-input hash", got.SHA256)
	}
}

func TestWriteSecretFile_Guards(t *testing.T) {
	dir := t.TempDir()
	existing := filepath.Join(dir, "existing")
	if err := os.WriteFile(existing, []byte("x"), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}

	tests := []struct {
		name    string
		path    string
		wantErr string
	}{
		{"empty path", "", "path must be specified"},
		{"relative path", "relative.txt", "path must be absolute"},
		{"path with dotdot", dir + "/../escape", "path must not contain .."},
		{"existing file", existing, "already exists"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := WriteSecretFile(tc.path, strings.NewReader("payload"))
			if err == nil {
				t.Fatalf("want error, got nil")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error %q does not contain %q", err.Error(), tc.wantErr)
			}
		})
	}
}

func TestWriteSecretFile_SymlinkRejected(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires elevated privileges on Windows")
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	if err := os.WriteFile(target, []byte("existing"), 0o600); err != nil {
		t.Fatalf("seed target: %v", err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	_, err := WriteSecretFile(link, strings.NewReader("payload"))
	if err == nil {
		t.Fatal("want error, got nil")
	}
	if !strings.Contains(err.Error(), "symlink") {
		t.Errorf("error %q does not mention symlink", err.Error())
	}

	// target must be untouched
	got, err := os.ReadFile(target) //nolint:gosec // G304: target is t.TempDir-scoped test fixture
	if err != nil {
		t.Fatalf("read target: %v", err)
	}
	if string(got) != "existing" {
		t.Errorf("target contents modified: got %q, want %q", got, "existing")
	}
}

func TestWriteSecretFile_ParentMissing(t *testing.T) {
	// Dest inside a non-existent directory must fail — we rely on OpenFile
	// to surface the error rather than pre-stating the parent, so we just
	// verify a recognisable error message.
	dest := filepath.Join(t.TempDir(), "missing-subdir", "file")
	_, err := WriteSecretFile(dest, strings.NewReader("x"))
	if err == nil {
		t.Fatal("want error, got nil")
	}
	if !errors.Is(err, os.ErrNotExist) {
		// Some filesystems surface a different errno; accept any error that
		// wraps ErrNotExist or mentions the path.
		if !strings.Contains(err.Error(), "open") {
			t.Errorf("unexpected error: %v", err)
		}
	}
}

// sha256Sum is a tiny helper so the test does not have to import crypto/sha256.
func sha256Sum(b []byte) []byte {
	h := sha256.New()
	_, _ = h.Write(b)
	return h.Sum(nil)
}
