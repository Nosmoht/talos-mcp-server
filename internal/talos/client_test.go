package talos

import (
	"context"
	"strings"
	"testing"

	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	talosclient "github.com/siderolabs/talos/pkg/machinery/client"
	"google.golang.org/grpc/metadata"
)

// TestRebootOptions_ModeReachesRequest: every machinery reboot mode arrives
// unchanged in the RebootRequest the client sends; a mode outside the
// machinery enum is refused instead of being sent.
func TestRebootOptions_ModeReachesRequest(t *testing.T) {
	t.Parallel()

	var outOfSet machineapi.RebootRequest_Mode
	for _, v := range machineapi.RebootRequest_Mode_value {
		outOfSet = max(outOfSet, machineapi.RebootRequest_Mode(v)+1)
	}

	tests := []struct {
		name    string
		mode    machineapi.RebootRequest_Mode
		wantErr bool
	}{
		{name: "default", mode: machineapi.RebootRequest_DEFAULT},
		{name: "powercycle", mode: machineapi.RebootRequest_POWERCYCLE},
		{name: "force", mode: machineapi.RebootRequest_FORCE},
		{name: "out-of-set refused", mode: outOfSet, wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			opts, err := rebootOptions(tc.mode)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("machineapi.RebootRequest_Mode enum: mode %d is outside the enum, want error got nil", tc.mode)
				}
				return
			}
			if err != nil {
				t.Fatalf("machineapi.RebootRequest_Mode enum: mode %v is in the enum, want nil error got %v", tc.mode, err)
			}

			var req machineapi.RebootRequest
			for _, opt := range opts {
				opt(&req)
			}
			if req.GetMode() != tc.mode {
				t.Errorf("machineapi.RebootRequest.Mode: want %v got %v", tc.mode, req.GetMode())
			}
		})
	}
}

// TestUpgradeOptions_RequestReachesOptions: every field of the UpgradeRequest
// arrives unchanged in the machinery UpgradeOptions; a reboot mode outside the
// machinery enum is refused instead of being sent.
func TestUpgradeOptions_RequestReachesOptions(t *testing.T) {
	t.Parallel()

	type upgradeFields struct {
		Image      string
		Preserve   bool
		Stage      bool
		Force      bool
		RebootMode machineapi.UpgradeRequest_RebootMode
	}

	var outOfSet machineapi.UpgradeRequest_RebootMode
	for _, v := range machineapi.UpgradeRequest_RebootMode_value {
		outOfSet = max(outOfSet, machineapi.UpgradeRequest_RebootMode(v)+1)
	}

	tests := []struct {
		name    string
		req     *machineapi.UpgradeRequest
		wantErr bool
	}{
		{
			name: "default reboot mode with preserve",
			req: &machineapi.UpgradeRequest{
				Image:      "ghcr.io/siderolabs/installer:v1.13.0",
				Preserve:   true,
				RebootMode: machineapi.UpgradeRequest_DEFAULT,
			},
		},
		{
			name: "powercycle with stage and force",
			req: &machineapi.UpgradeRequest{
				Image:      "ghcr.io/siderolabs/installer:v1.13.1",
				Stage:      true,
				Force:      true,
				RebootMode: machineapi.UpgradeRequest_POWERCYCLE,
			},
		},
		{
			name:    "out-of-set reboot mode refused",
			req:     &machineapi.UpgradeRequest{Image: "ghcr.io/siderolabs/installer:v1.13.0", RebootMode: outOfSet},
			wantErr: true,
		},
		{
			name:    "nil request refused",
			req:     nil,
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			opts, err := upgradeOptions(tc.req)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("machineapi.UpgradeRequest_RebootMode enum: request %v must be refused, got nil error", tc.req)
				}
				return
			}
			if err != nil {
				t.Fatalf("machineapi.UpgradeRequest_RebootMode enum: request %v is valid, want nil error got %v", tc.req, err)
			}

			var got talosclient.UpgradeOptions
			for _, opt := range opts {
				opt(&got)
			}
			want := upgradeFields{tc.req.Image, tc.req.Preserve, tc.req.Stage, tc.req.Force, tc.req.RebootMode}
			have := upgradeFields{got.Request.Image, got.Request.Preserve, got.Request.Stage, got.Request.Force, got.Request.RebootMode}
			if have != want {
				t.Errorf("talosclient.UpgradeOptions.Request: want %+v got %+v", want, have)
			}
		})
	}
}

