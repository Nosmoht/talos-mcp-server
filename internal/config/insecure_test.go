package config

import (
	"strings"
	"testing"
)

func TestCheckInsecureAllowlist(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		input     string
		wantErr   string // substring; empty means accept
		wantBroad []string
	}{
		{name: "empty", input: "", wantErr: "must be set"},
		{name: "whitespace", input: "  \t\n", wantErr: "must be set"},
		{name: "only commas", input: ",,,", wantErr: "no usable entries"},
		{name: "bare ip accepted", input: "192.0.2.5"},
		{name: "ipv4 /24 accepted no warn", input: "192.0.2.0/24"},
		{name: "ipv4 /28 accepted no warn", input: "192.0.2.0/28"},
		{name: "ipv4 /23 broad warn", input: "192.0.2.0/23", wantBroad: []string{"192.0.2.0/23"}},
		{name: "ipv4 /16 broad warn", input: "10.0.0.0/16", wantBroad: []string{"10.0.0.0/16"}},
		{name: "ipv4 /15 refused", input: "10.0.0.0/15", wantErr: "too permissive"},
		{name: "ipv4 /8 refused", input: "10.0.0.0/8", wantErr: "too permissive"},
		{name: "ipv4 /0 refused", input: "0.0.0.0/0", wantErr: "too permissive"},
		{name: "ipv6 /64 accepted no warn", input: "2001:db8::/64"},
		{name: "ipv6 /48 broad warn", input: "2001:db8::/48", wantBroad: []string{"2001:db8::/48"}},
		{name: "ipv6 /47 refused", input: "2001:db8::/47", wantErr: "too permissive"},
		{name: "ipv6 /0 refused", input: "::/0", wantErr: "too permissive"},
		{name: "mixed entries", input: "192.0.2.5, 192.0.2.0/24, 2001:db8::/64"},
		{name: "first entry refused before broad", input: "192.0.2.0/8, 192.0.2.0/24", wantErr: "too permissive"},
		{name: "hostname rejected", input: "node-1.lan", wantErr: "not a bare IP or CIDR"},
		{name: "malformed cidr rejected", input: "192.0.2.0/notanumber", wantErr: "invalid CIDR"},
		{name: "garbage entry rejected", input: "garbage", wantErr: "not a bare IP or CIDR"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := CheckInsecureAllowlist(tc.input)
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tc.wantErr)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error %q does not contain %q", err.Error(), tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !slicesEqualUnordered(got.BroadEntries, tc.wantBroad) {
				t.Fatalf("broad entries: got %v, want %v", got.BroadEntries, tc.wantBroad)
			}
		})
	}
}

func slicesEqualUnordered(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	ma := make(map[string]int, len(a))
	for _, v := range a {
		ma[v]++
	}
	for _, v := range b {
		ma[v]--
		if ma[v] < 0 {
			return false
		}
	}
	return true
}
