package tools

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/cosi-project/runtime/pkg/resource"
	"github.com/cosi-project/runtime/pkg/resource/meta"
	"github.com/cosi-project/runtime/pkg/resource/meta/spec"
	"github.com/cosi-project/runtime/pkg/state"
	"github.com/cosi-project/runtime/pkg/state/impl/inmem"
	"github.com/cosi-project/runtime/pkg/state/impl/namespaced"
	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	talosmeta "github.com/siderolabs/talos/pkg/machinery/meta"
	runtimeres "github.com/siderolabs/talos/pkg/machinery/resources/runtime"

	"github.com/Nosmoht/talos-mcp-server/internal/talos"
)

const (
	auditAllowedNode = "192.0.2.1"
	auditOtherNode   = "192.0.2.99"
	auditFingerprint = "ab"
)

type auditOutcomeCase struct {
	name string
	h    func(t *testing.T) *Handlers
	call func(h *Handlers) error
	want string
}

// runAuditOutcomeCases checks the AUDIT_OUTCOME value each exit of tool records
// (.claude/rules/insecure-mode.md §Audit pairing; values: Outcome* in helpers.go).
func runAuditOutcomeCases(t *testing.T, tool string, cases []auditOutcomeCase) {
	t.Helper()

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			log := captureDefaultSlog(t)
			_ = tc.call(tc.h(t))
			if got := auditOutcomeOf(log.String(), tool); got != tc.want {
				t.Errorf("insecure-mode.md §Audit pairing: %s %s: expected outcome %q got %q", tool, tc.name, tc.want, got)
			}
		})
	}
}

func authenticatedWithAllowlist(t *testing.T) *Handlers {
	t.Helper()

	allowlist, err := talos.ParseNodeAllowlist(auditAllowedNode)
	if err != nil {
		t.Fatalf("setup: parse allowlist: %v", err)
	}
	h := safeH()
	h.AllowedNodes = allowlist
	return h
}

func plainHandlers(*testing.T) *Handlers { return safeH() }

func maintenanceWith(fake *fakeMaintenanceClient) func(t *testing.T) *Handlers {
	return func(t *testing.T) *Handlers {
		t.Helper()
		return insecureHandlers(t, &recordingDialer{client: fake})
	}
}

func maintenanceDialFails(t *testing.T) *Handlers {
	t.Helper()
	h := insecureHandlers(t, &recordingDialer{})
	h.dialMaintenance = func(context.Context, string, []byte) (maintenanceClient, error) {
		return nil, errors.New("audit test: dial refused")
	}
	return h
}

var errAuditRPC = errors.New("audit test: rpc failed")

func TestGivenEachVersionExitWhenCalledThenItsOutcomeIsAudited(t *testing.T) {
	call := func(args VersionArgs) func(*Handlers) error {
		return func(h *Handlers) error {
			_, _, err := h.HandleVersion(context.Background(), nil, args)
			return err
		}
	}
	insecure := VersionArgs{Insecure: true, Endpoint: insecureTestEndpoint}

	runAuditOutcomeCases(t, "talos_version", []auditOutcomeCase{
		{"fingerprint without insecure", plainHandlers, call(VersionArgs{CertFingerprint: auditFingerprint}), OutcomeRefusedFPWithoutInsec},
		{"insecure not enabled", plainHandlers, call(insecure), OutcomeRefusedEnable},
		{"maintenance dial fails", maintenanceDialFails, call(insecure), OutcomeDialError},
		{"maintenance rpc fails", maintenanceWith(&fakeMaintenanceClient{rpcErr: errAuditRPC}), call(insecure), OutcomeRPCError},
		{"maintenance ok", maintenanceWith(&fakeMaintenanceClient{version: &machineapi.VersionResponse{}}), call(insecure), OutcomeOK},
		{"node outside allowlist", authenticatedWithAllowlist, call(VersionArgs{Nodes: []string{auditOtherNode}}), OutcomeRefusedAllowlist},
	})
}

func TestGivenEachGetExitWhenCalledThenItsOutcomeIsAudited(t *testing.T) {
	rd, err := meta.NewResourceDefinition(spec.ResourceDefinitionSpec{
		Type:             runtimeres.MetaKeyType,
		DefaultNamespace: runtimeres.NamespaceName,
	})
	if err != nil {
		t.Fatalf("setup: resource definition: %v", err)
	}
	readable := &fakeMaintenanceClient{
		state: state.WrapCore(namespaced.NewState(inmem.Build)),
		kinds: map[resource.Type]*meta.ResourceDefinition{"MetaKey": rd},
	}

	call := func(args GetResourceArgs) func(*Handlers) error {
		return func(h *Handlers) error {
			_, _, err := h.HandleGetResource(context.Background(), nil, args)
			return err
		}
	}
	insecure := GetResourceArgs{ResourceType: "MetaKey", Namespace: runtimeres.NamespaceName, Insecure: true, Endpoint: insecureTestEndpoint}

	runAuditOutcomeCases(t, "talos_get", []auditOutcomeCase{
		{"fingerprint without insecure", plainHandlers, call(GetResourceArgs{ResourceType: "MetaKey", CertFingerprint: auditFingerprint}), OutcomeRefusedFPWithoutInsec},
		{"insecure without endpoint", maintenanceWith(readable), call(GetResourceArgs{ResourceType: "MetaKey", Insecure: true}), OutcomeRefusedEndpointEmpty},
		{"maintenance dial fails", maintenanceDialFails, call(insecure), OutcomeDialError},
		{"maintenance kind unknown", maintenanceWith(&fakeMaintenanceClient{}), call(insecure), OutcomeRPCError},
		{"maintenance ok", maintenanceWith(readable), call(insecure), OutcomeOK},
		{"two nodes", plainHandlers, call(GetResourceArgs{ResourceType: "MetaKey", Nodes: []string{auditAllowedNode, auditOtherNode}}), OutcomeRefusedArgs},
		{"node outside allowlist", authenticatedWithAllowlist, call(GetResourceArgs{ResourceType: "MetaKey", Nodes: []string{auditOtherNode}}), OutcomeRefusedAllowlist},
	})
}

