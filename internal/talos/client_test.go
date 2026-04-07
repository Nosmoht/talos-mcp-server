package talos

import (
	"context"
	"testing"

	"google.golang.org/grpc/metadata"
)

// TestWithNodes_Empty verifies that an empty nodes slice does not modify the context.
func TestWithNodes_Empty(t *testing.T) {
	ctx := context.Background()
	if got := WithNodes(ctx, nil); got != ctx {
		t.Error("WithNodes(nil) should return the original context unchanged")
	}
	if got := WithNodes(ctx, []string{}); got != ctx {
		t.Error("WithNodes([]) should return the original context unchanged")
	}
}

// TestWithNodes_Single verifies that a single node uses the one-to-one "node" metadata key
// (required for COSI State methods that reject one-to-many fan-out proxying).
func TestWithNodes_Single(t *testing.T) {
	ctx := context.Background()
	got := WithNodes(ctx, []string{"192.168.2.61"})

	md, ok := metadata.FromOutgoingContext(got)
	if !ok {
		t.Fatal("expected outgoing metadata in context")
	}
	nodeVals := md.Get("node")
	if len(nodeVals) != 1 || nodeVals[0] != "192.168.2.61" {
		t.Fatalf("expected singular 'node' key = 192.168.2.61, got %v", nodeVals)
	}
	// Confirm plural "nodes" key is NOT set (would trigger one-to-many fan-out).
	if vals := md.Get("nodes"); len(vals) > 0 {
		t.Fatalf("expected no plural 'nodes' key for single node, got %v", vals)
	}
}

// TestWithNodes_Multiple verifies that multiple nodes use the fan-out "nodes" metadata key.
func TestWithNodes_Multiple(t *testing.T) {
	ctx := context.Background()
	got := WithNodes(ctx, []string{"192.168.2.61", "192.168.2.62"})

	md, ok := metadata.FromOutgoingContext(got)
	if !ok {
		t.Fatal("expected outgoing metadata in context")
	}
	nodesVals := md.Get("nodes")
	if len(nodesVals) != 2 {
		t.Fatalf("expected plural 'nodes' key with 2 entries, got %v", nodesVals)
	}
	if vals := md.Get("node"); len(vals) > 0 {
		t.Fatalf("expected no singular 'node' key for multiple nodes, got %v", vals)
	}
}
