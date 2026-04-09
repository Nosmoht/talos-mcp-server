package tools

import (
	"testing"
)

func TestPathConflicts(t *testing.T) {
	tests := []struct {
		p, b string
		want bool
	}{
		// Exact match.
		{"machine.security", "machine.security", true},
		// p is a child of b.
		{"machine.security.ca", "machine.security", true},
		{"machine.security.ca.crt", "machine.security", true},
		// b is a child of p (ancestor block).
		{"machine", "machine.security.ca", true},
		{"machine.security", "machine.security.ca", true},
		// No conflict — different subtrees.
		{"machine.network", "machine.security", false},
		{"cluster.etcd", "machine.security", false},
		// Prefix collision guard (e.g. "machine.sec" must not block "machine.security").
		{"machine.sec", "machine.security", false},
		{"machine.securityExtra", "machine.security", false},
	}

	for _, tc := range tests {
		got := pathConflicts(tc.p, tc.b)
		if got != tc.want {
			t.Errorf("pathConflicts(%q, %q) = %v, want %v", tc.p, tc.b, got, tc.want)
		}
	}
}

func TestNormaliseJSONPointer(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"/machine/network/hostname", "machine.network.hostname"},
		{"/machine", "machine"},
		{"", ""},
		// RFC 6902 escape sequences.
		{"/foo~1bar", "foo/bar"},
		{"/foo~0bar", "foo~bar"},
	}

	for _, tc := range tests {
		got := normaliseJSONPointer(tc.input)
		if got != tc.want {
			t.Errorf("normaliseJSONPointer(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestExtractPatchPaths_RFC6902(t *testing.T) {
	patch := `[
		{"op": "replace", "path": "/machine/network/hostname", "value": "node1"},
		{"op": "add",     "path": "/machine/certSANs/-",       "value": "192.168.1.1"}
	]`

	paths, err := extractPatchPaths([]byte(patch))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := map[string]bool{
		"machine.network.hostname": true,
		"machine.certSANs.-":       true,
	}

	if len(paths) != len(want) {
		t.Fatalf("got %d paths, want %d: %v", len(paths), len(want), paths)
	}

	for _, p := range paths {
		if !want[p] {
			t.Errorf("unexpected path %q", p)
		}
	}
}

func TestExtractPatchPaths_StrategicMerge(t *testing.T) {
	patch := `
machine:
  network:
    hostname: node1
  certSANs:
    - 192.168.1.1
`

	paths, err := extractPatchPaths([]byte(patch))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Only leaf paths are collected; intermediate container nodes are skipped.
	found := make(map[string]bool, len(paths))
	for _, p := range paths {
		found[p] = true
	}

	// machine.network.hostname and machine.certSANs are leaves.
	// machine and machine.network are intermediate containers — not expected.
	expected := []string{"machine.network.hostname", "machine.certSANs"}
	for _, e := range expected {
		if !found[e] {
			t.Errorf("expected path %q not found in %v", e, paths)
		}
	}

	notExpected := []string{"machine", "machine.network"}
	for _, e := range notExpected {
		if found[e] {
			t.Errorf("intermediate path %q should not be in %v", e, paths)
		}
	}
}

func TestCheckBlockedPaths_NoBlocklist(t *testing.T) {
	patch := `{"machine": {"network": {"hostname": "node1"}}}`
	if err := checkBlockedPaths([]byte(patch), nil); err != nil {
		t.Errorf("expected no error with empty blocklist, got: %v", err)
	}
}

func TestCheckBlockedPaths_Blocked(t *testing.T) {
	patch := `{"machine": {"security": {"ca": {"crt": "CERT"}}}}`
	blocked := []string{"machine.security"}

	err := checkBlockedPaths([]byte(patch), blocked)
	if err == nil {
		t.Fatal("expected error for blocked path, got nil")
	}
}

func TestCheckBlockedPaths_Allowed(t *testing.T) {
	patch := `{"machine": {"network": {"hostname": "node1"}}}`
	blocked := []string{"machine.security", "cluster.etcd"}

	if err := checkBlockedPaths([]byte(patch), blocked); err != nil {
		t.Errorf("expected no error for non-blocked path, got: %v", err)
	}
}

func TestCheckBlockedPaths_RFC6902Blocked(t *testing.T) {
	patch := `[{"op": "replace", "path": "/machine/security/ca/crt", "value": "CERT"}]`
	blocked := []string{"machine.security"}

	err := checkBlockedPaths([]byte(patch), blocked)
	if err == nil {
		t.Fatal("expected error for blocked RFC 6902 path, got nil")
	}
}

func TestCheckBlockedPaths_RFC6902Allowed(t *testing.T) {
	patch := `[{"op": "replace", "path": "/machine/network/hostname", "value": "node1"}]`
	blocked := []string{"machine.security"}

	if err := checkBlockedPaths([]byte(patch), blocked); err != nil {
		t.Errorf("expected no error for non-blocked RFC 6902 path, got: %v", err)
	}
}

func TestCheckBlockedPaths_RFC6902RootPointerBlocked(t *testing.T) {
	// An RFC 6902 op with path "/" (root document pointer) must be rejected
	// when any blocklist is active — it would overwrite all config keys.
	patch := `[{"op": "replace", "path": "/", "value": {}}]`
	blocked := []string{"machine.security"}

	err := checkBlockedPaths([]byte(patch), blocked)
	if err == nil {
		t.Fatal("expected error for root-document RFC 6902 operation, got nil")
	}
}

func TestCheckBlockedPaths_RFC6902RootPointerNoBlocklist(t *testing.T) {
	// Root pointer with no blocklist: no restriction.
	patch := `[{"op": "replace", "path": "/", "value": {}}]`
	if err := checkBlockedPaths([]byte(patch), nil); err != nil {
		t.Errorf("expected no error with empty blocklist, got: %v", err)
	}
}

func TestCheckBlockedPaths_RFC6902MoveFromBlockedPath(t *testing.T) {
	// "move" reads the source via "from"; moving content out of a blocked path
	// should be rejected even though the destination ("path") is not blocked.
	patch := `[{"op": "move", "from": "/machine/security", "path": "/machine/network"}]`
	blocked := []string{"machine.security"}

	err := checkBlockedPaths([]byte(patch), blocked)
	if err == nil {
		t.Fatal("expected error: move from blocked path should be rejected, got nil")
	}
}

func TestCheckBlockedPaths_RFC6902CopyFromBlockedPath(t *testing.T) {
	// "copy" reads from the "from" source; copying a blocked path must be rejected.
	patch := `[{"op": "copy", "from": "/machine/security/ca", "path": "/machine/network/ca"}]`
	blocked := []string{"machine.security"}

	err := checkBlockedPaths([]byte(patch), blocked)
	if err == nil {
		t.Fatal("expected error: copy from blocked path should be rejected, got nil")
	}
}

func TestCheckBlockedPaths_RFC6902MoveToBlockedPath(t *testing.T) {
	// "move" that targets a blocked path must also be rejected (destination is blocked).
	patch := `[{"op": "move", "from": "/machine/network/hostname", "path": "/machine/security/hostname"}]`
	blocked := []string{"machine.security"}

	err := checkBlockedPaths([]byte(patch), blocked)
	if err == nil {
		t.Fatal("expected error: move to blocked path should be rejected, got nil")
	}
}

func TestCheckBlockedPaths_RFC6902MoveUnrelatedPaths(t *testing.T) {
	// "move" between two unblocked paths should be allowed.
	patch := `[{"op": "move", "from": "/machine/network/hostname", "path": "/machine/network/alias"}]`
	blocked := []string{"machine.security"}

	if err := checkBlockedPaths([]byte(patch), blocked); err != nil {
		t.Errorf("expected no error for move between non-blocked paths, got: %v", err)
	}
}

func TestCheckBlockedPaths_EmptyMapClearsBlockedPath(t *testing.T) {
	// An empty map value for a blocked key effectively clears that subtree.
	// It must be treated as a leaf and rejected.
	patch := `{"machine": {"security": {}}}`
	blocked := []string{"machine.security"}

	err := checkBlockedPaths([]byte(patch), blocked)
	if err == nil {
		t.Fatal("expected error: empty-map clear of blocked path should be rejected, got nil")
	}
}
