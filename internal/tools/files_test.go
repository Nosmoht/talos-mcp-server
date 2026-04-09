package tools

import (
	"testing"
)

func TestCheckPathAllowed(t *testing.T) {
	cases := []struct {
		name    string
		path    string
		allowed []string
		wantErr bool
	}{
		{
			name:    "empty allowlist permits all paths",
			path:    "/root/secret",
			allowed: nil,
			wantErr: false,
		},
		{
			name:    "exact match allowed",
			path:    "/etc",
			allowed: []string{"/etc"},
			wantErr: false,
		},
		{
			name:    "child path under allowed prefix",
			path:    "/etc/hosts",
			allowed: []string{"/etc"},
			wantErr: false,
		},
		{
			name:    "deep child path under allowed prefix",
			path:    "/etc/ssl/certs/ca.crt",
			allowed: []string{"/etc"},
			wantErr: false,
		},
		{
			name:    "path outside prefix is rejected",
			path:    "/root/secret",
			allowed: []string{"/etc"},
			wantErr: true,
		},
		{
			name:    "prefix-similar name rejected (directory boundary safe)",
			path:    "/etc-evil/passwd",
			allowed: []string{"/etc"},
			wantErr: true,
		},
		{
			name:    "dotdot traversal cleaned and rejected",
			path:    "/etc/../root/secret",
			allowed: []string{"/etc"},
			wantErr: true,
		},
		{
			name:    "dotdot traversal staying inside prefix allowed",
			path:    "/etc/ssl/../hosts",
			allowed: []string{"/etc"},
			wantErr: false,
		},
		{
			name:    "trailing slash on path allowed",
			path:    "/etc/",
			allowed: []string{"/etc"},
			wantErr: false,
		},
		{
			name:    "trailing slash on prefix entry works",
			path:    "/etc/hosts",
			allowed: []string{"/etc/"},
			wantErr: false,
		},
		{
			name:    "second prefix matches when first does not",
			path:    "/proc/1/status",
			allowed: []string{"/etc", "/proc"},
			wantErr: false,
		},
		{
			name:    "no prefix matches returns error",
			path:    "/var/log/syslog",
			allowed: []string{"/etc", "/proc"},
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := checkPathAllowed(tc.path, tc.allowed)
			if (err != nil) != tc.wantErr {
				t.Errorf("checkPathAllowed(%q, %v) error = %v, wantErr %v", tc.path, tc.allowed, err, tc.wantErr)
			}
		})
	}
}

func TestParseAllowedPaths(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  []string
	}{
		{
			name:  "empty returns nil",
			input: "",
			want:  nil,
		},
		{
			name:  "single path",
			input: "/etc",
			want:  []string{"/etc"},
		},
		{
			name:  "multiple paths",
			input: "/etc,/proc,/sys",
			want:  []string{"/etc", "/proc", "/sys"},
		},
		{
			name:  "paths with whitespace trimmed",
			input: " /etc , /proc ",
			want:  []string{"/etc", "/proc"},
		},
		{
			name:  "empty segments skipped",
			input: "/etc,,/proc",
			want:  []string{"/etc", "/proc"},
		},
		{
			name:  "unclean path is canonicalized",
			input: "/etc/../var",
			want:  []string{"/var"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ParseAllowedPaths(tc.input)
			if len(got) != len(tc.want) {
				t.Fatalf("ParseAllowedPaths(%q) = %v, want %v", tc.input, got, tc.want)
			}
			for i := range tc.want {
				if got[i] != tc.want[i] {
					t.Errorf("ParseAllowedPaths(%q)[%d] = %q, want %q", tc.input, i, got[i], tc.want[i])
				}
			}
		})
	}
}
