package tools

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/cosi-project/runtime/pkg/resource"
	"github.com/cosi-project/runtime/pkg/resource/meta"
	"github.com/cosi-project/runtime/pkg/resource/meta/spec"
	"github.com/cosi-project/runtime/pkg/state"
	"github.com/cosi-project/runtime/pkg/state/impl/inmem"
	"github.com/cosi-project/runtime/pkg/state/impl/namespaced"
	runtimeres "github.com/siderolabs/talos/pkg/machinery/resources/runtime"

	"github.com/Nosmoht/talos-mcp-server/internal/talos"
)

// TestHandleGetResource_InsecureGuards verifies the maintenance-mode gate
// rejections in HandleGetResource — before any dial attempt. The mock client
// panics on COSIState() / ResolveResourceKind, so reaching the gRPC layer
// surfaces as a test failure.
func TestHandleGetResource_InsecureGuards(t *testing.T) {
	ctx := context.Background()

	allowlist, err := talos.ParseNodeAllowlist("192.0.2.5")
	if err != nil {
		t.Fatalf("setup: parse allowlist: %v", err)
	}

	enabledH := func() *Handlers {
		return &Handlers{
			Client:               &mockClient{},
			EnableInsecure:       true,
			InsecureAllowedNodes: allowlist,
		}
	}

	tests := []struct {
		name    string
		h       *Handlers
		args    GetResourceArgs
		wantErr string
	}{
		{
			name: "cert_fingerprint without insecure rejected",
			h:    safeH(),
			args: GetResourceArgs{
				ResourceType:    "MachineStatus",
				CertFingerprint: strings.Repeat("ab", 32),
			},
			wantErr: "cert_fingerprint requires insecure=true",
		},
		{
			name: "insecure without enable",
			h:    safeH(),
			args: GetResourceArgs{
				ResourceType: "MachineStatus",
				Insecure:     true,
				Endpoint:     "192.0.2.5",
			},
			wantErr: "TALOS_MCP_ENABLE_INSECURE",
		},
		{
			name: "insecure with nodes mutually exclusive",
			h:    enabledH(),
			args: GetResourceArgs{
				ResourceType: "MachineStatus",
				Insecure:     true,
				Endpoint:     "192.0.2.5",
				Nodes:        []string{"192.0.2.6"},
			},
			wantErr: "mutually exclusive",
		},
		{
			name: "insecure non-IP endpoint",
			h:    enabledH(),
			args: GetResourceArgs{
				ResourceType: "MachineStatus",
				Insecure:     true,
				Endpoint:     "node.example.com",
			},
			wantErr: "not a bare IP",
		},
		{
			name: "insecure endpoint not in allowlist",
			h:    enabledH(),
			args: GetResourceArgs{
				ResourceType: "MachineStatus",
				Insecure:     true,
				Endpoint:     "192.0.2.99",
			},
			wantErr: "not in TALOS_MCP_INSECURE_ALLOWED_NODES",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := tt.h.HandleGetResource(ctx, nil, tt.args)
			if err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("expected error containing %q, got: %v", tt.wantErr, err)
			}
		})
	}
}

// TestHandleGetResource_InsecureReadsDialedClientState: in maintenance mode
// talos_get resolves the kind and reads resources through the dialed client's
// COSI state (GetResourceArgs schema: resource_id selects one, omitted lists all).
func TestHandleGetResource_InsecureReadsDialedClientState(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	st := state.WrapCore(namespaced.NewState(inmem.Build))
	seededIDs := []string{"seeded-a", "seeded-b"}
	for _, id := range seededIDs {
		if err := st.Create(ctx, runtimeres.NewMetaKey(runtimeres.NamespaceName, id)); err != nil {
			t.Fatalf("setup: seed MetaKey %s: %v", id, err)
		}
	}
	rd, err := meta.NewResourceDefinition(spec.ResourceDefinitionSpec{
		Type:             runtimeres.MetaKeyType,
		DefaultNamespace: runtimeres.NamespaceName,
	})
	if err != nil {
		t.Fatalf("setup: resource definition: %v", err)
	}

	tests := []struct {
		name       string
		resourceID string
		wantIDs    []string
	}{
		{name: "get by id", resourceID: seededIDs[1], wantIDs: seededIDs[1:]},
		{name: "list", resourceID: "", wantIDs: seededIDs},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			fake := &fakeMaintenanceClient{state: st, kinds: map[resource.Type]*meta.ResourceDefinition{"MetaKey": rd}}
			h := insecureHandlers(t, &recordingDialer{client: fake})

			_, out, err := h.HandleGetResource(ctx, nil, GetResourceArgs{
				ResourceType: "MetaKey",
				ResourceID:   tc.resourceID,
				Namespace:    runtimeres.NamespaceName,
				Insecure:     true,
				Endpoint:     insecureTestEndpoint,
			})
			if err != nil {
				t.Fatalf("talos_get insecure %s: unexpected error %v", tc.name, err)
			}
			list, ok := out.(resourceList)
			if !ok {
				t.Fatalf("talos_get insecure %s: structured output type %T, want resourceList", tc.name, out)
			}
			var gotIDs []string
			for _, item := range list.Items {
				md, _ := item["metadata"].(map[string]any)
				id, _ := md["id"].(string)
				gotIDs = append(gotIDs, id)
			}
			if !slices.Equal(gotIDs, tc.wantIDs) {
				t.Errorf("GetResourceArgs schema: %s want ids %v got %v", tc.name, tc.wantIDs, gotIDs)
			}
			if !fake.closed {
				t.Errorf("dialInsecure contract: talos_get insecure %s must Close() the dialed client", tc.name)
			}
		})
	}
}
