package config

import (
	"errors"
	"fmt"
	"net"
	"strings"
)

// CIDR thresholds for the insecure-mode allowlist permissiveness ceiling.
// These are deliberately tight: maintenance-mode operations bypass mTLS, so
// the allowlist is the only network-layer defence. Anything broader than
// /16 IPv4 or /48 IPv6 is refused at startup; anything broader than /24 IPv4
// or /64 IPv6 logs a warning.
const (
	insecureIPv4HardRefuseMask = 16
	insecureIPv6HardRefuseMask = 48
	insecureIPv4WarnMask       = 24
	insecureIPv6WarnMask       = 64
)

// InsecureAllowlistInspection summarises a parsed comma-separated allowlist
// for the maintenance-mode endpoint. broadEntries is populated with CIDR
// strings that pass the hard-refuse threshold but exceed the warn threshold
// — main.go emits a slog.Warn for each entry returned.
type InsecureAllowlistInspection struct {
	BroadEntries []string
}

// CheckInsecureAllowlist returns an error when raw is empty/blank OR contains
// any CIDR broader than /16 IPv4 (/48 IPv6) OR `0.0.0.0/0` / `::/0`. These
// permissiveness ceilings exist because the insecure transport has no other
// authentication; an over-broad allowlist would let an LLM under prompt
// injection dial arbitrary IPs.
//
// Returns the list of "broad but accepted" CIDRs (broader than /24 IPv4 or
// /64 IPv6 but within the hard-refuse threshold) so the caller can log a
// startup warning for each.
//
// Non-CIDR entries (bare IPs, hostnames) are not subject to the ceiling —
// they each match exactly one node.
func CheckInsecureAllowlist(raw string) (*InsecureAllowlistInspection, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, errors.New("TALOS_MCP_INSECURE_ALLOWED_NODES must be set when TALOS_MCP_ENABLE_INSECURE=true (the unauthenticated transport requires an explicit IP allowlist)")
	}

	out := &InsecureAllowlistInspection{}
	hasEntry := false

	for _, entry := range strings.Split(raw, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		hasEntry = true

		if !strings.Contains(entry, "/") {
			// Bare entry — must be an IP. Hostnames are accepted by
			// NodeAllowlist's string match but never reachable on the
			// insecure path because CanonicalIP only emits IPs. Fail loudly
			// so an operator who pastes a hostname gets a clear error
			// rather than a silent "endpoint not allowlisted" later.
			if net.ParseIP(entry) == nil {
				return nil, fmt.Errorf("TALOS_MCP_INSECURE_ALLOWED_NODES entry %q is not a bare IP or CIDR — hostnames are not honoured here because CanonicalIP only emits IPs", entry)
			}
			continue
		}

		_, cidr, err := net.ParseCIDR(entry)
		if err != nil {
			return nil, fmt.Errorf("TALOS_MCP_INSECURE_ALLOWED_NODES entry %q: invalid CIDR: %w", entry, err)
		}

		ones, bits := cidr.Mask.Size()
		isIPv4 := bits == 32

		hard := insecureIPv4HardRefuseMask
		warn := insecureIPv4WarnMask
		if !isIPv4 {
			hard = insecureIPv6HardRefuseMask
			warn = insecureIPv6WarnMask
		}

		if ones < hard {
			return nil, fmt.Errorf("TALOS_MCP_INSECURE_ALLOWED_NODES entry %q is too permissive: /%d exceeds the hard refusal threshold /%d for maintenance-mode allowlists", entry, ones, hard)
		}
		if ones < warn {
			out.BroadEntries = append(out.BroadEntries, entry)
		}
	}

	if !hasEntry {
		return nil, errors.New("TALOS_MCP_INSECURE_ALLOWED_NODES contains no usable entries (only whitespace/empty tokens)")
	}

	return out, nil
}
