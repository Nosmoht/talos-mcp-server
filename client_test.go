package main

import (
	"context"
	"testing"
)

// TestWithNodes_Empty verifies that an empty nodes slice does not modify the context.
func TestWithNodes_Empty(t *testing.T) {
	ctx := context.Background()
	if got := withNodes(ctx, nil); got != ctx {
		t.Error("withNodes(nil) should return the original context unchanged")
	}
	if got := withNodes(ctx, []string{}); got != ctx {
		t.Error("withNodes([]) should return the original context unchanged")
	}
}

// TestWithNodes_NonEmpty verifies that a non-empty nodes slice produces a new context.
func TestWithNodes_NonEmpty(t *testing.T) {
	ctx := context.Background()
	got := withNodes(ctx, []string{"192.168.2.61", "192.168.2.62"})
	if got == ctx {
		t.Error("withNodes with nodes should return a new (different) context")
	}
}
