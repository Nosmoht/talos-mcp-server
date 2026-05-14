package talos

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
)

func TestCanonicalIP(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		want    string
		wantErr string // substring; empty means no error
	}{
		{name: "ipv4 routable", input: "192.0.2.5", want: "192.0.2.5"},
		{name: "ipv4 rfc1918", input: "192.168.1.1", want: "192.168.1.1"},
		{name: "ipv6 routable", input: "2001:db8::1", want: "2001:db8::1"},
		{name: "ipv4 mapped ipv6 collapses", input: "::ffff:192.0.2.5", want: "192.0.2.5"},
		{name: "empty", input: "", wantErr: "must not be empty"},
		{name: "hostname rejected", input: "node-1.example.com", wantErr: "not a bare IP"},
		{name: "host:port rejected", input: "192.0.2.5:50000", wantErr: "not a bare IP"},
		{name: "bracketed ipv6 rejected", input: "[2001:db8::1]", wantErr: "not a bare IP"},
		{name: "ipv6 zone rejected", input: "fe80::1%eth0", wantErr: "not a bare IP"},
		{name: "ipv4 unspecified", input: "0.0.0.0", wantErr: "unspecified"},
		{name: "ipv6 unspecified", input: "::", wantErr: "unspecified"},
		{name: "ipv4 loopback", input: "127.0.0.1", wantErr: "loopback"},
		{name: "ipv4 loopback range", input: "127.5.5.5", wantErr: "loopback"},
		{name: "ipv6 loopback", input: "::1", wantErr: "loopback"},
		{name: "ipv4 link-local imds", input: "169.254.169.254", wantErr: "link-local"},
		{name: "ipv4 link-local", input: "169.254.1.1", wantErr: "link-local"},
		{name: "ipv6 link-local", input: "fe80::1", wantErr: "link-local"},
		{name: "ipv4 multicast", input: "224.0.0.1", wantErr: "multicast"},
		{name: "ipv6 multicast", input: "ff02::1", wantErr: "multicast"},
		{name: "ipv4 broadcast", input: "255.255.255.255", wantErr: "broadcast"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := CanonicalIP(tc.input)
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil (result %q)", tc.wantErr, got)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error %q does not contain %q", err.Error(), tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("canonical form: got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestParseFingerprint(t *testing.T) {
	t.Parallel()

	validHex := strings.Repeat("ab", 32) // 64 chars
	validBytes, _ := hex.DecodeString(validHex)

	tests := []struct {
		name    string
		input   string
		want    []byte
		wantErr string
	}{
		{name: "plain 64 hex lower", input: validHex, want: validBytes},
		{name: "plain 64 hex upper", input: strings.ToUpper(validHex), want: validBytes},
		{name: "colons stripped", input: "ab:ab:ab:ab:ab:ab:ab:ab:ab:ab:ab:ab:ab:ab:ab:ab:ab:ab:ab:ab:ab:ab:ab:ab:ab:ab:ab:ab:ab:ab:ab:ab", want: validBytes},
		{name: "whitespace stripped", input: "  " + validHex + "\n", want: validBytes},
		{name: "mixed separators", input: strings.Join(strings.Split(validHex, ""), " "), want: validBytes},
		{name: "empty", input: "", wantErr: "must not be empty"},
		{name: "only separators", input: ":  \t\n", wantErr: "must not be empty"},
		{name: "63 chars", input: strings.Repeat("a", 63), wantErr: "exactly 64 hex"},
		{name: "65 chars", input: strings.Repeat("a", 65), wantErr: "exactly 64 hex"},
		{name: "non-hex chars", input: strings.Repeat("z", 64), wantErr: "non-hex"},
		{name: "unicode digit (fullwidth)", input: strings.Repeat("０", 64), wantErr: "exactly 64 hex"}, // fullwidth zero is 3 bytes in UTF-8
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := ParseFingerprint(tc.input)
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tc.wantErr)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error %q does not contain %q", err.Error(), tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if string(got) != string(tc.want) {
				t.Fatalf("decoded bytes mismatch: got %x, want %x", got, tc.want)
			}
		})
	}
}

func TestFingerprintVerifier(t *testing.T) {
	t.Parallel()

	rawLeaf := []byte("dummy-leaf-cert-bytes")
	leafHash := sha256.Sum256(rawLeaf)

	t.Run("match", func(t *testing.T) {
		t.Parallel()
		verify := fingerprintVerifier(leafHash[:])
		if err := verify([][]byte{rawLeaf}, nil); err != nil {
			t.Fatalf("expected nil, got %v", err)
		}
	})

	t.Run("mismatch", func(t *testing.T) {
		t.Parallel()
		verify := fingerprintVerifier(leafHash[:])
		other := []byte("different-cert")
		if err := verify([][]byte{other}, nil); !errors.Is(err, errFingerprintMismatch) {
			t.Fatalf("expected errFingerprintMismatch, got %v", err)
		}
	})

	t.Run("no peer cert", func(t *testing.T) {
		t.Parallel()
		verify := fingerprintVerifier(leafHash[:])
		if err := verify([][]byte{}, nil); !errors.Is(err, errNoPeerCert) {
			t.Fatalf("expected errNoPeerCert, got %v", err)
		}
	})

	t.Run("leaf-only — chain has multiple certs, only leaf hashed", func(t *testing.T) {
		t.Parallel()
		verify := fingerprintVerifier(leafHash[:])
		intermediate := []byte("intermediate-with-different-bytes")
		// rawCerts[0] is the leaf, rest are chain. Verifier must ignore the rest.
		if err := verify([][]byte{rawLeaf, intermediate}, nil); err != nil {
			t.Fatalf("expected nil (leaf matches), got %v", err)
		}
	})
}
