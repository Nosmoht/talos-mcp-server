package resources

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/cosi-project/runtime/pkg/resource"
	"github.com/cosi-project/runtime/pkg/resource/meta"
	"github.com/cosi-project/runtime/pkg/resource/meta/spec"
	"github.com/cosi-project/runtime/pkg/state"
	"github.com/cosi-project/runtime/pkg/state/impl/inmem"
	"github.com/cosi-project/runtime/pkg/state/impl/namespaced"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	runtimeres "github.com/siderolabs/talos/pkg/machinery/resources/runtime"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/Nosmoht/talos-mcp-server/internal/talos"
)

func TestParseCOSIURI(t *testing.T) {
	tests := []struct {
		name     string
		uri      string
		wantNode string
		wantNS   string
		wantType string
		wantID   string
		wantErr  bool
	}{
		{
			name:     "list - valid 2-segment path",
			uri:      "talos://10.0.0.1/resource/runtime/MachineStatus",
			wantNode: "10.0.0.1",
			wantNS:   "runtime",
			wantType: "MachineStatus",
		},
		{
			name:     "get - valid 3-segment path",
			uri:      "talos://10.0.0.1/resource/runtime/MachineStatuses.runtime.talos.dev/talos-cp-1",
			wantNode: "10.0.0.1",
			wantNS:   "runtime",
			wantType: "MachineStatuses.runtime.talos.dev",
			wantID:   "talos-cp-1",
		},
		{
			name:     "network namespace",
			uri:      "talos://192.168.1.10/resource/network/LinkStatuses.net.talos.dev/eth0",
			wantNode: "192.168.1.10",
			wantNS:   "network",
			wantType: "LinkStatuses.net.talos.dev",
			wantID:   "eth0",
		},
		{
			name:     "alias type",
			uri:      "talos://node1/resource/runtime/ms",
			wantNode: "node1",
			wantNS:   "runtime",
			wantType: "ms",
		},
		{
			name:     "ip with port - list",
			uri:      "talos://10.0.0.1:50000/resource/runtime/MachineStatus",
			wantNode: "10.0.0.1",
			wantNS:   "runtime",
			wantType: "MachineStatus",
		},
		{
			name:     "hostname with port - get",
			uri:      "talos://node1:50000/resource/network/LinkStatus/eth0",
			wantNode: "node1",
			wantNS:   "network",
			wantType: "LinkStatus",
			wantID:   "eth0",
		},
		{
			name:    "wrong scheme",
			uri:     "http://10.0.0.1/resource/runtime/MachineStatus",
			wantErr: true,
		},
		{
			name:    "missing node",
			uri:     "talos:///resource/runtime/MachineStatus",
			wantErr: true,
		},
		{
			name:    "missing type segment",
			uri:     "talos://10.0.0.1/resource/runtime",
			wantErr: true,
		},
		{
			name:    "wrong path prefix",
			uri:     "talos://10.0.0.1/resources/runtime/MachineStatus",
			wantErr: true,
		},
		{
			name:    "empty string",
			uri:     "",
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			node, ns, typ, id, err := ParseCOSIURI(tc.uri)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("ParseCOSIURI(%q) = nil error, want error", tc.uri)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseCOSIURI(%q) error: %v", tc.uri, err)
			}
			if node != tc.wantNode {
				t.Errorf("node = %q, want %q", node, tc.wantNode)
			}
			if ns != tc.wantNS {
				t.Errorf("namespace = %q, want %q", ns, tc.wantNS)
			}
			if typ != tc.wantType {
				t.Errorf("type = %q, want %q", typ, tc.wantType)
			}
			if id != tc.wantID {
				t.Errorf("id = %q, want %q", id, tc.wantID)
			}
		})
	}
}

func TestRegister_NoPanic(t *testing.T) {
	server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.0.0"}, nil)
	// Register with nil client — we only validate that URI templates are valid (no panic)
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Register panicked: %v", r)
		}
	}()
	Register(server, nil, nil)
}

// fakeClient serves resource kinds from a map and resources from state;
// resolveErr, when set, is returned by every resolve.
type fakeClient struct {
	state      state.State
	kinds      map[resource.Type]*meta.ResourceDefinition
	resolveErr error
}

