package version_test

import (
	"testing"

	"github.com/Nosmoht/talos-mcp-server/internal/version"
)

func TestParse(t *testing.T) {
	tests := []struct {
		name    string
		tag     string
		want    version.TalosVersion
		wantErr bool
	}{
		{name: "standard", tag: "v1.12.6", want: version.TalosVersion{Major: 1, Minor: 12, Patch: 6}},
		{name: "pre-release", tag: "v1.12.6-dirty", want: version.TalosVersion{Major: 1, Minor: 12, Patch: 6}},
		{name: "pre-release complex", tag: "v1.9.0-alpha.1", want: version.TalosVersion{Major: 1, Minor: 9, Patch: 0}},
		{name: "zero patch", tag: "v1.5.0", want: version.TalosVersion{Major: 1, Minor: 5, Patch: 0}},
		{name: "missing v prefix", tag: "1.2.3", wantErr: true},
		{name: "only two parts", tag: "v1.2", wantErr: true},
		{name: "non-numeric", tag: "vfoo.bar.baz", wantErr: true},
		{name: "empty", tag: "", wantErr: true},
		{name: "latest", tag: "latest", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := version.Parse(tt.tag)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("Parse(%q) = %v, want error", tt.tag, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("Parse(%q) error = %v", tt.tag, err)
			}
			if got != tt.want {
				t.Errorf("Parse(%q) = %v, want %v", tt.tag, got, tt.want)
			}
		})
	}
}

func TestExtractFromImage(t *testing.T) {
	tests := []struct {
		name    string
		image   string
		want    version.TalosVersion
		wantErr bool
	}{
		{
			name:  "standard image",
			image: "ghcr.io/siderolabs/installer:v1.12.6",
			want:  version.TalosVersion{Major: 1, Minor: 12, Patch: 6},
		},
		{
			name:  "factory image",
			image: "factory.talos.dev/installer/abc123:v1.9.0",
			want:  version.TalosVersion{Major: 1, Minor: 9, Patch: 0},
		},
		{
			name:  "pre-release tag",
			image: "ghcr.io/siderolabs/installer:v1.12.6-dirty",
			want:  version.TalosVersion{Major: 1, Minor: 12, Patch: 6},
		},
		{
			name:    "latest tag",
			image:   "ghcr.io/siderolabs/installer:latest",
			wantErr: true,
		},
		{
			name:    "no tag",
			image:   "ghcr.io/siderolabs/installer",
			wantErr: true,
		},
		{
			name:    "empty string",
			image:   "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := version.ExtractFromImage(tt.image)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ExtractFromImage(%q) = %v, want error", tt.image, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ExtractFromImage(%q) error = %v", tt.image, err)
			}
			if got != tt.want {
				t.Errorf("ExtractFromImage(%q) = %v, want %v", tt.image, got, tt.want)
			}
		})
	}
}

func TestValidateUpgradePath(t *testing.T) {
	v := func(major, minor, patch int) version.TalosVersion {
		return version.TalosVersion{Major: major, Minor: minor, Patch: patch}
	}

	tests := []struct {
		name    string
		current version.TalosVersion
		target  version.TalosVersion
		wantErr bool
	}{
		{name: "patch upgrade", current: v(1, 12, 0), target: v(1, 12, 6), wantErr: false},
		{name: "minor upgrade +1", current: v(1, 11, 5), target: v(1, 12, 0), wantErr: false},
		{name: "minor upgrade skip +2", current: v(1, 10, 0), target: v(1, 12, 0), wantErr: true},
		{name: "minor upgrade skip +3", current: v(1, 9, 0), target: v(1, 12, 0), wantErr: true},
		{name: "downgrade patch", current: v(1, 12, 6), target: v(1, 12, 0), wantErr: true},
		{name: "downgrade minor", current: v(1, 12, 0), target: v(1, 11, 9), wantErr: true},
		{name: "same version", current: v(1, 12, 6), target: v(1, 12, 6), wantErr: true},
		{name: "cross-major upgrade", current: v(1, 12, 6), target: v(2, 0, 0), wantErr: true},
		{name: "cross-major downgrade", current: v(2, 0, 0), target: v(1, 12, 6), wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := version.ValidateUpgradePath(tt.current, tt.target)
			if tt.wantErr && err == nil {
				t.Fatalf("ValidateUpgradePath(%s→%s) = nil, want error", tt.current, tt.target)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("ValidateUpgradePath(%s→%s) error = %v", tt.current, tt.target, err)
			}
		})
	}
}

func TestInSupportedRange(t *testing.T) {
	v := func(major, minor, patch int) version.TalosVersion {
		return version.TalosVersion{Major: major, Minor: minor, Patch: patch}
	}

	tests := []struct {
		name string
		v    version.TalosVersion
		want bool
	}{
		{name: "below min", v: v(1, 8, 9), want: false},
		{name: "at min", v: v(1, 9, 0), want: true},
		{name: "in range", v: v(1, 11, 3), want: true},
		{name: "in range upper", v: v(1, 12, 6), want: true},
		{name: "at max patch 0", v: v(1, 13, 0), want: true},
		{name: "at max patch 255", v: v(1, 13, 255), want: true},
		{name: "above max minor", v: v(1, 14, 0), want: false},
		{name: "above max major", v: v(2, 0, 0), want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.v.InSupportedRange()
			if got != tt.want {
				t.Errorf("%s.InSupportedRange() = %v, want %v", tt.v, got, tt.want)
			}
		})
	}
}

func TestString(t *testing.T) {
	v := version.TalosVersion{Major: 1, Minor: 12, Patch: 6}
	if got := v.String(); got != "v1.12.6" {
		t.Errorf("String() = %q, want %q", got, "v1.12.6")
	}
}
