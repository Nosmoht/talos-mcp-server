package talos

import (
	"reflect"
	"testing"
)

func TestNodeAllowlist_Exact(t *testing.T) {
	tests := []struct {
		name      string
		allowlist string
		want      []string
	}{
		{
			name:      "nil allowlist returns nil",
			allowlist: "",
			want:      nil,
		},
		{
			name:      "CIDR-only allowlist returns empty non-nil",
			allowlist: "192.0.2.0/24",
			want:      []string{},
		},
		{
			name:      "IPs only returns sorted list",
			allowlist: "192.0.2.11,192.0.2.10",
			want:      []string{"192.0.2.10", "192.0.2.11"},
		},
		{
			name:      "IPs and hostnames sorted together",
			allowlist: "node2.example.com,192.0.2.10,node1.example.com",
			want:      []string{"192.0.2.10", "node1.example.com", "node2.example.com"},
		},
		{
			name:      "mixed with CIDR excludes CIDR",
			allowlist: "192.0.2.10,198.51.100.0/24,node1.example.com",
			want:      []string{"192.0.2.10", "node1.example.com"},
		},
		{
			name:      "hostname lowercased in output",
			allowlist: "Node1.Example.COM",
			want:      []string{"node1.example.com"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a, err := ParseNodeAllowlist(tt.allowlist)
			if err != nil {
				t.Fatalf("ParseNodeAllowlist(%q) error: %v", tt.allowlist, err)
			}

			got := a.Exact()
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Exact() = %#v, want %#v", got, tt.want)
			}
		})
	}
}