var _ Client = (*fakeClient)(nil)

func (f *fakeClient) Version(context.Context, ...grpc.CallOption) (*machineapi.VersionResponse, error) {
	return nil, errors.New("fakeClient.Version not used by these tests")
}

func (f *fakeClient) ResolveResourceKind(_ context.Context, _ *resource.Namespace, resourceType resource.Type) (*meta.ResourceDefinition, error) {
	if f.resolveErr != nil {
		return nil, f.resolveErr
	}
	if rd, ok := f.kinds[resourceType]; ok {
		return rd, nil
	}
	// The machinery client reports an unregistered type this way (client/resources.go).
	return nil, status.Errorf(codes.NotFound, "resource %q is not registered", resourceType)
}

func (f *fakeClient) COSIState() state.State { return f.state }

const testNode = "192.0.2.5"

// newCOSIHandlers returns handlers allowing only testNode whose client holds
// MetaKey resources with seededIDs in an in-memory COSI state.
func newCOSIHandlers(t *testing.T, seededIDs []string) *Handlers {
	t.Helper()

	ctx := context.Background()
	st := state.WrapCore(namespaced.NewState(inmem.Build))
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
	allow, err := talos.ParseNodeAllowlist(testNode)
	if err != nil {
		t.Fatalf("setup: parse allowlist: %v", err)
	}

	return &Handlers{
		Client:       &fakeClient{state: st, kinds: map[resource.Type]*meta.ResourceDefinition{"MetaKey": rd}},
		AllowedNodes: allow,
	}
}

// TestHandleCOSIResource_NodeOutsideAllowlistRefused: a resource URI naming a
// node outside TALOS_MCP_ALLOWED_NODES is refused (README §Configuration).
func TestHandleCOSIResource_NodeOutsideAllowlistRefused(t *testing.T) {
	t.Parallel()

	h := newCOSIHandlers(t, nil)
	_, err := h.handleCOSIResource(context.Background(), &mcp.ReadResourceRequest{
		Params: &mcp.ReadResourceParams{URI: "talos://192.0.2.99/resource/runtime/MetaKey"},
	})
	if err == nil || !strings.Contains(err.Error(), "node not allowed") {
		t.Errorf("README TALOS_MCP_ALLOWED_NODES: want node-not-allowed refusal, got %v", err)
	}
}

// TestHandleCOSIResource_ReadsClientState: a resource URI with an id returns
// that resource and one without an id lists all of the type (talos-resource-get
// and talos-resource-list templates).
func TestHandleCOSIResource_ReadsClientState(t *testing.T) {
	t.Parallel()

	seededIDs := []string{"seeded-a", "seeded-b"}
	h := newCOSIHandlers(t, seededIDs)

	tests := []struct {
		name    string
		uri     string
		wantIDs []string
	}{
		{name: "get by id", uri: "talos://" + testNode + "/resource/runtime/MetaKey/" + seededIDs[1], wantIDs: seededIDs[1:]},
		{name: "list", uri: "talos://" + testNode + "/resource/runtime/MetaKey", wantIDs: seededIDs},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			res, err := h.handleCOSIResource(context.Background(), &mcp.ReadResourceRequest{
				Params: &mcp.ReadResourceParams{URI: tc.uri},
			})
			if err != nil {
				t.Fatalf("%s %s: unexpected error %v", tc.name, tc.uri, err)
			}
			if len(res.Contents) == 0 {
				t.Fatalf("%s %s: empty resource contents", tc.name, tc.uri)
			}

			var payload struct {
				Items []struct {
					Metadata struct {
						ID string `json:"id"`
					} `json:"metadata"`
				} `json:"items"`
			}
			if err := json.Unmarshal([]byte(res.Contents[0].Text), &payload); err != nil {
				t.Fatalf("%s: decode resource payload: %v", tc.name, err)
			}
			var gotIDs []string
			for _, item := range payload.Items {
				gotIDs = append(gotIDs, item.Metadata.ID)
			}
			if !slices.Equal(gotIDs, tc.wantIDs) {
				t.Errorf("resource template %s: want ids %v got %v", tc.name, tc.wantIDs, gotIDs)
			}
		})
	}
}
