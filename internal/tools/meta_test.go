package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/cosi-project/runtime/pkg/resource"
	"github.com/cosi-project/runtime/pkg/state"
	"github.com/cosi-project/runtime/pkg/state/impl/inmem"
	"github.com/cosi-project/runtime/pkg/state/impl/namespaced"
	runtimeres "github.com/siderolabs/talos/pkg/machinery/resources/runtime"

	"github.com/Nosmoht/talos-mcp-server/internal/talos"
)

func TestParseMetaKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		want    uint8
		wantErr string
	}{
		{name: "decimal", input: "6", want: 6},
		{name: "decimal max", input: "255", want: 255},
		{name: "decimal zero", input: "0", want: 0},
		{name: "hex lower", input: "0x0c", want: 12},
		{name: "hex upper", input: "0X0C", want: 12},
		{name: "explicit octal", input: "0o17", want: 15},
		{name: "empty", input: "", wantErr: "must not be empty"},
		{name: "leading zero rejected", input: "06", wantErr: "ambiguous leading zero"},
		{name: "leading zero rejected (long)", input: "013", wantErr: "ambiguous leading zero"},
		{name: "overflow", input: "256", wantErr: "exceeds META key range"},
		{name: "non-numeric", input: "abc", wantErr: "invalid syntax"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := parseMetaKey(tc.input)
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
			if got != tc.want {
				t.Errorf("got %d, want %d", got, tc.want)
			}
		})
	}
}

// TestHandleMeta_Guards verifies the action / confirm / privileged-key /
// insecure gates of talos_meta. The mock client panics on Meta* calls, so
// reaching the gRPC layer surfaces as a test failure.
func TestHandleMeta_Guards(t *testing.T) {
	ctx := context.Background()

	allowlist, err := talos.ParseNodeAllowlist("192.0.2.5")
	if err != nil {
		t.Fatalf("setup: parse allowlist: %v", err)
	}

	// h with the privileged key 6 (Upgrade) explicitly enumerated.
	enabledH := func() *Handlers {
		return &Handlers{
			Client:               &mockClient{},
			EnableInsecure:       true,
			InsecureAllowedNodes: allowlist,
			MetaPrivilegedKeys:   map[uint8]struct{}{6: {}},
		}
	}

	tests := []struct {
		name    string
		h       *Handlers
		args    MetaArgs
		wantErr string
	}{
		{
			name:    "unknown action",
			h:       safeH(),
			args:    MetaArgs{Action: "list", Key: "12"},
			wantErr: "action must be 'read', 'write', or 'delete'",
		},
		{
			name:    "write without confirm",
			h:       safeH(),
			args:    MetaArgs{Action: "write", Key: "12", Value: "x"},
			wantErr: "confirm must be set to true",
		},
		{
			name:    "delete without confirm",
			h:       safeH(),
			args:    MetaArgs{Action: "delete", Key: "12"},
			wantErr: "confirm must be set to true",
		},
		{
			name: "write non-reserved key without privileged enumeration",
			h:    safeH(),
			args: MetaArgs{
				Action:  "write",
				Key:     "9", // StateEncryptionConfig
				Value:   "x",
				Confirm: true,
			},
			wantErr: "is not in UserReserved* and not listed in TALOS_MCP_META_PRIVILEGED_KEYS",
		},
		{
			name: "delete non-reserved key without privileged enumeration",
			h:    safeH(),
			args: MetaArgs{
				Action:  "delete",
				Key:     "0x06", // Upgrade
				Confirm: true,
			},
			wantErr: "is not in UserReserved* and not listed in TALOS_MCP_META_PRIVILEGED_KEYS",
		},
		{
			name:    "invalid key",
			h:       safeH(),
			args:    MetaArgs{Action: "read", Key: "abc"},
			wantErr: "invalid syntax",
		},
		{
			name:    "leading-zero key rejected",
			h:       safeH(),
			args:    MetaArgs{Action: "read", Key: "06"},
			wantErr: "ambiguous leading zero",
		},
		{
			name: "cert_fingerprint without insecure",
			h:    safeH(),
			args: MetaArgs{
				Action:          "read",
				Key:             "12",
				CertFingerprint: strings.Repeat("a", 64),
			},
			wantErr: "cert_fingerprint requires insecure=true",
		},
		{
			name: "insecure without enable",
			h:    safeH(),
			args: MetaArgs{
				Action:   "read",
				Key:      "12",
				Insecure: true,
				Endpoint: "192.0.2.5",
			},
			wantErr: "TALOS_MCP_ENABLE_INSECURE",
		},
		{
			name: "insecure with nodes mutually exclusive",
			h:    enabledH(),
			args: MetaArgs{
				Action:   "read",
				Key:      "12",
				Insecure: true,
				Endpoint: "192.0.2.5",
				Nodes:    []string{"192.0.2.6"},
			},
			wantErr: "mutually exclusive",
		},
		{
			name: "insecure write to non-allowlisted endpoint",
			h:    enabledH(),
			args: MetaArgs{
				Action:   "write",
				Key:      "12",
				Value:    "x",
				Confirm:  true,
				Insecure: true,
				Endpoint: "192.0.2.99",
			},
			wantErr: "not in TALOS_MCP_INSECURE_ALLOWED_NODES",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := tt.h.HandleMeta(ctx, nil, tt.args)
			if err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("expected error containing %q, got: %v", tt.wantErr, err)
			}
		})
	}
}

