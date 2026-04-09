package tools

import (
	"context"
	"strings"
	"testing"
)

// TestHandleLogs_EmptyServiceName verifies empty service name is rejected.
func TestHandleLogs_EmptyServiceName(t *testing.T) {
	h := safeH()
	ctx := context.Background()

	_, _, err := h.HandleLogs(ctx, nil, LogsArgs{})
	if err == nil {
		t.Fatal("expected error for empty service_name, got nil")
	}
	if !strings.Contains(err.Error(), "service_name is required") {
		t.Errorf("unexpected error: %v", err)
	}
}
