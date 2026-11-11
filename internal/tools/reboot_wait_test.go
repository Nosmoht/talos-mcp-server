package tools

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	talosclient "github.com/siderolabs/talos/pkg/machinery/client"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// specRebootWait is the scope of the reboot wait in the #265 triage comment:
// the boot ID is read until it changes, retrying only the errors talosctl
// retries (cmd/talosctl/pkg/talos/action/node.go) and returning any other.
const specRebootWait = "spec #265 triage scope 2 (reboot wait)"

const (
	rebootTestNode = "192.0.2.20"
	oldBootID      = "0b0e8e39-6c1b-4c87-9a49-000000000001"
	newBootID      = "0b0e8e39-6c1b-4c87-9a49-000000000002"
)

type bootIDReply struct {
	id  string
	err error
}

// bootIDClient answers boot ID reads from a script; the last reply repeats.
type bootIDClient struct {
	*mockClient

	mu      sync.Mutex
	replies []bootIDReply
	reads   int
}

func (c *bootIDClient) Read(_ context.Context, _ string) (io.ReadCloser, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	r := c.replies[min(c.reads, len(c.replies)-1)]
	c.reads++

	if r.err != nil {
		return nil, r.err
	}

	return io.NopCloser(strings.NewReader(r.id + "\n")), nil
}

func waitForReboot(t *testing.T, timeout time.Duration, replies ...bootIDReply) (*bootIDClient, *rebootResult, error) {
	t.Helper()

	client := &bootIDClient{mockClient: &mockClient{}, replies: replies}
	h := &Handlers{Client: client}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	res, err := h.waitForNodeReboot(ctx, nil, rebootTestNode, oldBootID, time.Now(), time.Millisecond)

	return client, res, err
}

func TestGivenTransientErrorsDuringReboot_WhenBootIDChanges_ThenWaitReportsTheNewBootID(t *testing.T) {
	t.Parallel()

	_, res, err := waitForReboot(t, 5*time.Second,
		bootIDReply{id: oldBootID},
		bootIDReply{err: status.Error(codes.Unavailable, "connection refused")},
		bootIDReply{err: io.EOF},
		bootIDReply{err: status.Error(codes.DeadlineExceeded, "deadline exceeded")},
		bootIDReply{err: status.Error(codes.Canceled, "context canceled")},
		bootIDReply{err: fmt.Errorf("read boot_id: %w", context.DeadlineExceeded)},
		bootIDReply{err: fmt.Errorf("read boot_id: %w", context.Canceled)},
		bootIDReply{id: newBootID},
	)
	if err != nil {
		t.Fatalf("%s: expected the wait to finish once the boot ID changes, got %v", specRebootWait, err)
	}
	if res.NewBootID != newBootID {
		t.Errorf("%s: expected NewBootID %q, got %q", specRebootWait, newBootID, res.NewBootID)
	}
}

func TestGivenBootIDReadCutShort_WhenWaiting_ThenItIsNotTakenAsANewBootID(t *testing.T) {
	t.Parallel()

	_, res, err := waitForReboot(t, 5*time.Second,
		bootIDReply{id: ""},
		bootIDReply{id: newBootID[:8]},
		bootIDReply{id: newBootID},
	)
	if err != nil {
		t.Fatalf("%s: expected the wait to finish once a complete new boot ID is read, got %v", specRebootWait, err)
	}
	if res.NewBootID != newBootID {
		t.Errorf("%s: expected NewBootID %q, not a truncated read, got %q", specRebootWait, newBootID, res.NewBootID)
	}
}

func TestGivenNonRetryableErrorDuringReboot_WhenWaiting_ThenWaitReturnsItWithoutRetrying(t *testing.T) {
	t.Parallel()

	client, res, err := waitForReboot(t, 5*time.Second,
		bootIDReply{err: status.Error(codes.PermissionDenied, "not authorized")},
		bootIDReply{id: newBootID},
	)
	if err == nil {
		t.Fatalf("%s: expected an error for PermissionDenied, got result %+v", specRebootWait, res)
	}
	if got := talosclient.StatusCode(err); got != codes.PermissionDenied {
		t.Errorf("%s: expected the returned error to keep code PermissionDenied, got %v (%v)", specRebootWait, got, err)
	}
	if client.reads != 1 {
		t.Errorf("%s: expected no retry after a non-retryable error, got %d reads", specRebootWait, client.reads)
	}
}

func TestGivenBootIDNeverChanges_WhenTimeoutExpires_ThenWaitReturnsAnError(t *testing.T) {
	t.Parallel()

	_, res, err := waitForReboot(t, 50*time.Millisecond, bootIDReply{id: oldBootID})
	if err == nil {
		t.Fatalf("%s: expected an error when the boot ID does not change, got result %+v", specRebootWait, res)
	}
	if !strings.Contains(err.Error(), "boot ID did not change") {
		t.Errorf("%s: expected the error to say the boot ID did not change, got %v", specRebootWait, err)
	}
}

func TestGivenNodeStaysUnreachable_WhenTimeoutExpires_ThenWaitReturnsTheLastError(t *testing.T) {
	t.Parallel()

	_, res, err := waitForReboot(t, 50*time.Millisecond, bootIDReply{err: status.Error(codes.Unavailable, "connection refused")})
	if err == nil {
		t.Fatalf("%s: expected an error when the node never comes back, got result %+v", specRebootWait, res)
	}
	if got := talosclient.StatusCode(err); got != codes.Unavailable {
		t.Errorf("%s: expected the timeout error to carry the last Unavailable error, got %v (%v)", specRebootWait, got, err)
	}
}
