package tools

import (
	"encoding/json"
	"fmt"
	"strings"

	"go.yaml.in/yaml/v4"
)

// checkBlockedPaths returns an error when the patch touches any path listed in
// blocked. It handles both RFC 6902 JSON Patch arrays and strategic merge
// patches (YAML or JSON maps). An empty blocked slice is a no-op.
func checkBlockedPaths(patchDoc []byte, blocked []string) error {
	if len(blocked) == 0 {
		return nil
	}

	paths, err := extractPatchPaths(patchDoc)
	if err != nil {
		return fmt.Errorf("inspect patch paths: %w", err)
	}

	for _, p := range paths {
		// An empty path is an RFC 6902 root-document pointer ("/").
		// A root replacement would overwrite every config key, including all
		// blocked paths, so reject it whenever any blocklist is active.
		if p == "" {
			return fmt.Errorf("patch contains a root-document operation (RFC 6902 path \"/\") which would overwrite all blocked paths")
		}

		for _, b := range blocked {
			if pathConflicts(p, b) {
				return fmt.Errorf("patch targets blocked config path %q (blocked: %q)", p, b)
			}
		}
	}

	return nil
}

// pathConflicts reports whether patch path p conflicts with blocked path b.
// Conflict means p equals b, p is a descendant of b, or p is an ancestor of b.
func pathConflicts(p, b string) bool {
	return p == b ||
		strings.HasPrefix(p, b+".") ||
		strings.HasPrefix(b, p+".")
}

// extractPatchPaths returns all dot-separated field paths touched by a patch
// document. Supports RFC 6902 JSON Patch arrays (extracts the "path" field of
// each operation, normalised to dot notation) and strategic merge patches
// (YAML or JSON maps, flattened to dot-paths).
func extractPatchPaths(patchDoc []byte) ([]string, error) {
	// Try RFC 6902: JSON array of operation objects.
	var ops []struct {
		Op   string `json:"op"`
		Path string `json:"path"`
		From string `json:"from"` // present for "move" and "copy" ops (RFC 6902 §4.3, §4.4)
	}
	if json.Unmarshal(patchDoc, &ops) == nil && len(ops) > 0 {
		out := make([]string, 0, len(ops)*2)
		for _, op := range ops {
			out = append(out, normaliseJSONPointer(op.Path))
			// "move" and "copy" also read from the "from" path; check it too so that
			// an operation like {"op":"move","from":"/machine/security","path":"/x"}
			// does not silently bypass the blocklist.
			if (op.Op == "move" || op.Op == "copy") && op.From != "" {
				out = append(out, normaliseJSONPointer(op.From))
			}
		}
		return out, nil
	}

	// Strategic merge patch: YAML or JSON map.
	var m map[string]any
	if err := yaml.Unmarshal(patchDoc, &m); err != nil {
		return nil, fmt.Errorf("parse patch as YAML/JSON: %w", err)
	}

	var paths []string
	collectPaths("", m, &paths)

	return paths, nil
}

// normaliseJSONPointer converts an RFC 6902 JSON pointer (e.g. "/machine/network/hostname")
// to a dot-separated path (e.g. "machine.network.hostname"). Leading slashes are stripped.
// RFC 6902 escape sequences (~0 → ~, ~1 → /) are decoded.
func normaliseJSONPointer(ptr string) string {
	ptr = strings.TrimPrefix(ptr, "/")
	parts := strings.Split(ptr, "/")
	for i, p := range parts {
		p = strings.ReplaceAll(p, "~1", "/")
		p = strings.ReplaceAll(p, "~0", "~")
		parts[i] = p
	}

	return strings.Join(parts, ".")
}

// collectPaths recursively walks a decoded YAML map and appends only leaf
// dot-paths to paths. Intermediate container nodes are not added; only the
// deepest keys (non-map values, or empty maps) are recorded. This prevents
// false positives when a patch touches an unrelated sibling subtree whose
// ancestor happens to share a prefix with a blocked path.
func collectPaths(prefix string, m map[string]any, paths *[]string) {
	for k, v := range m {
		full := k
		if prefix != "" {
			full = prefix + "." + k
		}

		if child, ok := v.(map[string]any); ok && len(child) > 0 {
			// Non-empty map — recurse without recording this intermediate node.
			collectPaths(full, child, paths)
		} else {
			// Leaf: scalar, array, or empty map (empty map effectively replaces
			// the subtree, so it must be checked against blocked paths).
			*paths = append(*paths, full)
		}
	}
}
