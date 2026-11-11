package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/cosi-project/runtime/pkg/resource"
	"github.com/cosi-project/runtime/pkg/resource/meta"
	"github.com/cosi-project/runtime/pkg/resource/meta/spec"
	"github.com/cosi-project/runtime/pkg/state"
	"github.com/cosi-project/runtime/pkg/state/impl/inmem"
	"github.com/cosi-project/runtime/pkg/state/impl/namespaced"
	runtimeres "github.com/siderolabs/talos/pkg/machinery/resources/runtime"
)

const uncappedSeededResources = 101

type uncappedListClient struct {
	*mockClient

	st state.State
	rd *meta.ResourceDefinition
}

func (c *uncappedListClient) COSIState() state.State { return c.st }

func (c *uncappedListClient) ResolveResourceKind(_ context.Context, _ *resource.Namespace, _ resource.Type) (*meta.ResourceDefinition, error) {
	return c.rd, nil
}

func TestGetToolListUncapped(t *testing.T) {
	ctx := context.Background()
	st := state.WrapCore(namespaced.NewState(inmem.Build))
	for i := range uncappedSeededResources {
		id := fmt.Sprintf("seeded-%03d", i)
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

	h := &Handlers{Client: &uncappedListClient{mockClient: &mockClient{}, st: st, rd: rd}}
	_, out, err := h.HandleGetResource(ctx, nil, GetResourceArgs{
		ResourceType: "MetaKey",
		Namespace:    string(runtimeres.NamespaceName),
	})
	if err != nil {
		t.Fatalf("spec AC-38: precondition expected talos_get to list the type got %v", err)
	}

	raw, err := json.Marshal(out)
	if err != nil {
		t.Fatalf("spec AC-38: precondition expected JSON-encodable output got %v", err)
	}
	var payload struct {
		Items []json.RawMessage `json:"items"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("spec AC-38: precondition expected an items envelope got %v (%s)", err, raw)
	}

	if len(payload.Items) != uncappedSeededResources {
		t.Errorf("spec AC-38: expected %d items got %d", uncappedSeededResources, len(payload.Items))
	}
}
