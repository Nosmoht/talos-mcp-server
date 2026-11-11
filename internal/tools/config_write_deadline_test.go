package tools

import (
	"context"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/cosi-project/runtime/pkg/resource"
	"github.com/cosi-project/runtime/pkg/state"
	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	"google.golang.org/grpc"
	"google.golang.org/grpc/status"
)

const (
	deadlineNotYet         = 29 * time.Second
	deadlinePassed         = 31 * time.Second
	outcomeUnknownText     = "outcome unknown"
	outcomeUnknownCheckRef = "talos_get"
	outcomeUnknownAudit    = "outcome-unknown"
)

const immediateFailureAudit = "rpc-error"

type contextBoundApplyClient struct {
	*writeLockClient

	release chan struct{}
}

func (c *contextBoundApplyClient) ApplyConfiguration(ctx context.Context, _ *machineapi.ApplyConfigurationRequest, _ ...grpc.CallOption) (*machineapi.ApplyConfigurationResponse, error) {
	select {
	case <-ctx.Done():
		return nil, status.FromContextError(ctx.Err()).Err()
	case <-c.release:
		return nil, status.FromContextError(context.Canceled).Err()
	}
}

func newContextBoundHarness(t *testing.T) *writeLockHarness {
	t.Helper()

	hs := newWriteLockHarness(t, writeLockSetup{})
	client := &contextBoundApplyClient{writeLockClient: hs.client, release: make(chan struct{})}
	hs.h.Client = client
	// Ends a call that has no deadline, so the bubble exits and the test fails by its assertion instead of a deadlock panic.
	t.Cleanup(func() { close(client.release) })
	return hs
}

type deadlineObservation struct {
	returnedEarly bool
	returned      bool
	err           error
}

func observeAcrossDeadline(hs *writeLockHarness, w writeLockCall) deadlineObservation {
	done := hs.start(func(ctx context.Context) error { return hs.run(ctx, w) })

	time.Sleep(deadlineNotYet)
	synctest.Wait()
	select {
	case err := <-done:
		return deadlineObservation{returnedEarly: true, returned: true, err: err}
	default:
	}

	time.Sleep(deadlinePassed - deadlineNotYet)
	synctest.Wait()
	select {
	case err := <-done:
		return deadlineObservation{returned: true, err: err}
	default:
		return deadlineObservation{}
	}
}

func reportsUnknownOutcome(err error) bool {
	return err != nil &&
		strings.Contains(err.Error(), outcomeUnknownText) &&
		strings.Contains(err.Error(), outcomeUnknownCheckRef)
}

func TestPatchConfigTimeoutReportsOutcomeUnknown(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		hs := newContextBoundHarness(t)
		call := writeLockCall{tool: writeLockPatchTool, dryRun: false, nodes: []string{writeLockNode}}

		obs := observeAcrossDeadline(hs, call)
		if obs.returnedEarly {
			t.Fatalf("spec AC-48: expected=%s still running after %s got=returned err:%v", call, deadlineNotYet, obs.err)
		}
		if !obs.returned || !reportsUnknownOutcome(obs.err) {
			t.Errorf("spec AC-48: expected=%s returned by %s with an error containing %q and %q got=returned:%t err:%v",
				call, deadlinePassed, outcomeUnknownText, outcomeUnknownCheckRef, obs.returned, obs.err)
		}
	})
}

func TestApplyConfigTimeoutReportsOutcomeUnknown(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		hs := newContextBoundHarness(t)
		call := writeLockCall{tool: writeLockApplyTool, dryRun: false, nodes: []string{writeLockNode}}

		obs := observeAcrossDeadline(hs, call)
		if obs.returnedEarly {
			t.Fatalf("spec AC-49: expected=%s still running after %s got=returned err:%v", call, deadlineNotYet, obs.err)
		}
		if !obs.returned || !reportsUnknownOutcome(obs.err) {
			t.Errorf("spec AC-49: expected=%s returned by %s with an error containing %q and %q got=returned:%t err:%v",
				call, deadlinePassed, outcomeUnknownText, outcomeUnknownCheckRef, obs.returned, obs.err)
		}
	})
}

func TestApplyConfigTimeoutAuditsOutcomeUnknown(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		log := captureDefaultSlog(t)
		hs := newContextBoundHarness(t)
		call := writeLockCall{tool: writeLockApplyTool, dryRun: false, nodes: []string{writeLockNode}}

		obs := observeAcrossDeadline(hs, call)
		if !obs.returned {
			t.Fatalf("spec AC-50: expected=%s returned by %s with AUDIT_OUTCOME outcome %q got=still running",
				call, deadlinePassed, outcomeUnknownAudit)
		}
		if got := auditOutcomeOf(log.String(), writeLockApplyTool); got != outcomeUnknownAudit {
			t.Errorf("spec AC-50: expected AUDIT_OUTCOME outcome %q got %q", outcomeUnknownAudit, got)
		}
	})
}

