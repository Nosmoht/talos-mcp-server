package tools

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
)

func TestHandleHealth_InvalidTimeout(t *testing.T) {
	h := safeH()
	ctx := context.Background()

	tests := []struct {
		name        string
		waitTimeout string
		wantErr     string
	}{
		{
			name:        "invalid duration",
			waitTimeout: "not-a-duration",
			wantErr:     "parse wait_timeout",
		},
		{
			name:        "bare number without unit",
			waitTimeout: "120",
			wantErr:     "parse wait_timeout",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := HealthArgs{WaitTimeout: tt.waitTimeout}
			_, _, err := h.HandleHealth(ctx, &mcp.CallToolRequest{}, args)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error %q does not contain %q", err.Error(), tt.wantErr)
			}
		})
	}
}

// TestHandleVersion_InsecureReturnsDialedClientVersion: in maintenance mode
// talos_version returns the version response of the dialed client.
func TestHandleVersion_InsecureReturnsDialedClientVersion(t *testing.T) {
	t.Parallel()

	want := &machineapi.VersionResponse{}
	fake := &fakeMaintenanceClient{version: want}
	h := insecureHandlers(t, &recordingDialer{client: fake})

	_, out, err := h.HandleVersion(context.Background(), nil, VersionArgs{Insecure: true, Endpoint: insecureTestEndpoint})
	if err != nil {
		t.Fatalf("talos_version insecure: unexpected error %v", err)
	}
	if out != want {
		t.Errorf("talos_version insecure: want the dialed client's response %p got %v", want, out)
	}
	if !fake.closed {
		t.Errorf("dialInsecure contract: talos_version insecure must Close() the dialed client")
	}
}

// TestHandleVersion_InsecureDialsCanonicalEndpointWithFingerprint: an IPv4-mapped
// endpoint is dialed in canonical IPv4 form with the decoded fingerprint bytes
// (.claude/rules/insecure-mode.md §Endpoint canonicalisation, §Fingerprint format).
func TestHandleVersion_InsecureDialsCanonicalEndpointWithFingerprint(t *testing.T) {
	t.Parallel()

	wantFingerprint := bytes.Repeat([]byte{0xab}, sha256.Size)
	colonHex := strings.TrimSuffix(strings.Repeat("ab:", sha256.Size), ":")

	d := &recordingDialer{client: &fakeMaintenanceClient{version: &machineapi.VersionResponse{}}}
	h := insecureHandlers(t, d)

	if _, _, err := h.HandleVersion(context.Background(), nil, VersionArgs{
		Insecure:        true,
		Endpoint:        "::ffff:" + insecureTestEndpoint,
		CertFingerprint: colonHex,
	}); err != nil {
		t.Fatalf("talos_version insecure: unexpected error %v", err)
	}

	type dialed struct {
		Endpoint    string
		Fingerprint string
	}
	got := dialed{d.endpoint, hex.EncodeToString(d.fingerprint)}
	want := dialed{insecureTestEndpoint, hex.EncodeToString(wantFingerprint)}
	if got != want {
		t.Errorf("insecure-mode.md §Endpoint canonicalisation / §Fingerprint format: want dial %+v got %+v", want, got)
	}
}
