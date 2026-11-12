package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/cosi-project/runtime/pkg/state"
	"github.com/cosi-project/runtime/pkg/state/impl/inmem"
	"github.com/cosi-project/runtime/pkg/state/impl/namespaced"
	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	"google.golang.org/grpc"
)

const (
	blocklistNode = "192.0.2.50"

	blocklistHostnameDocument = "apiVersion: v1alpha1\nkind: HostnameConfig\nhostname: worker-33\n"
	blocklistNodeLabelsDoc    = "machine:\n  nodeLabels:\n    tier: test\n"

	typedDocumentRefusal = "typed config documents are refused"
)

type blocklistProbeClient struct {
	*mockClient

	st       state.State
	reached  []string
	gotApply *machineapi.ApplyConfigurationRequest
}

func (c *blocklistProbeClient) COSIState() state.State {
	c.reached = append(c.reached, "COSIState")
	return c.st
}

func (c *blocklistProbeClient) ApplyConfiguration(_ context.Context, req *machineapi.ApplyConfigurationRequest, _ ...grpc.CallOption) (*machineapi.ApplyConfigurationResponse, error) {
	c.reached = append(c.reached, "ApplyConfiguration")
	c.gotApply = req
	return &machineapi.ApplyConfigurationResponse{}, nil
}

func newBlocklistProbeClient(t *testing.T, machineConfig string) *blocklistProbeClient {
	t.Helper()

	st := state.WrapCore(namespaced.NewState(inmem.Build))
	if err := st.Create(context.Background(), newAnnotatedResource(machineConfig)); err != nil {
		t.Fatalf("setup: seed machine config: %v", err)
	}
	return &blocklistProbeClient{mockClient: &mockClient{}, st: st}
}

func dryRunPatchUnderBlocklist(t *testing.T, client *blocklistProbeClient, blocked []string, patch string) error {
	t.Helper()

	h := &Handlers{Client: client, BlockedConfigPaths: blocked}
	dryRun := true
	_, _, err := h.HandlePatchConfig(context.Background(), nil, PatchConfigArgs{
		Nodes:  []string{blocklistNode},
		Patch:  patch,
		DryRun: &dryRun,
	})
	return err
}

func TestBlockedPathInLaterPatchDocumentRefused(t *testing.T) {
	const blocked = "machine.network"
	// The later document must use v1alpha1 keys machinery knows, or LoadPatch rejects it before any client call.
	patch := blocklistNodeLabelsDoc + "---\nmachine:\n  network:\n    hostname: worker-33\n"
	client := newBlocklistProbeClient(t, singleDocConfig)

	err := dryRunPatchUnderBlocklist(t, client, []string{blocked}, patch)
	if err == nil || !strings.Contains(err.Error(), blocked) || len(client.reached) != 0 {
		t.Errorf("spec AC-23: expected=refusal naming %q before any Talos client call got=err:%v client calls:%v", blocked, err, client.reached)
	}
}

func TestTypedDocumentRefusedWhileBlocklistSet(t *testing.T) {
	cases := []struct {
		name  string
		patch string
	}{
		{name: "typed document first", patch: blocklistHostnameDocument},
		{name: "typed document after a v1alpha1 document", patch: blocklistNodeLabelsDoc + "---\n" + blocklistHostnameDocument},
		{name: "document carrying only kind", patch: "kind: HostnameConfig\nhostname: worker-33\n"},
		{name: "document carrying only apiVersion", patch: "apiVersion: v1alpha1\nhostname: worker-33\n"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client := newBlocklistProbeClient(t, singleDocConfig)

			err := dryRunPatchUnderBlocklist(t, client, []string{"machine.security"}, tc.patch)
			// LoadPatch also rejects the single-key documents, so only the refusal text separates the blocklist refusal from a parse error.
			if err == nil || !strings.Contains(err.Error(), typedDocumentRefusal) || len(client.reached) != 0 {
				t.Errorf("spec AC-29: case=%s expected=refusal containing %q before any Talos client call got=err:%v client calls:%v", tc.name, typedDocumentRefusal, err, client.reached)
			}
		})
	}
}

func TestTypedDocumentAcceptedWithoutBlocklist(t *testing.T) {
	client := newBlocklistProbeClient(t, singleDocConfig)

	err := dryRunPatchUnderBlocklist(t, client, nil, blocklistHostnameDocument)
	if err != nil || client.gotApply == nil || !client.gotApply.GetDryRun() {
		t.Errorf("spec AC-30: expected=ApplyConfiguration submitted with DryRun=true got=err:%v request:%v", err, client.gotApply)
	}
}