func TestDryRunPatchTimeoutClaimsNoUnknownOutcome(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		hs := newContextBoundHarness(t)
		call := writeLockCall{tool: writeLockPatchTool, dryRun: true, nodes: []string{writeLockNode}}

		obs := observeAcrossDeadline(hs, call)
		if !obs.returned || obs.err == nil || strings.Contains(obs.err.Error(), outcomeUnknownText) {
			t.Errorf("spec AC-51: expected=%s returned by %s with an error not containing %q got=returned:%t err:%v",
				call, deadlinePassed, outcomeUnknownText, obs.returned, obs.err)
		}
	})
}

func TestConfigWriteLockFreeAfterTimeout(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		hs := newContextBoundHarness(t)
		first := writeLockCall{tool: writeLockPatchTool, dryRun: false, nodes: []string{writeLockNode}}
		second := writeLockCall{tool: writeLockPatchTool, dryRun: false, nodes: []string{writeLockNode}}

		if obs := observeAcrossDeadline(hs, first); !obs.returned {
			t.Fatalf("spec AC-52: precondition expected=%s returned by %s got=still running", first, deadlinePassed)
		}

		hs.h.Client = hs.client
		err := hs.run(context.Background(), second)

		hs.client.mu.Lock()
		reached := hs.client.applies
		hs.client.mu.Unlock()
		if err != nil || reached != 1 {
			t.Errorf("spec AC-52: expected=%s reaches ApplyConfiguration once and returns no error got=ApplyConfiguration calls:%d err:%v",
				second, reached, err)
		}
	})
}

func TestPatchConfigImmediateFailureClaimsNoUnknownOutcome(t *testing.T) {
	hs := newWriteLockHarness(t, writeLockSetup{failFirstApply: true})
	call := writeLockCall{tool: writeLockPatchTool, dryRun: false, nodes: []string{writeLockNode}}

	err := hs.run(context.Background(), call)
	if err == nil || strings.Contains(err.Error(), outcomeUnknownText) {
		t.Errorf("spec AC-58: expected=%s fails at once with an error not containing %q got err:%v",
			call, outcomeUnknownText, err)
	}
}

func TestApplyConfigImmediateFailureAuditsRPCError(t *testing.T) {
	log := captureDefaultSlog(t)
	hs := newWriteLockHarness(t, writeLockSetup{failFirstApply: true})
	call := writeLockCall{tool: writeLockApplyTool, dryRun: false, nodes: []string{writeLockNode}}

	err := hs.run(context.Background(), call)
	if err == nil || strings.Contains(err.Error(), outcomeUnknownText) {
		t.Errorf("spec AC-59: expected=%s fails at once with an error not containing %q got err:%v",
			call, outcomeUnknownText, err)
	}
	if got := auditOutcomeOf(log.String(), writeLockApplyTool); got != immediateFailureAudit {
		t.Errorf("spec AC-59: expected AUDIT_OUTCOME outcome %q got %q", immediateFailureAudit, got)
	}
}

func TestDryRunApplyTimeoutClaimsNoUnknownOutcome(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		log := captureDefaultSlog(t)
		hs := newContextBoundHarness(t)
		call := writeLockCall{tool: writeLockApplyTool, dryRun: true, nodes: []string{writeLockNode}}

		obs := observeAcrossDeadline(hs, call)
		if !obs.returned || obs.err == nil || strings.Contains(obs.err.Error(), outcomeUnknownText) {
			t.Fatalf("spec AC-60: expected=%s returned by %s with an error not containing %q got=returned:%t err:%v",
				call, deadlinePassed, outcomeUnknownText, obs.returned, obs.err)
		}
		if got := auditOutcomeOf(log.String(), writeLockApplyTool); got == "" || got == outcomeUnknownAudit {
			t.Errorf("spec AC-60: expected an AUDIT_OUTCOME record with an outcome other than %q got %q", outcomeUnknownAudit, got)
		}
	})
}

type contextBoundGetState struct {
	state.State

	release chan struct{}
}

func (s *contextBoundGetState) Get(ctx context.Context, _ resource.Pointer, _ ...state.GetOption) (resource.Resource, error) {
	select {
	case <-ctx.Done():
		return nil, status.FromContextError(ctx.Err()).Err()
	case <-s.release:
		return nil, status.FromContextError(context.Canceled).Err()
	}
}

func TestPatchConfigFetchTimeoutReturns(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		hs := newWriteLockHarness(t, writeLockSetup{})
		st := &contextBoundGetState{State: hs.client.st, release: make(chan struct{})}
		hs.client.st = st
		// Ends a fetch that has no deadline, so the bubble exits and the test fails by its assertion instead of a deadlock panic.
		t.Cleanup(func() { close(st.release) })
		call := writeLockCall{tool: writeLockPatchTool, dryRun: false, nodes: []string{writeLockNode}}

		obs := observeAcrossDeadline(hs, call)
		if obs.returnedEarly {
			t.Fatalf("spec AC-61: expected=%s still running after %s got=returned err:%v", call, deadlineNotYet, obs.err)
		}
		if !obs.returned || obs.err == nil {
			t.Errorf("spec AC-61: expected=%s returned by %s with an error got=returned:%t err:%v",
				call, deadlinePassed, obs.returned, obs.err)
		}
	})
}