// TestReadMetaFromCOSI_ResourceIdentity verifies that readMetaFromCOSI looks
// up the META key using the canonical upstream resource type AND id format.
// This is the regression test for a critical bug found in code review where
// the type was "MetaKeys.meta.talos.dev" (wrong: should be "...runtime...")
// and the id was decimal "6" (wrong: should be "0x06" via MetaKeyTagToID).
//
// The test seeds a real in-memory COSI state with one MetaKey resource at the
// correct identity, calls readMetaFromCOSI through the production code path,
// and asserts the value round-trips. If either the type string OR the id
// format drift, this test fails with a "resource not found" error.
func TestReadMetaFromCOSI_ResourceIdentity(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	// Seed an in-memory COSI state with one MetaKey resource using the
	// canonical upstream constants. This is what a real Talos node exposes.
	st := state.WrapCore(namespaced.NewState(inmem.Build))
	const testKey uint8 = 6 // meta.Upgrade
	mk := runtimeres.NewMetaKey(runtimeres.NamespaceName, runtimeres.MetaKeyTagToID(testKey))
	mk.TypedSpec().Value = "v1.12.0"
	if err := st.Create(ctx, mk); err != nil {
		t.Fatalf("seed MetaKey: %v", err)
	}

	var (
		outcome  = OutcomeOK
		finalErr error
	)

	_, structured, err := readMetaFromCOSI(ctx, st, testKey, &outcome, &finalErr)
	if err != nil {
		t.Fatalf("readMetaFromCOSI: %v (outcome=%s) — does the resource type or id format match upstream pkg/machinery/resources/runtime?", err, outcome)
	}
	if outcome != OutcomeOK {
		t.Errorf("outcome = %q, want %q", outcome, OutcomeOK)
	}
	if structured == nil {
		t.Fatal("expected non-nil structured result")
	}
	resultMap, ok := structured.(map[string]any)
	if !ok {
		t.Fatalf("structured result is not map[string]any: %T", structured)
	}
	if resultMap["action"] != "read" {
		t.Errorf("action = %v, want read", resultMap["action"])
	}
	if got, want := resultMap["key"], testKey; got != want {
		t.Errorf("key = %v, want %v", got, want)
	}
	if resultMap["resource"] == nil {
		t.Error("resource payload missing")
	}

	// Sanity: the seeded resource ID must match what MetaKeyTagToID returned.
	if got := mk.Metadata().ID(); got != resource.ID("0x06") {
		t.Errorf("MetaKeyTagToID drift: id=%q, expected 0x06", got)
	}
}

// TestReadMetaFromCOSI_NotFound verifies that querying a non-existent key
// surfaces the upstream error with the outcome correctly set, so the
// auditOutcome pair records "rpc-error" not "ok".
func TestReadMetaFromCOSI_NotFound(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	st := state.WrapCore(namespaced.NewState(inmem.Build))

	var (
		outcome  = OutcomeOK
		finalErr error
	)

	_, _, err := readMetaFromCOSI(ctx, st, 99, &outcome, &finalErr)
	if err == nil {
		t.Fatal("expected error for non-existent key, got nil")
	}
	if outcome != OutcomeRPCError {
		t.Errorf("outcome = %q, want %q", outcome, OutcomeRPCError)
	}
	if finalErr == nil {
		t.Error("finalErr should be set when err is non-nil")
	}
}

// TestHandleMeta_PrivilegedKeyAllowlisted verifies that a key explicitly
// listed in MetaPrivilegedKeys passes the safelist gate. The call will reach
// the gRPC layer and panic on the mock — confirming the gate accepted it.
func TestHandleMeta_PrivilegedKeyAllowlisted(t *testing.T) {
	h := &Handlers{
		Client:             &mockClient{},
		MetaPrivilegedKeys: map[uint8]struct{}{6: {}}, // Upgrade
	}

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic from mockClient.MetaWrite (safelist accepted, RPC reached)")
		}
		msg, _ := r.(string)
		if !strings.Contains(msg, "MetaWrite") {
			t.Errorf("unexpected panic source: %v", r)
		}
	}()

	_, _, _ = h.HandleMeta(context.Background(), nil, MetaArgs{
		Action:  "write",
		Key:     "6",
		Value:   "x",
		Confirm: true,
	})
}
