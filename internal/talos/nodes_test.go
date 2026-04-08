package talos

import (
	"testing"
)

func TestParseNodeAllowlist(t *testing.T) {
	t.Run("empty string returns nil", func(t *testing.T) {
		a, err := ParseNodeAllowlist("")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if a != nil {
			t.Fatal("expected nil allowlist for empty string")
		}
	})

	t.Run("blank string returns nil", func(t *testing.T) {
		a, err := ParseNodeAllowlist("   ")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if a != nil {
			t.Fatal("expected nil allowlist for blank string")
		}
	})

	t.Run("malformed CIDR returns error", func(t *testing.T) {
		_, err := ParseNodeAllowlist("10.0.0.0/99")
		if err == nil {
			t.Fatal("expected error for malformed CIDR")
		}
	})

	t.Run("whitespace around entries is trimmed", func(t *testing.T) {
		a, err := ParseNodeAllowlist("  10.0.0.1 ,  10.0.0.2  ")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if err := a.CheckNode("10.0.0.1"); err != nil {
			t.Errorf("expected 10.0.0.1 to be allowed: %v", err)
		}
	})

	t.Run("empty entries between commas are skipped", func(t *testing.T) {
		a, err := ParseNodeAllowlist("10.0.0.1,,10.0.0.2")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if err := a.CheckNode("10.0.0.1"); err != nil {
			t.Errorf("expected 10.0.0.1 to be allowed: %v", err)
		}
	})
}

func TestNodeAllowlist_CheckNode(t *testing.T) {
	tests := []struct {
		name      string
		allowlist string
		node      string
		wantAllow bool
	}{
		// Nil allowlist
		{
			name:      "nil allowlist allows everything",
			allowlist: "",
			node:      "10.0.0.1",
			wantAllow: true,
		},
		// Exact IP matches
		{
			name:      "exact IP allowed",
			allowlist: "10.0.0.1",
			node:      "10.0.0.1",
			wantAllow: true,
		},
		{
			name:      "non-listed IP denied",
			allowlist: "10.0.0.1",
			node:      "10.0.0.2",
			wantAllow: false,
		},
		// Hostname matches
		{
			name:      "exact hostname allowed",
			allowlist: "node1.example.com",
			node:      "node1.example.com",
			wantAllow: true,
		},
		{
			name:      "hostname match is case-insensitive",
			allowlist: "Node1.Example.COM",
			node:      "node1.example.com",
			wantAllow: true,
		},
		{
			name:      "different hostname denied",
			allowlist: "node1.example.com",
			node:      "node2.example.com",
			wantAllow: false,
		},
		// CIDR matches
		{
			name:      "IP within CIDR allowed",
			allowlist: "10.0.0.0/24",
			node:      "10.0.0.5",
			wantAllow: true,
		},
		{
			name:      "IP outside CIDR denied",
			allowlist: "10.0.0.0/24",
			node:      "192.168.1.1",
			wantAllow: false,
		},
		{
			name:      "CIDR boundary: first address",
			allowlist: "10.0.0.0/24",
			node:      "10.0.0.0",
			wantAllow: true,
		},
		{
			name:      "CIDR boundary: last address",
			allowlist: "10.0.0.0/24",
			node:      "10.0.0.255",
			wantAllow: true,
		},
		// Mixed exact + CIDR
		{
			name:      "mixed: IP matches CIDR",
			allowlist: "10.0.0.1,192.168.0.0/16",
			node:      "192.168.5.10",
			wantAllow: true,
		},
		{
			name:      "mixed: IP matches exact",
			allowlist: "10.0.0.1,192.168.0.0/16",
			node:      "10.0.0.1",
			wantAllow: true,
		},
		{
			name:      "mixed: IP matches neither",
			allowlist: "10.0.0.1,192.168.0.0/16",
			node:      "172.16.0.1",
			wantAllow: false,
		},
		// Hostname with CIDR-only allowlist
		{
			name:      "hostname with CIDR-only allowlist is denied",
			allowlist: "10.0.0.0/24",
			node:      "node1.example.com",
			wantAllow: false,
		},
		// IPv6
		{
			name:      "IPv6 exact match",
			allowlist: "2001:db8::1",
			node:      "2001:db8::1",
			wantAllow: true,
		},
		{
			name:      "IPv6 CIDR match",
			allowlist: "2001:db8::/32",
			node:      "2001:db8::1",
			wantAllow: true,
		},
		{
			name:      "IPv6 CIDR no match",
			allowlist: "2001:db8::/32",
			node:      "2001:db9::1",
			wantAllow: false,
		},
		// IPv4-mapped IPv6 equivalence
		{
			name:      "IPv4-mapped IPv6 matches IPv4 allowlist",
			allowlist: "10.0.0.1",
			node:      "::ffff:10.0.0.1",
			wantAllow: true,
		},
		{
			name:      "IPv4 matches IPv4-mapped IPv6 allowlist",
			allowlist: "::ffff:10.0.0.1",
			node:      "10.0.0.1",
			wantAllow: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a, err := ParseNodeAllowlist(tt.allowlist)
			if err != nil {
				t.Fatalf("ParseNodeAllowlist(%q) error: %v", tt.allowlist, err)
			}

			err = a.CheckNode(tt.node)
			if tt.wantAllow && err != nil {
				t.Errorf("expected node %q to be allowed, got error: %v", tt.node, err)
			}
			if !tt.wantAllow && err == nil {
				t.Errorf("expected node %q to be denied, but was allowed", tt.node)
			}
		})
	}
}

func TestNodeAllowlist_CheckNodes(t *testing.T) {
	t.Run("nil allowlist allows all nodes", func(t *testing.T) {
		var a *NodeAllowlist
		if err := a.CheckNodes([]string{"10.0.0.1", "192.168.1.1"}); err != nil {
			t.Errorf("expected nil allowlist to allow all: %v", err)
		}
	})

	t.Run("empty node list passes", func(t *testing.T) {
		a, _ := ParseNodeAllowlist("10.0.0.1")
		if err := a.CheckNodes(nil); err != nil {
			t.Errorf("unexpected error for empty node list: %v", err)
		}
	})

	t.Run("first denied node returns error", func(t *testing.T) {
		a, _ := ParseNodeAllowlist("10.0.0.1")
		err := a.CheckNodes([]string{"10.0.0.1", "192.168.1.1"})
		if err == nil {
			t.Error("expected error for denied node 192.168.1.1")
		}
	})

	t.Run("all allowed nodes pass", func(t *testing.T) {
		a, _ := ParseNodeAllowlist("10.0.0.0/24")
		if err := a.CheckNodes([]string{"10.0.0.1", "10.0.0.2"}); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})
}
