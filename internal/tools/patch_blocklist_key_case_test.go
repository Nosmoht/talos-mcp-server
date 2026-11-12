package tools

import "testing"

// RFC 6902 §4 names the members "op", "path" and "from"; JSON member names are case-sensitive (RFC 8259 §7),
// so machinery's JSON-patch decoder ignores "PATH" and "OP" while the blocklist must not read them either.
func TestGivenRFC6902OperationReadDifferentlyByMachineryWhenItTouchesBlockedPathThenItIsRefused(t *testing.T) {
	blocked := []string{"cluster.etcd"}

	cases := []struct {
		name  string
		patch string
	}{
		{"second path key in other case", `[{"op":"remove","path":"/cluster/etcd","PATH":"/machine/nodeLabels"}]`},
		{"second op key in other case hides a move", `[{"op":"move","OP":"add","from":"/cluster/etcd","path":"/machine/nodeLabels"}]`},
		{"path key only in other case", `[{"op":"remove","PATH":"/cluster/etcd"}]`},
		// RFC 6901 §3: a non-empty JSON pointer starts with "/"; machinery's decoder drops the first segment unchecked.
		{"path without leading slash", `[{"op":"remove","path":"x/cluster/etcd"}]`},
		{"fragment-style path", `[{"op":"remove","path":"#/cluster/etcd"}]`},
		{"move from without leading slash", `[{"op":"move","from":"x/cluster/etcd","path":"/machine/nodeLabels"}]`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := checkBlockedPaths([]byte(tc.patch), blocked); err == nil {
				t.Errorf("RFC 6902 §4: patch %s against blocked %v: expected refusal got nil", tc.patch, blocked)
			}
		})
	}
}
