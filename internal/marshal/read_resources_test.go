package marshal

import (
	"context"
	"testing"

	"github.com/cosi-project/runtime/pkg/state"
	"github.com/cosi-project/runtime/pkg/state/impl/inmem"
	"github.com/cosi-project/runtime/pkg/state/impl/namespaced"
	runtimeres "github.com/siderolabs/talos/pkg/machinery/resources/runtime"
)

const readResourcesLimit = 2

func seededMetaKeyState(t *testing.T, ids ...string) state.State {
	t.Helper()

	st := state.WrapCore(namespaced.NewState(inmem.Build))
	for _, id := range ids {
		if err := st.Create(context.Background(), runtimeres.NewMetaKey(runtimeres.NamespaceName, id)); err != nil {
			t.Fatalf("setup: seed MetaKey %s: %v", id, err)
		}
	}
	return st
}

func TestReadResourcesLimitTruncates(t *testing.T) {
	st := seededMetaKeyState(t, "seeded-a", "seeded-b", "seeded-c")

	items, truncated, err := ReadResources(context.Background(), st, runtimeres.NamespaceName, runtimeres.MetaKeyType, "", readResourcesLimit)
	if err != nil {
		t.Fatalf("spec AC-39: precondition expected the list to succeed got %v", err)
	}
	if len(items) != readResourcesLimit || !truncated {
		t.Errorf("spec AC-39: expected %d items and truncated=true got %d items and truncated=%t", readResourcesLimit, len(items), truncated)
	}
}

func TestReadResourcesAbsentIDIsNotFound(t *testing.T) {
	st := seededMetaKeyState(t, "seeded-a")

	_, _, err := ReadResources(context.Background(), st, runtimeres.NamespaceName, runtimeres.MetaKeyType, "absent", 0)
	if !state.IsNotFoundError(err) {
		t.Errorf("spec AC-40: expected an error for which state.IsNotFoundError is true got %v", err)
	}
}
