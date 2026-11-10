package tools

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// wireMaintenanceEndpoint is the only entry of the maintenance allowlist the
// AC-30 handlers install (RFC 5737, spec.md A7).
const wireMaintenanceEndpoint = "192.0.2.20"

// wireConfigFile writes a readable machine config for talos_apply_config.
func wireConfigFile(t *testing.T) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "machine.yaml")
	if err := os.WriteFile(path, []byte("version: v1alpha1\nmachine:\n  type: worker\n"), 0o600); err != nil {
		t.Fatalf("setup: write config file: %v", err)
	}
	return path
}

// assertRebootModeRefusal checks that err refuses mode "reboot" with the
// guidance to apply with no_reboot and then use talos_reboot.
func assertRebootModeRefusal(t *testing.T, ac string, err error) {
	t.Helper()

	if err == nil {
		t.Errorf("spec ac.md %s: expected=mode \"reboot\" is refused got=success", ac)
		return
	}
	for _, want := range []string{"no_reboot", "talos_reboot"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("spec ac.md %s: expected=error text containing %q got=%q", ac, want, err.Error())
		}
	}
}

// TestSpecAC28_GivenPatchConfigRebootMode_WhenCalled_ThenRefusedWithNoRebootGuidance_AND_NoTalosAPIRequest
// covers ac.md AC-28: talos_patch_config refuses mode "reboot" with the
// no_reboot + talos_reboot guidance and makes no Talos API call.
func TestSpecAC28_GivenPatchConfigRebootMode_WhenCalled_ThenRefusedWithNoRebootGuidance_AND_NoTalosAPIRequest(t *testing.T) {
	endpoint, client := startWireEndpoint(t, nil)

	_, _, err := wireHandlers(client).HandlePatchConfig(wireCtx(t), nil, PatchConfigArgs{
		Patch: `[{"op":"add","path":"/machine/network/hostname","value":"ac28"}]`,
		Mode:  "reboot",
		Nodes: []string{wireNodeA},
	})

	assertRebootModeRefusal(t, "AC-28", err)
	if reqs := endpoint.recorded(); len(reqs) != 0 {
		t.Errorf("spec ac.md AC-28: expected=no Talos API call got=%d requests %v", len(reqs), reqs)
	}
}

// TestSpecAC29_GivenAuthenticatedApplyConfigRebootMode_WhenCalled_ThenRefusedWithNoRebootGuidance_AND_NoTalosAPIRequest
// covers ac.md AC-29: authenticated talos_apply_config refuses mode "reboot"
// with the no_reboot + talos_reboot guidance and makes no Talos API call.
func TestSpecAC29_GivenAuthenticatedApplyConfigRebootMode_WhenCalled_ThenRefusedWithNoRebootGuidance_AND_NoTalosAPIRequest(t *testing.T) {
	endpoint, client := startWireEndpoint(t, nil)

	_, _, err := wireHandlers(client).HandleApplyConfig(wireCtx(t), nil, ApplyConfigArgs{
		ConfigFile: wireConfigFile(t),
		Mode:       "reboot",
		Nodes:      []string{wireNodeA},
	})

	assertRebootModeRefusal(t, "AC-29", err)
	if reqs := endpoint.recorded(); len(reqs) != 0 {
		t.Errorf("spec ac.md AC-29: expected=no Talos API call got=%d requests %v", len(reqs), reqs)
	}
}

// TestSpecAC30_GivenInsecureApplyConfigRebootMode_WhenCalled_ThenRefusedWithNoRebootGuidance_AND_NoMaintenanceConnection
// covers ac.md AC-30: maintenance-mode talos_apply_config refuses mode "reboot"
// with the no_reboot + talos_reboot guidance and opens no maintenance connection.
func TestSpecAC30_GivenInsecureApplyConfigRebootMode_WhenCalled_ThenRefusedWithNoRebootGuidance_AND_NoMaintenanceConnection(t *testing.T) {
	_, client := startWireEndpoint(t, nil)
	h := wireHandlers(client)
	h.EnableInsecure = true
	h.InsecureAllowedNodes = wireAllowlist(t, wireMaintenanceEndpoint)
	dials := 0
	h.dialMaintenance = func(_ context.Context, _ string, _ []byte) (maintenanceClient, error) {
		dials++
		return nil, errors.New("maintenance-mode connection opened")
	}

	_, _, err := h.HandleApplyConfig(wireCtx(t), nil, ApplyConfigArgs{
		ConfigFile: wireConfigFile(t),
		Mode:       "reboot",
		Insecure:   true,
		Endpoint:   wireMaintenanceEndpoint,
	})

	assertRebootModeRefusal(t, "AC-30", err)
	if dials != 0 {
		t.Errorf("spec ac.md AC-30: expected=no maintenance-mode connection opened got=%d dials", dials)
	}
}
