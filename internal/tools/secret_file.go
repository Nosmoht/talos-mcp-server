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

// SecretFileResult describes a file produced by WriteSecretFile.
type SecretFileResult struct {
	Path   string `json:"path"`
	Bytes  int64  `json:"bytes"`
	SHA256 string `json:"sha256"`
}

// WriteSecretFile writes src to a new owner-only (0600) file at destPath and returns its path, size and sha256.
// It refuses an existing file or symlink and removes a partial file on failure.
func WriteSecretFile(destPath string, src io.Reader) (SecretFileResult, error) {
	if err := checkSecretFilePath(destPath); err != nil {
		return SecretFileResult{}, err
	}

	f, err := os.OpenFile(destPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600) //nolint:gosec // destPath checked by checkSecretFilePath; O_EXCL refuses existing paths
	if errors.Is(err, fs.ErrExist) {
		return SecretFileResult{}, fmt.Errorf("secret_file: %q already exists as a file or symlink: %w", destPath, err)
	}
	if err != nil {
		return SecretFileResult{}, fmt.Errorf("secret_file: open %q: %w", destPath, err)
	}

	// OpenFile's mode is masked by the umask; Chmod's is not.
	if err := f.Chmod(0o600); err != nil {
		_ = f.Close()
		_ = os.Remove(destPath)
		return SecretFileResult{}, fmt.Errorf("secret_file: chmod %q: %w", destPath, err)
	}

	hasher := sha256.New()
	tee := io.TeeReader(src, hasher)

	n, err := io.Copy(f, tee)
	if err != nil {
		_ = f.Close()
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

func checkSecretFilePath(destPath string) error {
	if destPath == "" {
		return fmt.Errorf("secret_file: path must be specified")
	}
	if !filepath.IsAbs(destPath) {
		return fmt.Errorf("secret_file: path must be absolute (got %q)", destPath)
	}
	if cleaned := filepath.Clean(destPath); cleaned != destPath {
		return fmt.Errorf("secret_file: path must not contain .. or redundant separators (got %q)", destPath)
	}
	return nil
}

func checkSecretFileAbsent(destPath string) error {
	info, err := os.Lstat(destPath)
	if err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("secret_file: refusing to write through symlink at %q", destPath)
		}
		return fmt.Errorf("secret_file: destination %q already exists", destPath)
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("secret_file: lstat %q: %w", destPath, err)
	}
	return nil
}
