package config

import (
	"errors"
	"fmt"
	"net"
	"strconv"
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
			continue // bare IP / hostname — not subject to CIDR ceiling
		}

		_, cidr, err := net.ParseCIDR(entry)
		if err != nil {
			// Defer the parse error to ParseNodeAllowlist; here we only
			// care about the permissiveness ceiling.
			continue
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

// ParseMetaPrivilegedKeys parses TALOS_MCP_META_PRIVILEGED_KEYS as a
// comma-separated list of META keys (uint8). Accepts decimal (`6`), hex
// (`0x06`, `0X06`), and explicit octal (`0o13`). REJECTS leading-zero
// decimal-looking input (`013`) because Go's strconv.ParseUint with base 0
// interprets it as octal, which surprises operators who type META values
// in decimal.
//
// Empty entries (`",,5"`, leading/trailing commas, whitespace-only) are
// silently skipped. Duplicates collapse via the map. On any parse error
// or out-of-range value (> 255), returns an error naming the offending entry.
//
// Empty input (whitespace-only) returns an empty map and nil error — that
// is the safe default.
func ParseMetaPrivilegedKeys(raw string) (map[uint8]struct{}, error) {
	out := make(map[uint8]struct{})
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return out, nil
	}

	for _, entry := range strings.Split(raw, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}

		// Reject leading-zero decimal-looking input that strconv would parse as octal.
		// Allow 0x..., 0X..., 0o..., 0b... explicitly; allow bare "0" itself.
		if len(entry) > 1 && entry[0] == '0' {
			second := entry[1]
			if second != 'x' && second != 'X' && second != 'o' && second != 'O' && second != 'b' && second != 'B' {
				return nil, fmt.Errorf("TALOS_MCP_META_PRIVILEGED_KEYS entry %q has ambiguous leading zero: use explicit 0x prefix for hex or 0o for octal", entry)
			}
		}

		// bit-size 9 lets us detect overflow > 255 explicitly (uint8's range is 0..255).
		v, err := strconv.ParseUint(entry, 0, 9)
		if err != nil {
			return nil, fmt.Errorf("TALOS_MCP_META_PRIVILEGED_KEYS entry %q: %w", entry, err)
		}
		if v > 255 {
			return nil, fmt.Errorf("TALOS_MCP_META_PRIVILEGED_KEYS entry %q: value %d exceeds META key range 0-255", entry, v)
		}

		out[uint8(v)] = struct{}{}
	}

	return out, nil
}
