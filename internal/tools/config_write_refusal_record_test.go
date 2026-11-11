package tools

import (
	"context"
	"strings"
	"testing"
)

const writeBusyAuditOutcome = "refused:config-write-in-progress"

func logRecordMentionsAll(log string, parts ...string) bool {
	for _, record := range strings.Split(log, "\n") {
		matched := true
		for _, part := range parts {
			if !strings.Contains(record, part) {
				matched = false
				break
			}
		}
		if matched && record != "" {
			return true
		}
	}
	return false
}

func TestBusyApplyAuditsWriteInProgress(t *testing.T) {
	log := captureDefaultSlog(t)
	hs := newWriteLockHarness(t, writeLockSetup{held: true})
	holder := writeLockCall{tool: writeLockPatchTool, dryRun: true, nodes: []string{writeLockNode}}
	refused := writeLockCall{tool: writeLockApplyTool, dryRun: true, nodes: []string{writeLockNode}}
	hs.holdFirst(t, holder)

	returned, err := hs.within(func(ctx context.Context) error { return hs.run(ctx, refused) })
	if !returned {
		t.Fatalf("spec AC-53: precondition expected=%s returned while %s holds the lock got=still running after %s", refused, holder, writeLockBound)
	}
	if got := auditOutcomeOf(log.String(), writeLockApplyTool); got != writeBusyAuditOutcome {
		t.Errorf("spec AC-53: expected AUDIT_OUTCOME outcome %q for %s while %s holds the lock got %q (err:%v)",
			writeBusyAuditOutcome, refused, holder, got, err)
	}
}

func TestBusyPatchRefusalIsLogged(t *testing.T) {
	log := captureDefaultSlog(t)
	hs := newWriteLockHarness(t, writeLockSetup{held: true})
	holder := writeLockCall{tool: writeLockApplyTool, dryRun: true, nodes: []string{writeLockNode}}
	refused := writeLockCall{tool: writeLockPatchTool, dryRun: true, nodes: []string{writeLockNode}}
	hs.holdFirst(t, holder)

	returned, err := hs.within(func(ctx context.Context) error { return hs.run(ctx, refused) })
	if !returned {
		t.Fatalf("spec AC-54: precondition expected=%s returned while %s holds the lock got=still running after %s", refused, holder, writeLockBound)
	}
	if !logRecordMentionsAll(log.String(), writeLockPatchTool, writeLockBusyText) {
		t.Errorf("spec AC-54: expected a server log record containing %q and %q for %s while %s holds the lock got=none (err:%v) log:\n%s",
			writeLockPatchTool, writeLockBusyText, refused, holder, err, log.String())
	}
}