func TestGivenEachMetaExitWhenCalledThenItsOutcomeIsAudited(t *testing.T) {
	reservedKey := fmt.Sprint(uint8(talosmeta.UserReserved1))
	privilegedKey := fmt.Sprint(uint8(talosmeta.Upgrade))

	call := func(args MetaArgs) func(*Handlers) error {
		return func(h *Handlers) error {
			_, _, err := h.HandleMeta(context.Background(), nil, args)
			return err
		}
	}
	insecureWrite := MetaArgs{Action: "write", Key: reservedKey, Value: "v", Confirm: true, Insecure: true, Endpoint: insecureTestEndpoint}
	insecureDelete := MetaArgs{Action: "delete", Key: reservedKey, Confirm: true, Insecure: true, Endpoint: insecureTestEndpoint}

	runAuditOutcomeCases(t, "talos_meta", []auditOutcomeCase{
		{"fingerprint without insecure", plainHandlers, call(MetaArgs{Action: "read", Key: reservedKey, CertFingerprint: auditFingerprint}), OutcomeRefusedFPWithoutInsec},
		{"unparseable key", plainHandlers, call(MetaArgs{Action: "read", Key: "x"}), OutcomeRefusedArgs},
		{"write without confirm", plainHandlers, call(MetaArgs{Action: "write", Key: reservedKey}), OutcomeRefusedConfirm},
		{"write to unlisted privileged key", plainHandlers, call(MetaArgs{Action: "write", Key: privilegedKey, Confirm: true}), OutcomeRefusedMetaKey},
		{"unknown action", plainHandlers, call(MetaArgs{Action: "list", Key: reservedKey}), OutcomeRefusedArgs},
		{"insecure with nodes", maintenanceWith(&fakeMaintenanceClient{}), call(MetaArgs{Action: "read", Key: reservedKey, Nodes: []string{auditAllowedNode}, Insecure: true, Endpoint: insecureTestEndpoint}), OutcomeRefusedNodesExclusive},
		{"maintenance dial fails", maintenanceDialFails, call(insecureWrite), OutcomeDialError},
		{"maintenance write fails", maintenanceWith(&fakeMaintenanceClient{rpcErr: errAuditRPC}), call(insecureWrite), OutcomeRPCError},
		{"maintenance delete ok", maintenanceWith(&fakeMaintenanceClient{}), call(insecureDelete), OutcomeOK},
		{"write to node outside allowlist", authenticatedWithAllowlist, call(MetaArgs{Action: "write", Key: reservedKey, Confirm: true, Nodes: []string{auditOtherNode}}), OutcomeRefusedAllowlist},
	})
}

func TestGivenEachApplyConfigRefusalWhenCalledThenItsOutcomeIsAudited(t *testing.T) {
	configFile := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configFile, []byte("machine: {}"), 0o600); err != nil {
		t.Fatalf("setup: write temp config: %v", err)
	}
	applyNow := false

	call := func(args ApplyConfigArgs) func(*Handlers) error {
		return func(h *Handlers) error {
			_, _, err := h.HandleApplyConfig(context.Background(), nil, args)
			return err
		}
	}
	withBlocklist := func(*testing.T) *Handlers {
		h := safeH()
		h.BlockedConfigPaths = []string{"machine.network"}
		return h
	}
	insecure := ApplyConfigArgs{ConfigFile: configFile, Insecure: true, Endpoint: insecureTestEndpoint}

	runAuditOutcomeCases(t, "talos_apply_config", []auditOutcomeCase{
		{"fingerprint without insecure", plainHandlers, call(ApplyConfigArgs{ConfigFile: configFile, CertFingerprint: auditFingerprint}), OutcomeRefusedFPWithoutInsec},
		{"blocklist set", withBlocklist, call(ApplyConfigArgs{ConfigFile: configFile}), OutcomeRefusedArgs},
		{"two nodes", plainHandlers, call(ApplyConfigArgs{ConfigFile: configFile, Nodes: []string{auditAllowedNode, auditOtherNode}}), OutcomeRefusedArgs},
		{"apply without confirm", plainHandlers, call(ApplyConfigArgs{ConfigFile: configFile, DryRun: &applyNow}), OutcomeRefusedConfirm},
		{"node outside allowlist", authenticatedWithAllowlist, call(ApplyConfigArgs{ConfigFile: configFile, Nodes: []string{auditOtherNode}}), OutcomeRefusedAllowlist},
		{"relative config path", plainHandlers, call(ApplyConfigArgs{ConfigFile: "config.yaml"}), OutcomeRefusedArgs},
		{"reboot mode", plainHandlers, call(ApplyConfigArgs{ConfigFile: configFile, Mode: "reboot"}), OutcomeRefusedArgs},
		{"insecure not enabled", plainHandlers, call(insecure), OutcomeRefusedEnable},
		{"maintenance apply without confirm", maintenanceWith(&fakeMaintenanceClient{}), call(ApplyConfigArgs{ConfigFile: configFile, DryRun: &applyNow, Insecure: true, Endpoint: insecureTestEndpoint}), OutcomeRefusedConfirm},
		{"maintenance dial fails", maintenanceDialFails, call(insecure), OutcomeDialError},
	})
}
