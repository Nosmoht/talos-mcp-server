package tools

import (
	"errors"
	"strconv"
	"strings"
	"testing"
)

// recordLine returns the first line of log that carries every given key=value
// token as a whole field, or "" when no line does.
func recordLine(log string, tokens ...string) string {
	for _, line := range strings.Split(log, "\n") {
		padded := line + " "
		matched := true
		for _, tok := range tokens {
			if !strings.Contains(padded, " "+tok+" ") {
				matched = false
				break
			}
		}
		if matched {
			return line
		}
	}
	return ""
}

// Predicate: stderr slog is the only audit channel, so auditLog must write an
// INFO record `AUDIT` with the fields tool, nodes and args (README.md § Security,
// Audit logging row; AGENTS.md § Structured logging).
func TestGivenAuditLog_WhenCalled_ThenAuditRecordCarriesToolNodesAndArgs(t *testing.T) {
	cases := []struct {
		name      string
		nodes     []string
		wantNodes string
	}{
		{name: "explicit nodes are comma-joined", nodes: []string{"192.0.2.10", "192.0.2.11"}, wantNodes: "192.0.2.10,192.0.2.11"},
		{name: "no nodes records the talosconfig default marker", nodes: nil, wantNodes: "<default>"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			buf := captureDefaultSlog(t)

			safeH().auditLog("talos_reboot", struct{ Confirm bool }{Confirm: true}, tc.nodes)

			log := buf.String()
			rec := recordLine(log, "level=INFO", "msg=AUDIT")
			if rec == "" {
				t.Fatalf("README.md § Security (Audit logging): expected one level=INFO msg=AUDIT record, got log=%q", log)
			}
			wantFields := []string{
				"tool=talos_reboot",
				"nodes=" + tc.wantNodes,
				"args=" + strconv.Quote(`{"Confirm":true}`),
			}
			for _, field := range wantFields {
				if recordLine(rec, field) == "" {
					t.Errorf("README.md § Security (Audit logging): AUDIT record expected field %s, got record=%q", field, rec)
				}
			}
		})
	}
}

// Predicate: mcpLogError writes an ERROR record `tool error` to the server's
// stderr slog log, carrying the tool and the error (AGENTS.md § Mutating tool
// safety pattern, step 5).
func TestGivenMcpLogError_WhenCalled_ThenErrorLevelRecordCarriesToolAndError(t *testing.T) {
	buf := captureDefaultSlog(t)

	safeH().mcpLogError("talos_upgrade", errors.New("upgrade failed"))

	log := buf.String()
	rec := recordLine(log, "level=ERROR", `msg="tool error"`)
	if rec == "" {
		t.Fatalf("AGENTS.md § Mutating tool safety pattern: expected one level=ERROR msg=\"tool error\" record, got log=%q", log)
	}
	for _, field := range []string{"tool=talos_upgrade", `error="upgrade failed"`} {
		if recordLine(rec, field) == "" {
			t.Errorf("AGENTS.md § Mutating tool safety pattern: ERROR record expected field %s, got record=%q", field, rec)
		}
	}
}

// Predicate: mcpLogWarn writes a WARN record `tool warning` to the server's
// stderr slog log, carrying the tool, the caller's message and the error
// (mcpLogWarn doc comment in helpers.go).
func TestGivenMcpLogWarn_WhenCalled_ThenWarnLevelRecordCarriesToolMessageAndError(t *testing.T) {
	buf := captureDefaultSlog(t)

	safeH().mcpLogWarn("talos_apply_config", "config applied with warnings", errors.New("partial failure"))

	log := buf.String()
	rec := recordLine(log, "level=WARN", `msg="tool warning"`)
	if rec == "" {
		t.Fatalf("helpers.go mcpLogWarn doc: expected one level=WARN msg=\"tool warning\" record, got log=%q", log)
	}
	wantFields := []string{"tool=talos_apply_config", `msg="config applied with warnings"`, `error="partial failure"`}
	for _, field := range wantFields {
		if recordLine(rec, field) == "" {
			t.Errorf("helpers.go mcpLogWarn doc: WARN record expected field %s, got record=%q", field, rec)
		}
	}
}
