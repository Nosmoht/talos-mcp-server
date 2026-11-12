package talos

import (
	"testing"

	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
)

func TestParseApplyModeMapsEveryName(t *testing.T) {
	cases := []struct {
		name string
		want machineapi.ApplyConfigurationRequest_Mode
	}{
		{name: "try", want: machineapi.ApplyConfigurationRequest_TRY},
		{name: "auto", want: machineapi.ApplyConfigurationRequest_AUTO},
		{name: "", want: machineapi.ApplyConfigurationRequest_AUTO},
		{name: "no_reboot", want: machineapi.ApplyConfigurationRequest_NO_REBOOT},
		{name: "staged", want: machineapi.ApplyConfigurationRequest_STAGED},
	}

	for _, tc := range cases {
		t.Run("mode="+tc.name, func(t *testing.T) {
			got, err := ParseApplyMode(tc.name)
			if err != nil || got != tc.want {
				t.Errorf("spec AC-56: case=%q expected=%s without error got=%s err:%v", tc.name, tc.want, got, err)
			}
		})
	}
}
