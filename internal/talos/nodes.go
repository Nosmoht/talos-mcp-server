package talos

import (
	"fmt"
	"net"
	"sort"
	"strings"
)

// NodeAllowlist is a pre-parsed allowlist of permitted node IPs, hostnames,
// and CIDR ranges. A nil *NodeAllowlist permits all nodes.
type NodeAllowlist struct {
	cidrs []*net.IPNet
	exact map[string]struct{} // lowercased canonical strings
}

// ParseNodeAllowlist parses a comma-separated list of node IPs, hostnames,
// and CIDR ranges. An empty or blank string returns (nil, nil), meaning all
// nodes are permitted. Returns an error if any CIDR entry is malformed.
func ParseNodeAllowlist(csv string) (*NodeAllowlist, error) {
	csv = strings.TrimSpace(csv)
	if csv == "" {
		return nil, nil
	}

	a := &NodeAllowlist{
		exact: make(map[string]struct{}),
	}

	for _, entry := range strings.Split(csv, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}

		if strings.Contains(entry, "/") {
			_, cidr, err := net.ParseCIDR(entry)
			if err != nil {
				return nil, fmt.Errorf("invalid CIDR entry %q: %w", entry, err)
			}
			a.cidrs = append(a.cidrs, cidr)
		} else {
			// Normalize IPs to canonical form; hostnames are lowercased.
			if ip := net.ParseIP(entry); ip != nil {
				a.exact[ip.String()] = struct{}{}
			} else {
				a.exact[strings.ToLower(entry)] = struct{}{}
			}
		}
	}

	return a, nil
}

// CheckNode returns nil if the node is permitted by the allowlist, or an error
// if it is not. A nil *NodeAllowlist permits all nodes.
func (a *NodeAllowlist) CheckNode(node string) error {
	if a == nil {
		return nil
	}

	// Normalize the input node to its canonical form.
	normalized := node
	if ip := net.ParseIP(node); ip != nil {
		normalized = ip.String()
		// Check CIDR ranges.
		for _, cidr := range a.cidrs {
			if cidr.Contains(ip) {
				return nil
			}
		}
	}

	// Check exact match (normalized IP or lowercased hostname).
	key := strings.ToLower(normalized)
	if _, ok := a.exact[key]; ok {
		return nil
	}

	return fmt.Errorf("node %q is not in the allowed nodes list (TALOS_MCP_ALLOWED_NODES)", node)
}

// Len returns the total number of allowlist entries (exact matches + CIDR ranges).
// Returns 0 for a nil *NodeAllowlist.
func (a *NodeAllowlist) Len() int {
	if a == nil {
		return 0
	}
	return len(a.exact) + len(a.cidrs)
}

// Exact returns a sorted copy of the exact-match allowlist entries (normalized
// IPs and lowercased hostnames). CIDR ranges are omitted — they cannot be
// enumerated as discrete completion candidates. Returns nil when the allowlist
// is disabled.
func (a *NodeAllowlist) Exact() []string {
	if a == nil {
		return nil
	}
	out := make([]string, 0, len(a.exact))
	for k := range a.exact {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// CheckNodes returns the first error from CheckNode across all nodes, or nil
// if all nodes are permitted. A nil *NodeAllowlist permits all nodes.
func (a *NodeAllowlist) CheckNodes(nodes []string) error {
	if a == nil {
		return nil
	}
	for _, node := range nodes {
		if err := a.CheckNode(node); err != nil {
			return err
		}
	}
	return nil
}
