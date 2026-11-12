package talos

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
)

var applyModes = map[string]machineapi.ApplyConfigurationRequest_Mode{
	"auto":      machineapi.ApplyConfigurationRequest_AUTO,
	"no_reboot": machineapi.ApplyConfigurationRequest_NO_REBOOT,
	"staged":    machineapi.ApplyConfigurationRequest_STAGED,
	"try":       machineapi.ApplyConfigurationRequest_TRY,
}

// ApplyModeNames returns the accepted apply modes in sorted order.
func ApplyModeNames() []string {
	return slices.Sorted(maps.Keys(applyModes))
}

// ParseApplyMode maps a public apply mode to the protobuf enum; an empty mode means auto.
func ParseApplyMode(mode string) (machineapi.ApplyConfigurationRequest_Mode, error) {
	if mode == "" {
		return machineapi.ApplyConfigurationRequest_AUTO, nil
	}
	// machinery v1.14 deprecates "reboot", so it has a refusal and no applyModes entry.
	if mode == "reboot" {
		return 0, fmt.Errorf("mode \"reboot\" is not supported: apply with mode \"no_reboot\", then reboot the node with talos_reboot")
	}
	m, ok := applyModes[mode]
	if !ok {
		return 0, fmt.Errorf("unknown mode %q: must be one of %s", mode, strings.Join(ApplyModeNames(), ", "))
	}
	return m, nil
}
