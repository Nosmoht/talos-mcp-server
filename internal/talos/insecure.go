package talos

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"strings"

	talosclient "github.com/siderolabs/talos/pkg/machinery/client"
)

// errNoPeerCert is returned by the fingerprint verifier when the TLS handshake
// presents no peer certificates. fingerprintBytes always pairs with a leaf cert.
var errNoPeerCert = errors.New("server presented no certificate")

// errFingerprintMismatch is returned by the fingerprint verifier when the
// SHA-256 of the leaf certificate does not match the operator-supplied
// fingerprint. The constant-time compare hides timing differences.
var errFingerprintMismatch = errors.New("server certificate SHA-256 does not match cert_fingerprint")

// CanonicalIP parses s as an IPv4 or IPv6 address and returns the canonical
// string form used uniformly across allowlist match, per-endpoint lock key,
// and gRPC dial target. Two different string representations of the same
// physical address (e.g. "::ffff:1.2.3.4" and "1.2.3.4") collapse to the
// same canonical key — this closes a lock-bypass class identified during
// adversarial review.
//
// Returns an error if s is not an IP, or belongs to a rejected class:
//
//   - unspecified (0.0.0.0 / ::)
//   - loopback (127.0.0.0/8, ::1)
//   - link-local (169.254.0.0/16 [incl. cloud IMDS], fe80::/10)
//   - multicast
//   - IPv4 broadcast (255.255.255.255)
//
// Hostnames, port suffixes (host:port), bracketed forms ([2001:db8::1]),
// IPv6 zone identifiers (fe80::1%eth0) all fail parse and are rejected.
func CanonicalIP(s string) (string, error) {
	if s == "" {
		return "", errors.New("endpoint must not be empty")
	}
	ip := net.ParseIP(s)
	if ip == nil {
		return "", fmt.Errorf("endpoint %q is not a bare IP address (no port, scheme, hostname, or IPv6 zone)", s)
	}
	if ip.IsUnspecified() {
		return "", fmt.Errorf("endpoint %q is the unspecified address (0.0.0.0 or ::) — not a valid target", s)
	}
	if ip.IsLoopback() {
		return "", fmt.Errorf("endpoint %q is a loopback address — not a valid maintenance-mode target", s)
	}
	if ip.IsLinkLocalUnicast() {
		return "", fmt.Errorf("endpoint %q is link-local — refused to prevent IMDS / SLAAC-local exfiltration", s)
	}
	if ip.IsMulticast() {
		return "", fmt.Errorf("endpoint %q is a multicast address — not a valid unicast target", s)
	}
	if ip4 := ip.To4(); ip4 != nil {
		// 255.255.255.255 is IPv4 limited broadcast — IsMulticast doesn't catch it.
		if ip4[0] == 0xff && ip4[1] == 0xff && ip4[2] == 0xff && ip4[3] == 0xff {
			return "", fmt.Errorf("endpoint %q is the IPv4 broadcast address — not a valid unicast target", s)
		}
		// Collapse IPv4-mapped IPv6 to dotted-quad so different string forms
		// resolve to the same canonical key.
		return ip4.String(), nil
	}
	return ip.String(), nil
}

// ParseFingerprint normalizes a user-supplied SHA-256 fingerprint string and
// returns the decoded 32 bytes. The input may contain ":" or whitespace
// separators (talosctl prints fingerprints in colon-separated form, but tools
// may strip them). After stripping, the value must be exactly 64 lowercase or
// uppercase hex characters — anything else is rejected with an explicit error.
func ParseFingerprint(s string) ([]byte, error) {
	stripped := strings.Map(func(r rune) rune {
		switch r {
		case ':', ' ', '\t', '\n', '\r':
			return -1
		}
		return r
	}, s)
	if stripped == "" {
		return nil, errors.New("cert_fingerprint must not be empty after stripping ':' and whitespace")
	}
	if len(stripped) != 64 {
		return nil, fmt.Errorf("cert_fingerprint must be exactly 64 hex chars (got %d after strip)", len(stripped))
	}
	decoded, err := hex.DecodeString(stripped)
	if err != nil {
		return nil, fmt.Errorf("cert_fingerprint contains non-hex characters: %w", err)
	}
	return decoded, nil
}

// fingerprintVerifier returns a tls.Config.VerifyPeerCertificate callback that
// rejects the connection unless SHA-256(rawCerts[0]) equals expected. The
// comparison is constant-time to hide timing differences. Only the leaf cert
// (rawCerts[0]) is hashed — this mirrors talosctl's pinning behavior.
func fingerprintVerifier(expected []byte) func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
	return func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
		if len(rawCerts) == 0 {
			return errNoPeerCert
		}
		got := sha256.Sum256(rawCerts[0])
		if subtle.ConstantTimeCompare(got[:], expected) != 1 {
			return errFingerprintMismatch
		}
		return nil
	}
}

// NewInsecureClient builds a one-shot Talos client targeting a maintenance-mode
// node. The transport is TLS with InsecureSkipVerify — the connection is
// encrypted, but the client presents no certificate and (by default) does not
// verify the server's certificate against any CA.
//
// endpoint must already be in canonical form (returned by CanonicalIP).
//
// fingerprint may be nil. When non-nil, the TLS handshake is gated by a
// VerifyPeerCertificate callback that compares SHA-256(leaf-cert) against the
// supplied bytes using constant-time compare. A mismatch terminates the
// handshake.
//
// The pkg/machinery/client package exports no maintenance-mode helper — the
// internal cmd/talosctl helper WithClientMaintenance composes WithTLSConfig
// the same way this function does. We compose manually so the lib-level path
// is the only source of truth in this codebase.
//
// The caller MUST Close() the returned client when done; the per-call factory
// shape is intentional: each maintenance-mode call targets a different short-
// lived endpoint, so a shared pool would buy nothing.
func NewInsecureClient(ctx context.Context, endpoint string, fingerprint []byte) (*talosclient.Client, error) {
	tlsCfg := &tls.Config{
		InsecureSkipVerify: true, //nolint:gosec // maintenance-mode endpoints have no CA; fingerprint pinning is the optional defence
		MinVersion:         tls.VersionTLS12,
	}
	if len(fingerprint) > 0 {
		tlsCfg.VerifyPeerCertificate = fingerprintVerifier(fingerprint)
		// VerifyConnection ALSO runs the fingerprint check — VerifyPeerCertificate
		// is bypassed on TLS session resumption (G123), but VerifyConnection runs
		// on every handshake including resumed sessions.
		tlsCfg.VerifyConnection = func(cs tls.ConnectionState) error {
			if len(cs.PeerCertificates) == 0 {
				return errNoPeerCert
			}
			return fingerprintVerifier(fingerprint)([][]byte{cs.PeerCertificates[0].Raw}, nil)
		}
	}

	opts := []talosclient.OptionFunc{
		talosclient.WithTLSConfig(tlsCfg),
		talosclient.WithEndpoints(endpoint),
		talosclient.WithGRPCDialOptions(grpcKeepalive),
	}

	dialCtx, cancel := context.WithTimeout(ctx, dialTimeout)
	defer cancel()

	c, err := talosclient.New(dialCtx, opts...)
	if err != nil {
		return nil, fmt.Errorf("dial maintenance-mode Talos gRPC at %s (timeout %s): %w", endpoint, dialTimeout, err)
	}

	return c, nil
}
