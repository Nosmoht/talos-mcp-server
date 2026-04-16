package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestInvariant_ReadOnlyGatesDestructiveTools enforces Invariant #1 from the
// talos-mcp coverage plan: every tool carrying DestructiveHint=true must be
// registered only when readOnly=false. The test starts two servers, one in
// each mode, lists their tools via an in-memory client, and asserts no
// destructive tool survives the readOnly-true run.
func TestInvariant_ReadOnlyGatesDestructiveTools(t *testing.T) {
	rwTools := listToolsWithMode(t, false)
	roTools := listToolsWithMode(t, true)

	var leaked []string
	for name, tool := range roTools {
		if isDestructive(tool) {
			leaked = append(leaked, name)
		}
	}
	if len(leaked) > 0 {
		t.Fatalf("destructive tools leaked through readOnly=true gate: %v", leaked)
	}

	// Sanity check: read/write mode must expose at least one destructive tool,
	// otherwise the test is trivially passing and doesn't actually catch gaps.
	destructiveCount := 0
	for _, tool := range rwTools {
		if isDestructive(tool) {
			destructiveCount++
		}
	}
	if destructiveCount == 0 {
		t.Fatal("no destructive tool present in readOnly=false registration; invariant test would never catch a regression")
	}
}

// knownConfirmGapTools is the waiver registry for destructive tools that
// legitimately cannot carry a `confirm` property in their InputSchema yet.
// The map is currently empty — every destructive tool advertises confirm.
//
// To add a waiver: insert `"talos_<name>": "<reason + tracking issue>"`. The
// invariants test will then `t.Logf` the gap on every CI run instead of failing.
// File a tracking GitHub issue per
// .claude/skills/add-mcp-tool/references/safety-defaults.md before adding
// the entry — the t.Logf line is not a tracker.
//
// Issue #156 was the canonical use of this mechanism: talos_service_action
// shipped without confirm, was waived here, tracked as #156, and retrofitted
// — at which point the entry was removed.
var knownConfirmGapTools = map[string]string{}

// TestInvariant_DestructiveToolsRequireConfirm enforces Invariant #2 (partial):
// every destructive tool's InputSchema must expose a `confirm` property so
// handlers can reject calls that do not explicitly acknowledge the mutation.
//
// risk_acknowledgements enforcement is deferred until the Phase B retrofit
// lands (no current tool carries the field yet).
func TestInvariant_DestructiveToolsRequireConfirm(t *testing.T) {
	tools := listToolsWithMode(t, false)

	for name, tool := range tools {
		if !isDestructive(tool) {
			continue
		}
		t.Run(name, func(t *testing.T) {
			if schemaHasProperty(t, tool.InputSchema, "confirm") {
				if _, waived := knownConfirmGapTools[name]; waived {
					t.Errorf("tool %q is on the knownConfirmGapTools waiver but now carries a confirm property — remove the waiver entry", name)
				}
				return
			}
			if reason, waived := knownConfirmGapTools[name]; waived {
				t.Logf("tool %q missing confirm — waived: %s", name, reason)
				return
			}
			t.Errorf("tool %q has DestructiveHint=true but no `confirm` property in input schema", name)
		})
	}
}

// listToolsWithMode boots a server with the given readOnly flag, connects an
// in-memory client, and returns the ListTools result indexed by tool name.
func listToolsWithMode(t *testing.T, readOnly bool) map[string]*mcp.Tool {
	t.Helper()

	server := mcp.NewServer(&mcp.Implementation{Name: "invariants-test", Version: "0.0.0"}, nil)
	// mockClient's methods panic on gRPC use; listing tools never invokes them.
	Register(server, &Handlers{Client: &mockClient{}}, readOnly)

	serverTransport, clientTransport := mcp.NewInMemoryTransports()

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	// Server.Run blocks for the lifetime of the transport; run it in a goroutine.
	go func() {
		_ = server.Run(ctx, serverTransport)
	}()

	client := mcp.NewClient(&mcp.Implementation{Name: "invariants-client", Version: "0.0.0"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client.Connect: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })

	result, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}

	byName := make(map[string]*mcp.Tool, len(result.Tools))
	for _, tool := range result.Tools {
		byName[tool.Name] = tool
	}
	return byName
}

// isDestructive returns true iff the tool carries DestructiveHint=true.
func isDestructive(tool *mcp.Tool) bool {
	if tool == nil || tool.Annotations == nil || tool.Annotations.DestructiveHint == nil {
		return false
	}
	return *tool.Annotations.DestructiveHint
}

// schemaHasProperty returns true when the JSON schema (as received over the
// wire, i.e. any) declares a property with the given name. It tolerates both
// map[string]any (the default client-side representation) and json.RawMessage.
func schemaHasProperty(t *testing.T, schema any, property string) bool {
	t.Helper()
	if schema == nil {
		return false
	}

	var schemaMap map[string]any
	switch s := schema.(type) {
	case map[string]any:
		schemaMap = s
	case json.RawMessage:
		if err := json.Unmarshal(s, &schemaMap); err != nil {
			t.Fatalf("unmarshal schema: %v", err)
		}
	default:
		// Fallback: serialise → parse. Works for SDK types that marshal to JSON.
		raw, err := json.Marshal(s)
		if err != nil {
			t.Fatalf("marshal schema (%T): %v", s, err)
		}
		if err := json.Unmarshal(raw, &schemaMap); err != nil {
			t.Fatalf("unmarshal schema (%T): %v", s, err)
		}
	}

	props, ok := schemaMap["properties"].(map[string]any)
	if !ok {
		return false
	}
	// Property names in existing Args structs are lower-camelCase via json
	// tags but equality check is literal. Accept both "confirm" and any
	// case-insensitive variant to survive future renames.
	lower := strings.ToLower(property)
	for name := range props {
		if strings.ToLower(name) == lower {
			return true
		}
	}
	return false
}
