package config

import (
	"os"
	"strings"
	"testing"
)

// profileEnvKeys lists every env var LoadSafetyProfile consults. Tests call
// clearProfileEnv to start from a known-unset baseline.
var profileEnvKeys = []string{
	"TALOS_MCP_SAFETY_PROFILE",
	"TALOS_MCP_READ_ONLY",
	"TALOS_MCP_ALLOW_CLUSTER_WIDE",
	"TALOS_MCP_ENABLE_GEN",
	"TALOS_MCP_SKIP_VERSION_CHECK",
}

// clearProfileEnv records each var's current state, unsets it, and registers a
// cleanup that restores the original value. Safe for parallel package testing
// when combined with t.Setenv in subtests (t.Setenv asserts serial execution).
func clearProfileEnv(t *testing.T) {
	t.Helper()
	for _, k := range profileEnvKeys {
		orig, had := os.LookupEnv(k)
		if err := os.Unsetenv(k); err != nil {
			t.Fatalf("unset %s: %v", k, err)
		}
		if had {
			t.Cleanup(func() { _ = os.Setenv(k, orig) })
		}
	}
}

func TestLoadSafetyProfile(t *testing.T) {
	tests := []struct {
		name          string
		env           map[string]string
		wantErrSubstr string
		wantProfile   string
		wantReadOnly  bool
		wantCluster   bool
		wantGen       bool
		wantSkipVer   bool
		wantOverrides []string
	}{
		{
			name:        "unset defaults match pre-profile behaviour",
			env:         map[string]string{},
			wantProfile: ProfileNone,
		},
		{
			name:         "conservative profile sets read_only=true",
			env:          map[string]string{"TALOS_MCP_SAFETY_PROFILE": "conservative"},
			wantProfile:  ProfileConservative,
			wantReadOnly: true,
		},
		{
			name:        "standard profile keeps all flags false",
			env:         map[string]string{"TALOS_MCP_SAFETY_PROFILE": "standard"},
			wantProfile: ProfileStandard,
		},
		{
			name:        "expert profile enables cluster_wide and gen",
			env:         map[string]string{"TALOS_MCP_SAFETY_PROFILE": "expert"},
			wantProfile: ProfileExpert,
			wantCluster: true,
			wantGen:     true,
		},
		{
			name: "individual override beats profile",
			env: map[string]string{
				"TALOS_MCP_SAFETY_PROFILE":     "conservative",
				"TALOS_MCP_READ_ONLY":          "false",
				"TALOS_MCP_ALLOW_CLUSTER_WIDE": "true",
			},
			wantProfile:   ProfileConservative,
			wantReadOnly:  false,
			wantCluster:   true,
			wantOverrides: []string{"read_only=false", "allow_cluster_wide=true"},
		},
		{
			name: "individual vars work without a profile (backwards compat)",
			env: map[string]string{
				"TALOS_MCP_READ_ONLY":          "true",
				"TALOS_MCP_SKIP_VERSION_CHECK": "true",
			},
			wantProfile:   ProfileNone,
			wantReadOnly:  true,
			wantSkipVer:   true,
			wantOverrides: []string{"read_only=true", "skip_version_check=true"},
		},
		{
			name:          "non-'true' value maps to false (preserves strict legacy semantics)",
			env:           map[string]string{"TALOS_MCP_READ_ONLY": "1"},
			wantProfile:   ProfileNone,
			wantReadOnly:  false,
			wantOverrides: []string{"read_only=false"},
		},
		{
			name:          "invalid profile returns error",
			env:           map[string]string{"TALOS_MCP_SAFETY_PROFILE": "paranoid"},
			wantErrSubstr: `invalid TALOS_MCP_SAFETY_PROFILE "paranoid"`,
		},
		{
			name:        "profile name is case-insensitive",
			env:         map[string]string{"TALOS_MCP_SAFETY_PROFILE": "EXPERT"},
			wantProfile: ProfileExpert,
			wantCluster: true,
			wantGen:     true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			clearProfileEnv(t)
			for k, v := range tc.env {
				t.Setenv(k, v)
			}

			got, err := LoadSafetyProfile()
			if tc.wantErrSubstr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tc.wantErrSubstr)
				}
				if !strings.Contains(err.Error(), tc.wantErrSubstr) {
					t.Fatalf("error %q does not contain %q", err.Error(), tc.wantErrSubstr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if got.Profile != tc.wantProfile {
				t.Errorf("Profile = %q, want %q", got.Profile, tc.wantProfile)
			}
			if got.ReadOnly != tc.wantReadOnly {
				t.Errorf("ReadOnly = %t, want %t", got.ReadOnly, tc.wantReadOnly)
			}
			if got.AllowClusterWide != tc.wantCluster {
				t.Errorf("AllowClusterWide = %t, want %t", got.AllowClusterWide, tc.wantCluster)
			}
			if got.EnableGen != tc.wantGen {
				t.Errorf("EnableGen = %t, want %t", got.EnableGen, tc.wantGen)
			}
			if got.SkipVersionCheck != tc.wantSkipVer {
				t.Errorf("SkipVersionCheck = %t, want %t", got.SkipVersionCheck, tc.wantSkipVer)
			}
			if tc.wantOverrides != nil && !slicesEqual(got.Overrides, tc.wantOverrides) {
				t.Errorf("Overrides = %v, want %v", got.Overrides, tc.wantOverrides)
			}
		})
	}
}

func TestSafetyProfileLogFields(t *testing.T) {
	p := &SafetyProfile{
		Profile:          ProfileExpert,
		ReadOnly:         false,
		AllowClusterWide: true,
		EnableGen:        true,
		SkipVersionCheck: false,
		Overrides:        []string{"read_only=false"},
	}

	fields := p.LogFields()
	if len(fields)%2 != 0 {
		t.Fatalf("LogFields must return even-length key/value pairs, got %d", len(fields))
	}

	want := map[string]any{
		"profile":            ProfileExpert,
		"read_only":          false,
		"allow_cluster_wide": true,
		"enable_gen":         true,
		"skip_version_check": false,
		"overrides":          "read_only=false",
	}
	seen := make(map[string]bool, len(want))
	for i := 0; i < len(fields); i += 2 {
		key, ok := fields[i].(string)
		if !ok {
			t.Fatalf("LogFields key at %d is not a string: %T", i, fields[i])
		}
		expected, present := want[key]
		if !present {
			t.Errorf("unexpected LogFields key %q", key)
			continue
		}
		if fields[i+1] != expected {
			t.Errorf("LogFields[%q] = %v, want %v", key, fields[i+1], expected)
		}
		seen[key] = true
	}
	for k := range want {
		if !seen[k] {
			t.Errorf("LogFields is missing key %q", k)
		}
	}
}

func slicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
