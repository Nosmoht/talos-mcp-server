// Package config aggregates startup configuration for the talos-mcp server.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// SafetyProfile aggregates mutating-tool gating flags for the talos-mcp server.
//
// Values can be set three ways, listed highest-priority first:
//
//  1. Individual env vars (TALOS_MCP_READ_ONLY, TALOS_MCP_ALLOW_CLUSTER_WIDE,
//     TALOS_MCP_ENABLE_GEN, TALOS_MCP_SKIP_VERSION_CHECK, TALOS_MCP_ENABLE_INSECURE)
//     — override any profile.
//  2. TALOS_MCP_SAFETY_PROFILE=conservative|standard|expert presets.
//  3. Defaults matching pre-profile behaviour when neither is set.
//
// Profiles are opt-in: operators who have not set TALOS_MCP_SAFETY_PROFILE see
// exactly the same behaviour as before this struct was introduced. Introduction
// of the profile field is therefore a non-breaking change.
type SafetyProfile struct {
	ReadOnly         bool
	AllowClusterWide bool
	EnableGen        bool
	SkipVersionCheck bool
	// EnableInsecure unlocks maintenance-mode operations on tools that accept
	// insecure=true (talos_apply_config, talos_get, talos_version, talos_meta).
	// Bypasses mTLS — the transport is TLS-encrypted but the client presents no
	// cert and (by default) does not verify the server. Operators MUST also set
	// TALOS_MCP_INSECURE_ALLOWED_NODES; main.go log.Fatalf-s on EnableInsecure=true
	// with an unset/empty/over-permissive allowlist.
	EnableInsecure bool

	// Profile records which profile preset was applied before overrides
	// (for startup logging). "none" when TALOS_MCP_SAFETY_PROFILE was unset.
	Profile string

	// Overrides lists the individual env-var overrides that were applied on top
	// of the profile, in the order "var=value" (for startup logging).
	Overrides []string
}

const (
	// ProfileNone is reported when TALOS_MCP_SAFETY_PROFILE was unset — the
	// server derives flags purely from individual env vars and defaults.
	ProfileNone = "none"

	// ProfileConservative sets READ_ONLY=true; ALLOW_CLUSTER_WIDE, ENABLE_GEN,
	// and SKIP_VERSION_CHECK all default to false. Recommended for new deployments.
	ProfileConservative = "conservative"

	// ProfileStandard sets READ_ONLY=false with cluster-wide and gen disabled.
	// Matches the typical production deployment that needs reboot/upgrade
	// access but not CA rotation or offline secret generation.
	ProfileStandard = "standard"

	// ProfileExpert sets READ_ONLY=false, ALLOW_CLUSTER_WIDE=true, ENABLE_GEN=true,
	// ENABLE_INSECURE=true. All gated tool categories are registered, and
	// maintenance-mode operations are unlocked subject to the required
	// TALOS_MCP_INSECURE_ALLOWED_NODES allowlist (enforced in main.go).
	ProfileExpert = "expert"
)

// LoadSafetyProfile returns the effective profile, or an error for an unrecognised profile or override value.
func LoadSafetyProfile() (*SafetyProfile, error) {
	p := &SafetyProfile{}

	raw, set := os.LookupEnv("TALOS_MCP_SAFETY_PROFILE")
	profile := strings.ToLower(strings.TrimSpace(raw))
	switch {
	case !set:
		p.Profile = ProfileNone
	case profile == ProfileConservative:
		p.Profile = ProfileConservative
		p.ReadOnly = true
	case profile == ProfileStandard:
		p.Profile = ProfileStandard
	case profile == ProfileExpert:
		p.Profile = ProfileExpert
		p.AllowClusterWide = true
		p.EnableGen = true
		p.EnableInsecure = true
	default:
		return nil, fmt.Errorf("invalid TALOS_MCP_SAFETY_PROFILE %q: must be one of %s, %s, %s",
			profile, ProfileConservative, ProfileStandard, ProfileExpert)
	}

	flags := []struct {
		envKey string
		target *bool
		label  string
	}{
		{"TALOS_MCP_READ_ONLY", &p.ReadOnly, "read_only"},
		{"TALOS_MCP_ALLOW_CLUSTER_WIDE", &p.AllowClusterWide, "allow_cluster_wide"},
		{"TALOS_MCP_ENABLE_GEN", &p.EnableGen, "enable_gen"},
		{"TALOS_MCP_SKIP_VERSION_CHECK", &p.SkipVersionCheck, "skip_version_check"},
		{"TALOS_MCP_ENABLE_INSECURE", &p.EnableInsecure, "enable_insecure"},
	}
	for _, f := range flags {
		if err := applyOverride(f.envKey, f.target, f.label, &p.Overrides); err != nil {
			return nil, err
		}
	}

	return p, nil
}

func applyOverride(envKey string, target *bool, label string, overrides *[]string) error {
	raw, ok := os.LookupEnv(envKey)
	if !ok {
		return nil
	}
	v, err := strconv.ParseBool(raw)
	if err != nil {
		return fmt.Errorf("invalid %s %q: %w", envKey, raw, err)
	}
	*target = v
	*overrides = append(*overrides, fmt.Sprintf("%s=%t", label, v))
	return nil
}

// LogFields returns structured slog fields describing the effective profile.
// Callers use `slog.Info("safety profile", profile.LogFields()...)`.
func (p *SafetyProfile) LogFields() []any {
	return []any{
		"profile", p.Profile,
		"read_only", p.ReadOnly,
		"allow_cluster_wide", p.AllowClusterWide,
		"enable_gen", p.EnableGen,
		"skip_version_check", p.SkipVersionCheck,
		"enable_insecure", p.EnableInsecure,
		"overrides", strings.Join(p.Overrides, ","),
	}
}
