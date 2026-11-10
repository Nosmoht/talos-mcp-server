package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/cosi-project/runtime/pkg/state"
	"github.com/cosi-project/runtime/pkg/state/impl/inmem"
	"github.com/cosi-project/runtime/pkg/state/impl/namespaced"
	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	"github.com/siderolabs/talos/pkg/machinery/config/configloader"
	talosconfig "github.com/siderolabs/talos/pkg/machinery/resources/config"
	"google.golang.org/grpc"
)

// Diagnostic spec references: the PatchConfigArgs.Patch schema text in
// lifecycle.go, descPatchConfig in register.go, and CLAUDE.md § Safety.
const (
	specMerge    = "spec PatchConfigArgs.Patch (fetch, merge, submit)"
	specFormats  = "spec descPatchConfig (strategic merge or RFC 6902)"
	specMultiDoc = "spec descPatchConfig (RFC 6902 rejected on multi-document config)"
	specDryRun   = "spec CLAUDE.md § Safety (dry_run defaults to true)"
)

const (
	patchTestNode = "192.0.2.10"

	singleDocConfig = "version: v1alpha1\nmachine:\n  type: worker\n"

	multiDocConfig = singleDocConfig +
		"---\napiVersion: v1alpha1\nkind: KubeNodeConfig\nlabels:\n  existing: \"true\"\n"

	baseConfigMarker = "type: worker"
)

// patchClient serves a fixed machine config through COSIState and records the
// ApplyConfiguration request; every other call panics via mockClient.
type patchClient struct {
	*mockClient

	st       state.State
	gotApply *machineapi.ApplyConfigurationRequest
}

func (c *patchClient) COSIState() state.State { return c.st }

func (c *patchClient) ApplyConfiguration(_ context.Context, req *machineapi.ApplyConfigurationRequest, _ ...grpc.CallOption) (*machineapi.ApplyConfigurationResponse, error) {
	c.gotApply = req
	return &machineapi.ApplyConfigurationResponse{}, nil
}

func newPatchClient(t *testing.T, config string) *patchClient {
	t.Helper()

	st := state.WrapCore(namespaced.NewState(inmem.Build))
	if err := st.Create(context.Background(), newAnnotatedResource(config)); err != nil {
		t.Fatalf("seed machine config: %v", err)
	}

	md := newAnnotatedResource(config).Metadata()
	if md.Namespace() != talosconfig.NamespaceName || md.Type() != talosconfig.MachineConfigType || md.ID() != talosconfig.ActiveID {
		t.Fatalf("fixture identity %s/%s/%s does not match the resource HandlePatchConfig reads", md.Namespace(), md.Type(), md.ID())
	}

	return &patchClient{mockClient: &mockClient{}, st: st}
}

func runDryRunPatch(t *testing.T, config, patch string) (*patchClient, error) {
	t.Helper()

	client := newPatchClient(t, config)
	h := &Handlers{Client: client}
	dryRun := true

	_, _, err := h.HandlePatchConfig(context.Background(), nil, PatchConfigArgs{
		Nodes:  []string{patchTestNode},
		Patch:  patch,
		DryRun: &dryRun,
	})

	return client, err
}

// appliedConfig returns the config HandlePatchConfig sent to ApplyConfiguration,
// after checking that it was sent as a dry run and loads as a machine config.
func appliedConfig(t *testing.T, client *patchClient) string {
	t.Helper()

	if client.gotApply == nil {
		t.Fatalf("%s: expected an ApplyConfiguration call, got none", specMerge)
	}
	if !client.gotApply.GetDryRun() {
		t.Errorf("%s: expected DryRun=true, got false", specDryRun)
	}
	if _, err := configloader.NewFromBytes(client.gotApply.GetData()); err != nil {
		t.Fatalf("%s: expected a loadable machine config, got %v:\n%s", specMerge, err, client.gotApply.GetData())
	}

	return string(client.gotApply.GetData())
}

func assertContains(t *testing.T, spec, got, want string) {
	t.Helper()

	if !strings.Contains(got, want) {
		t.Errorf("%s: expected applied config to contain %q, got:\n%s", spec, want, got)
	}
}

func TestGivenSingleDocumentConfig_WhenStrategicMergePatchSetsNodeLabel_ThenAppliedConfigKeepsBaseAndAddsLabel(t *testing.T) {
	client, err := runDryRunPatch(t, singleDocConfig, `{"machine":{"nodeLabels":{"integration-test":"true"}}}`)
	if err != nil {
		t.Fatalf("%s: expected no error, got %v", specFormats, err)
	}

	got := appliedConfig(t, client)
	assertContains(t, specMerge, got, baseConfigMarker)
	assertContains(t, specMerge, got, `integration-test: "true"`)
}

func TestGivenMultiDocumentConfig_WhenKubeNodeConfigPatchSetsLabel_ThenAppliedConfigKeepsExistingAndAddsLabel(t *testing.T) {
	client, err := runDryRunPatch(t, multiDocConfig, "apiVersion: v1alpha1\nkind: KubeNodeConfig\nlabels:\n  integration-test: \"true\"\n")
	if err != nil {
		t.Fatalf("%s: expected no error, got %v", specFormats, err)
	}

	got := appliedConfig(t, client)
	assertContains(t, specMerge, got, baseConfigMarker)
	assertContains(t, specMerge, got, `existing: "true"`)
	assertContains(t, specMerge, got, `integration-test: "true"`)
}

func TestGivenSingleDocumentConfig_WhenJSON6902PatchAddsNodeLabel_ThenAppliedConfigKeepsBaseAndAddsLabel(t *testing.T) {
	client, err := runDryRunPatch(t, singleDocConfig, `[{"op":"add","path":"/machine/nodeLabels","value":{"json6902-test":"true"}}]`)
	if err != nil {
		t.Fatalf("%s: expected no error, got %v", specFormats, err)
	}

	got := appliedConfig(t, client)
	assertContains(t, specMerge, got, baseConfigMarker)
	assertContains(t, specMerge, got, `json6902-test: "true"`)
}

func TestGivenMultiDocumentConfig_WhenJSON6902Patch_ThenRejectedBeforeApply(t *testing.T) {
	client, err := runDryRunPatch(t, multiDocConfig, `[{"op":"add","path":"/machine/nodeLabels","value":{"json6902-test":"true"}}]`)
	if err == nil {
		t.Fatalf("%s: expected an error, got nil", specMultiDoc)
	}
	// configpatcher returns a plain errors.New for this case (no sentinel), so the
	// message is the only way to tell it from other failures that also skip apply.
	if !strings.Contains(err.Error(), "not supported for multi-document machine configuration") {
		t.Errorf("%s: expected the configpatcher multi-document rejection, got %v", specMultiDoc, err)
	}
	if client.gotApply != nil {
		t.Errorf("%s: expected no ApplyConfiguration call after a failed merge, got one", specMerge)
	}
}
