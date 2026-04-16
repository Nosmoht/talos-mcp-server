package tools

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// SecretFileResult describes a file produced by WriteSecretFile. The sha256
// field lets callers verify integrity without re-reading the bytes.
type SecretFileResult struct {
	Path   string `json:"path"`
	Bytes  int64  `json:"bytes"`
	SHA256 string `json:"sha256"`
}

// WriteSecretFile streams src to destPath with a consistent set of guards
// appropriate for key material, kubeconfig, etcd snapshots, and any other
// file the server emits on behalf of a tool handler.
//
// Guards:
//
//   - destPath must be absolute and filepath.Clean-stable (rejects `..` and
//     redundant separators).
//   - destPath must not exist. A symlink at destPath is reported explicitly
//     so the operator can distinguish accidental prior runs from deliberate
//     redirection attempts.
//   - The file is created with O_CREAT|O_EXCL|O_WRONLY mode 0600. O_EXCL
//     closes the TOCTOU race between the Lstat check and the open.
//   - On any write failure the partial file is unlinked before returning.
//
// Returns the canonical path, byte count, and hex-encoded sha256 of the
// written content. Callers are expected to surface those fields as the tool's
// structured result (never the file bytes themselves).
func WriteSecretFile(destPath string, src io.Reader) (SecretFileResult, error) {
	if destPath == "" {
		return SecretFileResult{}, fmt.Errorf("secret_file: path must be specified")
	}
	if !filepath.IsAbs(destPath) {
		return SecretFileResult{}, fmt.Errorf("secret_file: path must be absolute (got %q)", destPath)
	}
	if cleaned := filepath.Clean(destPath); cleaned != destPath {
		return SecretFileResult{}, fmt.Errorf("secret_file: path must not contain .. or redundant separators (got %q)", destPath)
	}

	// Lstat so a symlink at destPath is reported as a symlink rather than
	// silently followed. O_EXCL on the open below covers the race window
	// after this check.
	if info, err := os.Lstat(destPath); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return SecretFileResult{}, fmt.Errorf("secret_file: refusing to write through symlink at %q", destPath)
		}
		return SecretFileResult{}, fmt.Errorf("secret_file: destination %q already exists", destPath)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return SecretFileResult{}, fmt.Errorf("secret_file: lstat %q: %w", destPath, err)
	}

	f, err := os.OpenFile(destPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600) //nolint:gosec // G304: destPath is validated above (absolute, clean, non-symlink, non-existent); this function exists precisely to provide the safe-write primitive.
	if err != nil {
		return SecretFileResult{}, fmt.Errorf("secret_file: open %q: %w", destPath, err)
	}

	hasher := sha256.New()
	tee := io.TeeReader(src, hasher)

	n, err := io.Copy(f, tee)
	if err != nil {
		_ = f.Close()
		// Best-effort cleanup of the partial file. os.Remove swallowing
		// errors is acceptable here because the original err is already
		// the authoritative failure.
		_ = os.Remove(destPath)
		return SecretFileResult{}, fmt.Errorf("secret_file: write %q: %w", destPath, err)
	}

	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(destPath)
		return SecretFileResult{}, fmt.Errorf("secret_file: sync %q: %w", destPath, err)
	}

	if err := f.Close(); err != nil {
		_ = os.Remove(destPath)
		return SecretFileResult{}, fmt.Errorf("secret_file: close %q: %w", destPath, err)
	}

	return SecretFileResult{
		Path:   destPath,
		Bytes:  n,
		SHA256: hex.EncodeToString(hasher.Sum(nil)),
	}, nil
}
