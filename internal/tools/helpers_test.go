package tools

import (
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestJSONResult_ReturnsStructured verifies that jsonResult hands the value
// straight through as the second return (StructuredContent), with no Content
// text wrapping. The go-sdk auto-populates Content with JSON text; the helper
// must NOT pre-populate it.
func TestJSONResult_ReturnsStructured(t *testing.T) {
	type payload struct {
		Answer int `json:"answer"`
	}

	p := payload{Answer: 42}

	res, out, err := jsonResult(p)
	if err != nil {
		t.Fatalf("jsonResult: unexpected error %v", err)
	}
	if res != nil {
		t.Fatalf("jsonResult: expected nil *CallToolResult, got %+v", res)
	}
	got, ok := out.(payload)
	if !ok {
		t.Fatalf("jsonResult: out type = %T, want payload", out)
	}
	if got != p {
		t.Fatalf("jsonResult: out = %+v, want %+v", got, p)
	}
}

// TestJSONWithTextResult_DualContent verifies the dual-content path used by
// the 4 text-heavy tools: Content carries human-readable prose, StructuredContent
// carries the typed DTO. Both must ride on the wire.
func TestJSONWithTextResult_DualContent(t *testing.T) {
	type payload struct {
		Lines []string `json:"lines"`
	}

	p := payload{Lines: []string{"a", "b"}}
	text := "a\nb"

	res, out, err := jsonWithTextResult(p, text)
	if err != nil {
		t.Fatalf("jsonWithTextResult: unexpected error %v", err)
	}
	if res == nil {
		t.Fatal("jsonWithTextResult: expected non-nil *CallToolResult")
	}
	if len(res.Content) != 1 {
		t.Fatalf("jsonWithTextResult: Content length = %d, want 1", len(res.Content))
	}
	tc, ok := res.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("jsonWithTextResult: Content[0] type = %T, want *mcp.TextContent", res.Content[0])
	}
	if tc.Text != text {
		t.Fatalf("jsonWithTextResult: Content[0].Text = %q, want %q", tc.Text, text)
	}
	got, ok := out.(payload)
	if !ok {
		t.Fatalf("jsonWithTextResult: out type = %T, want payload", out)
	}
	if len(got.Lines) != 2 || got.Lines[0] != "a" || got.Lines[1] != "b" {
		t.Fatalf("jsonWithTextResult: out = %+v, want %+v", got, p)
	}
}
