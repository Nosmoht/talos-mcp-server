package config

import (
	"strings"
	"testing"
)

type overrideFlag struct {
	envKey string
	get    func(*SafetyProfile) bool
}

var (
	overrideReadOnly         = overrideFlag{envKey: "TALOS_MCP_READ_ONLY", get: func(p *SafetyProfile) bool { return p.ReadOnly }}
	overrideAllowClusterWide = overrideFlag{envKey: "TALOS_MCP_ALLOW_CLUSTER_WIDE", get: func(p *SafetyProfile) bool { return p.AllowClusterWide }}
	overrideEnableGen        = overrideFlag{envKey: "TALOS_MCP_ENABLE_GEN", get: func(p *SafetyProfile) bool { return p.EnableGen }}
	overrideSkipVersionCheck = overrideFlag{envKey: "TALOS_MCP_SKIP_VERSION_CHECK", get: func(p *SafetyProfile) bool { return p.SkipVersionCheck }}
	overrideEnableInsecure   = overrideFlag{envKey: "TALOS_MCP_ENABLE_INSECURE", get: func(p *SafetyProfile) bool { return p.EnableInsecure }}

	overrideFlags = []overrideFlag{overrideReadOnly, overrideAllowClusterWide, overrideEnableGen, overrideSkipVersionCheck, overrideEnableInsecure}

	parseBoolTrueSpellings  = []string{"1", "t", "T", "TRUE", "true", "True"}
	parseBoolFalseSpellings = []string{"0", "f", "F", "FALSE", "false", "False"}
	parseBoolRejected       = []string{"yes", "on", "2", " true"}
)

func loadProfileWith(t *testing.T, profile string, flag overrideFlag, value string) (*SafetyProfile, error) {
	t.Helper()

	clearProfileEnv(t)
	if profile != "" {
		t.Setenv("TALOS_MCP_SAFETY_PROFILE", profile)
	}
	t.Setenv(flag.envKey, value)
	return LoadSafetyProfile()
}

func TestOverrideTrueSpellingsEnableFlag(t *testing.T) {
	cases := []struct {
		flag    overrideFlag
		profile string
	}{
		{flag: overrideReadOnly, profile: ProfileStandard},
		{flag: overrideAllowClusterWide, profile: ""},
		{flag: overrideEnableGen, profile: ProfileConservative},
		{flag: overrideSkipVersionCheck, profile: ProfileExpert},
		{flag: overrideEnableInsecure, profile: ""},
	}

	for _, tc := range cases {
		for _, spelling := range parseBoolTrueSpellings {
			name := tc.flag.envKey + "=" + spelling + "/profile=" + tc.profile
			t.Run(name, func(t *testing.T) {
				p, err := loadProfileWith(t, tc.profile, tc.flag, spelling)
				if err != nil || !tc.flag.get(p) {
					t.Errorf("spec AC-21: case=%s expected=flag true got=err:%v profile:%+v", name, err, p)
				}
			})
		}
	}
}

func TestOverrideFalseSpellingsDisableFlag(t *testing.T) {
	cases := []struct {
		flag    overrideFlag
		profile string
	}{
		{flag: overrideReadOnly, profile: ProfileConservative},
		{flag: overrideAllowClusterWide, profile: ProfileExpert},
		{flag: overrideEnableGen, profile: ProfileExpert},
		{flag: overrideSkipVersionCheck, profile: ""},
		{flag: overrideEnableInsecure, profile: ProfileExpert},
	}

	for _, tc := range cases {
		for _, spelling := range parseBoolFalseSpellings {
			name := tc.flag.envKey + "=" + spelling + "/profile=" + tc.profile
			t.Run(name, func(t *testing.T) {
				p, err := loadProfileWith(t, tc.profile, tc.flag, spelling)
				if err != nil || tc.flag.get(p) {
					t.Errorf("spec AC-22: case=%s expected=flag false got=err:%v profile:%+v", name, err, p)
				}
			})
		}
	}
}

func TestOverrideUnrecognisedValueRefused(t *testing.T) {
	for _, flag := range overrideFlags {
		for _, value := range parseBoolRejected {
			name := flag.envKey + "=" + value
			t.Run(name, func(t *testing.T) {
				_, err := loadProfileWith(t, "", flag, value)
				if err == nil || !strings.Contains(err.Error(), flag.envKey) {
					t.Errorf("spec AC-33: case=%q expected=error naming %s got=%v", name, flag.envKey, err)
				}
			})
		}
	}
}

func TestOverrideEmptyValueRefused(t *testing.T) {
	for _, profile := range []string{ProfileConservative, ProfileExpert, ""} {
		for _, flag := range overrideFlags {
			name := flag.envKey + "=/profile=" + profile
			t.Run(name, func(t *testing.T) {
				p, err := loadProfileWith(t, profile, flag, "")
				if err == nil || !strings.Contains(err.Error(), flag.envKey) {
					t.Errorf("spec AC-34: case=%s expected=error naming %s got=err:%v profile:%+v", name, flag.envKey, err, p)
				}
			})
		}
	}
}