// TestEventsOptions_TailReachesRequest: the tail value arrives unchanged in the
// EventsRequest, including a negative tail (machinery: all past events).
func TestEventsOptions_TailReachesRequest(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		tail int32
	}{
		{name: "positive tail", tail: 50},
		{name: "negative tail means all", tail: -1},
		{name: "zero tail", tail: 0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var req machineapi.EventsRequest
			for _, opt := range eventsOptions(tc.tail) {
				opt(&req)
			}
			if req.GetTailEvents() != tc.tail {
				t.Errorf("machineapi.EventsRequest.TailEvents: want %d got %d", tc.tail, req.GetTailEvents())
			}
		})
	}
}

// TestWithSingleNode_Empty verifies that an empty nodes slice does not modify the context.
func TestWithSingleNode_Empty(t *testing.T) {
	ctx := context.Background()
	got, err := WithSingleNode(ctx, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != ctx {
		t.Error("WithSingleNode(nil) should return the original context unchanged")
	}
	got, err = WithSingleNode(ctx, []string{}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != ctx {
		t.Error("WithSingleNode([]) should return the original context unchanged")
	}
}

// TestWithSingleNode_Single verifies that a single node uses the one-to-one "node" metadata key
// (required for COSI State methods that reject one-to-many fan-out proxying).
func TestWithSingleNode_Single(t *testing.T) {
	ctx := context.Background()
	got, err := WithSingleNode(ctx, []string{"192.0.2.61"}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	md, ok := metadata.FromOutgoingContext(got)
	if !ok {
		t.Fatal("expected outgoing metadata in context")
	}
	nodeVals := md.Get("node")
	if len(nodeVals) != 1 || nodeVals[0] != "192.0.2.61" {
		t.Fatalf("expected singular 'node' key = 192.0.2.61, got %v", nodeVals)
	}
	// Confirm plural "nodes" key is NOT set (would trigger one-to-many fan-out).
	if vals := md.Get("nodes"); len(vals) > 0 {
		t.Fatalf("expected no plural 'nodes' key for single node, got %v", vals)
	}
}

// TestWithSingleNode_Multiple verifies that more than one node is an error and
// leaves the context without node routing.
func TestWithSingleNode_Multiple(t *testing.T) {
	ctx := context.Background()
	got, err := WithSingleNode(ctx, []string{"192.0.2.61", "192.0.2.62"}, nil)
	if err == nil {
		t.Fatal("expected an error for more than one node, got nil")
	}
	if !strings.Contains(err.Error(), "exactly one node") {
		t.Errorf("error = %q, want it to state that exactly one node is required", err)
	}

	md, _ := metadata.FromOutgoingContext(got)
	if len(md.Get("node")) > 0 || len(md.Get("nodes")) > 0 {
		t.Errorf("expected no node routing on error, got node=%v nodes=%v", md.Get("node"), md.Get("nodes"))
	}
}

// TestWithSingleNode_AllowlistRejection verifies that a node not in the allowlist is rejected.
func TestWithSingleNode_AllowlistRejection(t *testing.T) {
	allow, err := ParseNodeAllowlist("10.0.0.1")
	if err != nil {
		t.Fatalf("ParseNodeAllowlist: %v", err)
	}
	ctx := context.Background()
	_, err = WithSingleNode(ctx, []string{"192.0.2.1"}, allow)
	if err == nil {
		t.Error("expected error for node not in allowlist, got nil")
	}
}
