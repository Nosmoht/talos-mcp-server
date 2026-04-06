package tools

import (
	"bytes"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"testing"
)

func TestAuditLog_NilLogger(_ *testing.T) {
	h := safeH()
	// Must not panic with no logger set
	h.auditLog("talos_reboot", struct{}{}, []string{"10.0.0.1"})
}

func TestMcpLogError_NilLogger(_ *testing.T) {
	h := safeH()
	// Must not panic with no logger set
	h.mcpLogError("talos_upgrade", fmt.Errorf("connection refused"))
}

func TestSetLogger_StoresLogger(t *testing.T) {
	h := safeH()
	l := slog.New(slog.NewTextHandler(io.Discard, nil))
	h.SetLogger(l)
	if h.logger.Load() == nil {
		t.Fatal("expected logger to be stored")
	}
}

func TestAuditLog_WithLogger(t *testing.T) {
	h := safeH()
	var buf bytes.Buffer
	h.SetLogger(slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	h.auditLog("talos_reboot", struct{ Confirm bool }{Confirm: true}, []string{"10.0.0.1"})
	out := buf.String()
	if !strings.Contains(out, "tool invoked") {
		t.Errorf("expected 'tool invoked' in log output, got: %s", out)
	}
	if !strings.Contains(out, "talos_reboot") {
		t.Errorf("expected tool name in log output, got: %s", out)
	}
}

func TestMcpLogError_WithLogger(t *testing.T) {
	h := safeH()
	var buf bytes.Buffer
	h.SetLogger(slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	h.mcpLogError("talos_upgrade", fmt.Errorf("upgrade failed"))
	out := buf.String()
	if !strings.Contains(out, "tool error") {
		t.Errorf("expected 'tool error' in log output, got: %s", out)
	}
	if !strings.Contains(out, "talos_upgrade") {
		t.Errorf("expected tool name in log output, got: %s", out)
	}
}
