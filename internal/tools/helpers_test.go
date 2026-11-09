package tools

import (
	"bytes"
	"context"
	"log/slog"
	"reflect"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Nosmoht/talos-mcp-server/internal/talos"
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

// insecureTestEndpoint is the only entry of the allowlist insecureHandlers installs.
const insecureTestEndpoint = "192.0.2.5"

// insecureHandlers returns handlers with maintenance mode enabled for
// insecureTestEndpoint and dialing through d.
func insecureHandlers(t *testing.T, d *recordingDialer) *Handlers {
	t.Helper()

	allowlist, err := talos.ParseNodeAllowlist(insecureTestEndpoint)
	if err != nil {
		t.Fatalf("setup: parse allowlist: %v", err)
	}

	h := safeH()
	h.EnableInsecure = true
	h.InsecureAllowedNodes = allowlist
	h.dialMaintenance = d.dial
	return h
}

// captureDefaultSlog routes slog.Default into the returned buffer until the
// test ends. Tests that call it must not call t.Parallel.
func captureDefaultSlog(t *testing.T) *bytes.Buffer {
	t.Helper()

	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

// auditOutcomeOf returns the outcome field of the AUDIT_OUTCOME record for tool
// in log, or "" when there is none.
func auditOutcomeOf(log, tool string) string {
	for _, line := range strings.Split(log, "\n") {
		if !strings.Contains(line, "msg=AUDIT_OUTCOME") || !strings.Contains(line, "tool="+tool+" ") {
			continue
		}
		for _, field := range strings.Fields(line) {
			if v, ok := strings.CutPrefix(field, "outcome="); ok {
				return v
			}
		}
	}
	return ""
}

// TestMaintenanceDialerDefault: without an injected dialer the insecure paths dial
// through dialTalosMaintenance (.claude/rules/insecure-mode.md §Transport posture);
// an injected dialer replaces it.
func TestMaintenanceDialerDefault(t *testing.T) {
	t.Run("nil dialer selects dialTalosMaintenance", func(t *testing.T) {
		h := safeH()
		got := reflect.ValueOf(h.maintenanceDialer()).Pointer()
		want := reflect.ValueOf(dialTalosMaintenance).Pointer()
		if got != want {
			t.Errorf("insecure-mode.md §Transport posture: default dialer must be dialTalosMaintenance (%#x), got %#x", want, got)
		}
	})

	t.Run("injected dialer is used", func(t *testing.T) {
		d := &recordingDialer{client: &fakeMaintenanceClient{}}
		h := safeH()
		h.dialMaintenance = d.dial

		if _, err := h.maintenanceDialer()(context.Background(), insecureTestEndpoint, nil); err != nil {
			t.Fatalf("injected dialer: unexpected error %v", err)
		}
		if d.calls != 1 {
			t.Errorf("injected dialer must replace the default: want one recorded dial, got %d", d.calls)
		}
	})
}
