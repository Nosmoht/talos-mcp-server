package completions

import (
	"context"
	"reflect"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Nosmoht/talos-mcp-server/internal/talos"
)

func mustAllowlist(t *testing.T, csv string) *talos.NodeAllowlist {
	t.Helper()
	a, err := talos.ParseNodeAllowlist(csv)
	if err != nil {
		t.Fatalf("ParseNodeAllowlist(%q): %v", csv, err)
	}
	return a
}

func callHandle(
	t *testing.T,
	allow *talos.NodeAllowlist,
	ref *mcp.CompleteReference,
	argName, partial string,
	ctxArgs map[string]string,
) []string {
	t.Helper()
	h := NewHandler(allow)
	var cctx *mcp.CompleteContext
	if ctxArgs != nil {
		cctx = &mcp.CompleteContext{Arguments: ctxArgs}
	}
	res, err := h(context.Background(), &mcp.CompleteRequest{
		Params: &mcp.CompleteParams{
			Ref:      ref,
			Argument: mcp.CompleteParamsArgument{Name: argName, Value: partial},
			Context:  cctx,
		},
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if res == nil {
		t.Fatalf("handler returned nil result")
	}
	return res.Completion.Values
}

func contains(haystack []string, needle string) bool {
	for _, h := range haystack {
		if h == needle {
			return true
		}
	}
	return false
}

func TestPromptCompletion(t *testing.T) {
	allow := mustAllowlist(t, "192.0.2.11,192.0.2.10")

	tests := []struct {
		name    string
		prompt  string
		arg     string
		partial string
		want    []string
	}{
		{
			name:   "diagnose-node node returns allowlist",
			prompt: "diagnose-node",
			arg:    "node",
			want:   []string{"192.0.2.10", "192.0.2.11"},
		},
		{
			name:   "investigate-etcd node returns allowlist",
			prompt: "investigate-etcd",
			arg:    "node",
			want:   []string{"192.0.2.10", "192.0.2.11"},
		},
		{
			name:   "pre-upgrade-checklist nodes returns allowlist",
			prompt: "pre-upgrade-checklist",
			arg:    "nodes",
			want:   []string{"192.0.2.10", "192.0.2.11"},
		},
		{
			name:    "debug-service node with partial filters allowlist",
			prompt:  "debug-service",
			arg:     "node",
			partial: "192.0.2.1",
			want:    []string{"192.0.2.10", "192.0.2.11"},
		},
		{
			name:   "apply-config mode returns exact modes",
			prompt: "apply-config",
			arg:    "mode",
			want:   []string{"auto", "no_reboot", "reboot", "staged", "try"},
		},
		{
			name:    "unknown prompt returns empty",
			prompt:  "not-a-prompt",
			arg:     "node",
			partial: "",
			want:    []string{},
		},
		{
			name:    "known prompt unknown arg returns empty",
			prompt:  "diagnose-node",
			arg:     "not-a-field",
			partial: "",
			want:    []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := callHandle(t, allow, &mcp.CompleteReference{Type: "ref/prompt", Name: tt.prompt}, tt.arg, tt.partial, nil)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("values = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestPromptServiceCompletion(t *testing.T) {
	allow := mustAllowlist(t, "")
	got := callHandle(t, allow, &mcp.CompleteReference{Type: "ref/prompt", Name: "debug-service"}, "service", "", nil)
	if !contains(got, "kubelet") || !contains(got, "etcd") {
		t.Errorf("expected kubelet and etcd in %#v", got)
	}
	// sorted deterministic check
	want := []string{"apid", "containerd", "cri", "etcd", "kubelet", "machined", "trustd", "udevd"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("service list = %#v, want %#v", got, want)
	}
}

func TestPromptServiceCaseInsensitivePrefix(t *testing.T) {
	allow := mustAllowlist(t, "")
	got := callHandle(t, allow, &mcp.CompleteReference{Type: "ref/prompt", Name: "debug-service"}, "service", "KU", nil)
	if !reflect.DeepEqual(got, []string{"kubelet"}) {
		t.Errorf("case-insensitive prefix filter returned %#v, want [kubelet]", got)
	}
}

func TestResourceCompletion(t *testing.T) {
	allow := mustAllowlist(t, "192.0.2.10,192.0.2.11")
	const talosTpl = "talos://{node}/resource/{namespace}/{type}"

	t.Run("namespace returns common namespaces", func(t *testing.T) {
		got := callHandle(t, allow, &mcp.CompleteReference{Type: "ref/resource", URI: talosTpl}, "namespace", "", nil)
		if !contains(got, "runtime") {
			t.Errorf("expected runtime in %#v", got)
		}
	})

	t.Run("node with partial filters allowlist", func(t *testing.T) {
		got := callHandle(t, allow, &mcp.CompleteReference{Type: "ref/resource", URI: talosTpl}, "node", "192.0.2", nil)
		want := []string{"192.0.2.10", "192.0.2.11"}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("node values = %#v, want %#v", got, want)
		}
	})

	t.Run("type deferred returns empty", func(t *testing.T) {
		got := callHandle(t, allow, &mcp.CompleteReference{Type: "ref/resource", URI: talosTpl}, "type", "", nil)
		if len(got) != 0 {
			t.Errorf("expected empty values for type arg, got %#v", got)
		}
	})

	t.Run("non-talos URI returns empty", func(t *testing.T) {
		got := callHandle(t, allow, &mcp.CompleteReference{Type: "ref/resource", URI: "file:///etc/hosts"}, "namespace", "", nil)
		if len(got) != 0 {
			t.Errorf("expected empty values for non-talos URI, got %#v", got)
		}
	})
}

func TestUnknownRefType(t *testing.T) {
	// Bypass SDK unmarshal validation by constructing the ref directly.
	got := callHandle(t, nil, &mcp.CompleteReference{Type: "ref/tool", Name: "whatever"}, "arg", "", nil)
	if len(got) != 0 {
		t.Errorf("expected empty for unknown ref type, got %#v", got)
	}
}

func TestNilAllowlistNoPanic(t *testing.T) {
	got := callHandle(t, nil, &mcp.CompleteReference{Type: "ref/prompt", Name: "diagnose-node"}, "node", "", nil)
	if len(got) != 0 {
		t.Errorf("expected empty values for nil allowlist, got %#v", got)
	}
}

func TestCIDROnlyAllowlistReturnsEmpty(t *testing.T) {
	allow := mustAllowlist(t, "192.0.2.0/24")
	got := callHandle(t, allow, &mcp.CompleteReference{Type: "ref/prompt", Name: "diagnose-node"}, "node", "", nil)
	if len(got) != 0 {
		t.Errorf("expected empty values for CIDR-only allowlist, got %#v", got)
	}
}

func TestContextArgumentsIgnoredButAccepted(t *testing.T) {
	// Regression guard: the handler must accept requests with Context.Arguments
	// populated (MCP clients send this for later template variables) and
	// continue to return candidates without consulting it. When dynamic
	// discovery is added, this test will need to be updated.
	allow := mustAllowlist(t, "192.0.2.10")
	ctxArgs := map[string]string{"node": "192.0.2.99"} // unrelated to result
	got := callHandle(t, allow, &mcp.CompleteReference{Type: "ref/resource", URI: "talos://{node}/resource/{namespace}/{type}"}, "namespace", "run", ctxArgs)
	if !contains(got, "runtime") {
		t.Errorf("expected runtime in %#v despite Context.Arguments", got)
	}
}

func TestNilRefReturnsEmpty(t *testing.T) {
	h := NewHandler(nil)
	res, err := h(context.Background(), &mcp.CompleteRequest{
		Params: &mcp.CompleteParams{
			Ref:      nil,
			Argument: mcp.CompleteParamsArgument{Name: "x", Value: ""},
		},
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if res == nil || len(res.Completion.Values) != 0 {
		t.Errorf("expected empty result for nil ref, got %#v", res)
	}
}
