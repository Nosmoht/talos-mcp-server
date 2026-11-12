package tools

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"go.yaml.in/yaml/v4"
)

func checkBlockedPaths(patchDoc []byte, blocked []string) error {
	if len(blocked) == 0 {
		return nil
	}

	paths, err := extractPatchPaths(patchDoc)
	if err != nil {
		return fmt.Errorf("inspect patch paths: %w", err)
	}

	for _, p := range paths {
		if p == "" {
			return fmt.Errorf("patch replaces the whole config document (RFC 6902 path \"/\" or a top-level $patch), which would overwrite blocked config path %q", blocked[0])
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

func extractPatchPaths(patchDoc []byte) ([]string, error) {
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

	// configpatcher.LoadPatch loads every document, so each is inspected and typed ones are refused.
	dec := yaml.NewDecoder(bytes.NewReader(patchDoc))
	var paths []string
	for i := 1; ; i++ {
		var m map[string]any
		err := dec.Decode(&m)
		if errors.Is(err, io.EOF) {
			return paths, nil
		}
		if err != nil {
			return nil, fmt.Errorf("parse patch as YAML/JSON: %w", err)
		}
		_, hasAPIVersion := m["apiVersion"]
		_, hasKind := m["kind"]
		if hasAPIVersion || hasKind {
			return nil, fmt.Errorf("patch document %d carries apiVersion/kind: typed config documents are refused while TALOS_MCP_BLOCKED_CONFIG_PATHS is set", i)
		}
		collectPaths("", m, &paths)
	}
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

// collectPaths records only leaf paths, because an intermediate node would conflict with every blocked path below it.
func collectPaths(prefix string, m map[string]any, paths *[]string) {
	for k, v := range m {
		// machinery applies $patch to the mapping that holds it (delete removes that whole node), so the parent path is recorded.
		if k == "$patch" {
			*paths = append(*paths, prefix)
			continue
		}

		full := k
		if prefix != "" {
			full = prefix + "." + k
		}

		if child, ok := v.(map[string]any); ok && len(child) > 0 {
			collectPaths(full, child, paths)
		} else {
			// An empty map replaces the subtree, so it counts as a leaf.
			*paths = append(*paths, full)
		}
	}
}
