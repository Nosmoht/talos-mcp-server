package tools

import (
	"context"
	"strings"
	"testing"
	"testing/synctest"
)

func TestGivenConfigFetchFailsWhenPatchingThenServerLogsError(t *testing.T) {
	log := captureDefaultSlog(t)
	hs := newWriteLockHarness(t, writeLockSetup{noMachineConfig: true})
	call := writeLockCall{tool: writeLockPatchTool, dryRun: true, nodes: []string{writeLockNode}}

	if err := hs.run(context.Background(), call); err == nil {
		t.Fatalf("review-operational-safety finding 1: expected=%s fails without a machine config got=nil error", call)
	}
	for _, line := range strings.Split(log.String(), "\n") {
		if strings.Contains(line, "tool="+writeLockPatchTool) && strings.Contains(line, "get current machine config") {
			return
		}
	}
	t.Errorf("review-operational-safety finding 1: expected=a server log line with tool=%s containing %q got=%q",
		writeLockPatchTool, "get current machine config", log.String())
}

func TestPatchConfigTimeoutLogsOutcomeUnknown(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		log := captureDefaultSlog(t)
		hs := newContextBoundHarness(t)
		call := writeLockCall{tool: writeLockPatchTool, dryRun: false, nodes: []string{writeLockNode}}

		obs := observeAcrossDeadline(hs, call)
		if !obs.returned {
			t.Fatalf("diff-d1 operability/patch-outcome-unknown-not-recorded-server-side: expected=%s returned by %s got=still running", call, deadlinePassed)
		}
		for _, line := range strings.Split(log.String(), "\n") {
			if strings.Contains(line, "tool="+writeLockPatchTool) && strings.Contains(line, outcomeUnknownText) {
				return
			}
		}
		t.Errorf("diff-d1 operability/patch-outcome-unknown-not-recorded-server-side: expected=a server log line with tool=%s containing %q got=%q",
			writeLockPatchTool, outcomeUnknownText, log.String())
	})
}
