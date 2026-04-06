// Package version provides Talos Linux version parsing and compatibility validation.
package version

import (
	"fmt"
	"strconv"
	"strings"
)

// MinSupported is the oldest Talos minor version where all 19 gRPC methods
// used by this server (including ResolveResourceKind) are confirmed stable.
var MinSupported = TalosVersion{Major: 1, Minor: 9, Patch: 0}

// MaxTested is the newest Talos minor series validated against the compiled
// machinery SDK. The patch component is set to 255 to match any patch release.
var MaxTested = TalosVersion{Major: 1, Minor: 12, Patch: 255}

// TalosVersion holds a parsed Talos semver tag.
type TalosVersion struct {
	Major int
	Minor int
	Patch int
}

// Parse parses a Talos version tag (e.g. "v1.12.6" or "v1.12.6-dirty").
// The leading "v" is required. Pre-release suffixes after the patch component
// are stripped — only major.minor.patch are retained.
func Parse(tag string) (TalosVersion, error) {
	if !strings.HasPrefix(tag, "v") {
		return TalosVersion{}, fmt.Errorf("version tag %q must start with 'v'", tag)
	}

	// Strip leading "v" and any pre-release suffix (e.g. "-dirty", "-alpha.1").
	core := strings.TrimPrefix(tag, "v")
	if idx := strings.IndexByte(core, '-'); idx >= 0 {
		core = core[:idx]
	}

	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return TalosVersion{}, fmt.Errorf("version tag %q: expected vMAJOR.MINOR.PATCH", tag)
	}

	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return TalosVersion{}, fmt.Errorf("version tag %q: invalid major: %w", tag, err)
	}

	minor, err := strconv.Atoi(parts[1])
	if err != nil {
		return TalosVersion{}, fmt.Errorf("version tag %q: invalid minor: %w", tag, err)
	}

	patch, err := strconv.Atoi(parts[2])
	if err != nil {
		return TalosVersion{}, fmt.Errorf("version tag %q: invalid patch: %w", tag, err)
	}

	return TalosVersion{Major: major, Minor: minor, Patch: patch}, nil
}

// ExtractFromImage extracts the version from a Talos installer image reference
// by splitting on ":" and parsing the tag. Returns an error if the image has
// no tag or the tag is not a parseable semver (e.g. "latest").
//
// Examples:
//
//	ghcr.io/siderolabs/installer:v1.12.6   → {1,12,6}, nil
//	factory.talos.dev/installer/abc:v1.9.0 → {1,9,0}, nil
//	ghcr.io/siderolabs/installer:latest    → error
func ExtractFromImage(image string) (TalosVersion, error) {
	idx := strings.LastIndex(image, ":")
	if idx < 0 || idx == len(image)-1 {
		return TalosVersion{}, fmt.Errorf("image %q has no tag", image)
	}

	tag := image[idx+1:]

	return Parse(tag)
}

// String formats the version as "vMAJOR.MINOR.PATCH".
func (v TalosVersion) String() string {
	return fmt.Sprintf("v%d.%d.%d", v.Major, v.Minor, v.Patch)
}

// Less reports whether v is strictly less than other.
func (v TalosVersion) Less(other TalosVersion) bool {
	if v.Major != other.Major {
		return v.Major < other.Major
	}

	if v.Minor != other.Minor {
		return v.Minor < other.Minor
	}

	return v.Patch < other.Patch
}

// Equal reports whether v equals other.
func (v TalosVersion) Equal(other TalosVersion) bool {
	return v.Major == other.Major && v.Minor == other.Minor && v.Patch == other.Patch
}

// InSupportedRange reports whether v falls within [MinSupported, MaxTested].
func (v TalosVersion) InSupportedRange() bool {
	return !v.Less(MinSupported) && !MaxTested.Less(v)
}

// ValidateUpgradePath checks that upgrading from current to target follows the
// Talos upgrade rules:
//   - Same major version
//   - Target minor is current minor (patch-only upgrade) or current minor+1
//   - Target must be strictly greater than current
//
// Returns a descriptive error when the path is invalid.
func ValidateUpgradePath(current, target TalosVersion) error {
	if target.Major != current.Major {
		return fmt.Errorf("cross-major upgrades are not supported (current %s, target %s)", current, target)
	}

	if target.Less(current) || target.Equal(current) {
		return fmt.Errorf("target version %s is not newer than current version %s", target, current)
	}

	minorDiff := target.Minor - current.Minor
	if minorDiff > 1 {
		return fmt.Errorf(
			"skipping minor versions is not supported: upgrading from %s to %s skips %d minor version(s); upgrade one minor version at a time",
			current, target, minorDiff-1,
		)
	}

	return nil
}
