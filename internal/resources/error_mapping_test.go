package resources

import (
	"context"
	"errors"
	"testing"

	"github.com/cosi-project/runtime/pkg/resource"
	"github.com/cosi-project/runtime/pkg/state"
	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	talosclient "github.com/siderolabs/talos/pkg/machinery/client"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// specErrorMapping: only a true not-found becomes ResourceNotFoundError. Talos
// denies sensitive resources to non-admin roles with authz.ErrNotAuthorized.
const specErrorMapping = "spec #265 triage scope 3 (resource read error mapping)"

var errNotAuthorized = status.Error(codes.PermissionDenied, "not authorized")

// deniedState rejects every Get the way Talos rejects a sensitive resource.
type deniedState struct {
	state.State
}

func (deniedState) Get(context.Context, resource.Pointer, ...state.GetOption) (resource.Resource, error) {
	return nil, errNotAuthorized
}

func readCOSIResource(h *Handlers, uri string) error {
	_, err := h.handleCOSIResource(context.Background(), &mcp.ReadResourceRequest{
		Params: &mcp.ReadResourceParams{URI: uri},
	})

	return err
}

func assertDeniedNotNotFound(t *testing.T, err error) {
	t.Helper()

	if err == nil {
		t.Fatalf("%s: expected an error, got nil", specErrorMapping)
	}

	var rpcErr *jsonrpc.Error
	if errors.As(err, &rpcErr) && rpcErr.Code == jsonrpc.CodeInvalidParams {
		t.Errorf("%s: expected PermissionDenied not to be reported as not found, got %v", specErrorMapping, err)
	}

	if got := talosclient.StatusCode(err); got != codes.PermissionDenied {
		t.Errorf("%s: expected the returned error to keep code PermissionDenied, got %v (%v)", specErrorMapping, got, err)
	}
}

func TestGivenResolverDeniesAccess_WhenResourceIsRead_ThenPermissionErrorIsReturned(t *testing.T) {
	t.Parallel()

	h := newCOSIHandlers(t, []string{"seeded-a"})
	h.Client.(*fakeClient).resolveErr = errNotAuthorized

	assertDeniedNotNotFound(t, readCOSIResource(h, "talos://"+testNode+"/resource/runtime/MetaKey/seeded-a"))
}

func TestGivenStateDeniesGet_WhenResourceIsRead_ThenPermissionErrorIsReturned(t *testing.T) {
	t.Parallel()

	h := newCOSIHandlers(t, []string{"seeded-a"})
	fc := h.Client.(*fakeClient)
	fc.state = deniedState{State: fc.state}

	assertDeniedNotNotFound(t, readCOSIResource(h, "talos://"+testNode+"/resource/runtime/MetaKey/seeded-a"))
}
