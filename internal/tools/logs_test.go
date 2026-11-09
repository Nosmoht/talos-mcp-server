package tools

import (
	"context"
	"math"
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

// TestHandleEvents_TailReachesClient: talos_events tail_count (EventsArgs.TailCount
// schema: default defaultEventsTailCount, -1 for all) reaches the client.
func TestHandleEvents_TailReachesClient(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		tailCount int32
		want      int32
	}{
		{name: "omitted uses default", tailCount: 0, want: defaultEventsTailCount},
		{name: "explicit count", tailCount: defaultEventsTailCount + 1, want: defaultEventsTailCount + 1},
		{name: "all events", tailCount: -1, want: -1},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			m := &mockClient{recordEvents: true, gotEventsTail: math.MinInt32}
			h := safeH()
			h.Client = m

			if _, _, err := h.HandleEvents(context.Background(), nil, EventsArgs{TailCount: tc.tailCount}); err != nil {
				t.Fatalf("talos_events tail_count=%d: unexpected error %v", tc.tailCount, err)
			}
			if m.gotEventsTail != tc.want {
				t.Errorf("EventsArgs.TailCount schema: tail_count=%d want %d got %d", tc.tailCount, tc.want, m.gotEventsTail)
			}
		})
	}
}
