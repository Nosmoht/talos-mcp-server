package talos

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// ParseMetaKey parses a META key (decimal or 0x/0o/0b-prefixed, 0-255). It refuses a leading 0 without a base prefix, which base 0 reads as octal, and underscores, which base 0 accepts as digit separators.
func ParseMetaKey(s string) (uint8, error) {
	if s == "" {
		return 0, errors.New("key must not be empty")
	}
	if strings.Contains(s, "_") {
		return 0, fmt.Errorf("key %q must not contain underscores", s)
	}
	if len(s) > 1 && s[0] == '0' {
		second := s[1]
		if second != 'x' && second != 'X' && second != 'o' && second != 'O' && second != 'b' && second != 'B' {
			return 0, fmt.Errorf("key %q has ambiguous leading zero: use explicit 0x prefix for hex or 0o for octal", s)
		}
	}
	v, err := strconv.ParseUint(s, 0, 9)
	if err != nil {
		return 0, fmt.Errorf("key %q: %w", s, err)
	}
	if v > 255 {
		return 0, fmt.Errorf("key %q: value %d exceeds META key range 0-255", s, v)
	}
	return uint8(v), nil
}

// ParseMetaPrivilegedKeys parses a comma-separated TALOS_MCP_META_PRIVILEGED_KEYS list into a key set.
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
		v, err := ParseMetaKey(entry)
		if err != nil {
			return nil, fmt.Errorf("TALOS_MCP_META_PRIVILEGED_KEYS entry %q: %w", entry, err)
		}
		out[v] = struct{}{}
	}

	return out, nil
}
