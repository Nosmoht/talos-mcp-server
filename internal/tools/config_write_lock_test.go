package tools

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cosi-project/runtime/pkg/state"
	"github.com/cosi-project/runtime/pkg/state/impl/inmem"
	"github.com/cosi-project/runtime/pkg/state/impl/namespaced"
	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	"google.golang.org/grpc"

	"github.com/Nosmoht/talos-mcp-server/internal/talos"
)

const (
	writeLockNode       = "192.0.2.10"
	writeLockNodeMapped = "::ffff:192.0.2.10"
	writeLockOtherNode  = "192.0.2.11"
	writeLockBound      = 5 * time.Second
	writeLockPatch      = `{"machine":{"nodeLabels":{"write-lock":"true"}}}`
	writeLockPatchTool  = "talos_patch_config"
	writeLockApplyTool  = "talos_apply_config"
	writeLockBusyText   = "config write in progress"
	writeLockRetryText  = "retry"
)

var errWriteLockRPC = errors.New("write-lock stub: ApplyConfiguration failed")

type writeLockClient struct {
	*mockClient

	st             state.State
	hold           chan struct{}
	entered        chan struct{}
	failFirstApply bool

	mu      sync.Mutex
	calls   int
	applies int
}

func (c *writeLockClient) COSIState() state.State {
	c.mu.Lock()
	c.calls++
	c.mu.Unlock()
	return c.st
}

func (c *writeLockClient) ApplyConfiguration(ctx context.Context, _ *machineapi.ApplyConfigurationRequest, _ ...grpc.CallOption) (*machineapi.ApplyConfigurationResponse, error) {
	c.mu.Lock()
	c.calls++
	c.applies++
	first := c.applies == 1
	c.mu.Unlock()

	select {
	case c.entered <- struct{}{}:
	default:
	}

	if c.hold != nil {
		select {
		case <-c.hold:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	if first && c.failFirstApply {
		return nil, errWriteLockRPC
	}
	return &machineapi.ApplyConfigurationResponse{}, nil
}

func (c *writeLockClient) callCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls
}

type writeLockSetup struct {
	held            bool
	failFirstApply  bool
	noMachineConfig bool
}

type writeLockCall struct {
	tool   string
	dryRun bool
	nodes  []string
}

func (w writeLockCall) String() string {
	return fmt.Sprintf("%s(dry_run=%t nodes=%v)", w.tool, w.dryRun, w.nodes)
}

type writeLockHarness struct {
	h          *Handlers
	client     *writeLockClient
	configFile string
	wg         sync.WaitGroup
}

func newWriteLockHarness(t *testing.T, setup writeLockSetup) *writeLockHarness {
	t.Helper()

	st := state.WrapCore(namespaced.NewState(inmem.Build))
	if !setup.noMachineConfig {
		if err := st.Create(context.Background(), newAnnotatedResource(singleDocConfig)); err != nil {
			t.Fatalf("setup: seed machine config: %v", err)
		}
	}

	configFile := filepath.Join(t.TempDir(), "machine.yaml")
	if err := os.WriteFile(configFile, []byte(singleDocConfig), 0o600); err != nil {
		t.Fatalf("setup: write config file: %v", err)
	}

	client := &writeLockClient{
		mockClient:     &mockClient{},
		st:             st,
		entered:        make(chan struct{}, 1),
		failFirstApply: setup.failFirstApply,
	}
	if setup.held {
		client.hold = make(chan struct{})
	}

	hs := &writeLockHarness{h: &Handlers{Client: client}, client: client, configFile: configFile}
	t.Cleanup(func() {
		if client.hold != nil {
			close(client.hold)
		}
		hs.wg.Wait()
	})
	return hs
}

func (hs *writeLockHarness) run(ctx context.Context, w writeLockCall) error {
	dryRun := w.dryRun
	if w.tool == writeLockPatchTool {
		_, _, err := hs.h.HandlePatchConfig(ctx, nil, PatchConfigArgs{
			Patch:   writeLockPatch,
			DryRun:  &dryRun,
			Confirm: !dryRun,
			Nodes:   w.nodes,
		})
		return err
	}
	_, _, err := hs.h.HandleApplyConfig(ctx, nil, ApplyConfigArgs{
		ConfigFile: hs.configFile,
		DryRun:     &dryRun,
		Confirm:    !dryRun,
		Nodes:      w.nodes,
	})
	return err
}

func (hs *writeLockHarness) start(fn func(ctx context.Context) error) <-chan error {
	done := make(chan error, 1)
	hs.wg.Add(1)
	go func() {
		defer hs.wg.Done()
		done <- fn(context.Background())
	}()
	return done
}

func (hs *writeLockHarness) within(fn func(ctx context.Context) error) (returned bool, err error) {
	select {
	case err := <-hs.start(fn):
		return true, err
	case <-time.After(writeLockBound):
		return false, nil
	}
}

func (hs *writeLockHarness) holdFirst(t *testing.T, w writeLockCall) {
	t.Helper()

	done := hs.start(func(ctx context.Context) error { return hs.run(ctx, w) })
	select {
	case <-hs.client.entered:
	case err := <-done:
		t.Fatalf("setup: first write %s returned before reaching ApplyConfiguration: %v", w, err)
	case <-time.After(writeLockBound):
		t.Fatalf("setup: first write %s did not reach ApplyConfiguration within %s", w, writeLockBound)
	}
}

func isWriteBusyRefusal(err error) bool {
	return err != nil &&
		strings.Contains(err.Error(), writeLockBusyText) &&
		strings.Contains(err.Error(), writeLockRetryText)
}

func TestSecondAuthenticatedConfigWriteRefusedWhileOneRuns(t *testing.T) {
	patch := func(dryRun bool, nodes ...string) writeLockCall {
		return writeLockCall{tool: writeLockPatchTool, dryRun: dryRun, nodes: nodes}
	}
	apply := func(dryRun bool, nodes ...string) writeLockCall {
		return writeLockCall{tool: writeLockApplyTool, dryRun: dryRun, nodes: nodes}
	}

	cases := []struct {
		name   string
		first  writeLockCall
		second writeLockCall
	}{
		{name: "patch dry-run then patch dry-run on the same node", first: patch(true, writeLockNode), second: patch(true, writeLockNode)},
		{name: "patch dry-run then patch on another spelling of the node", first: patch(true, writeLockNode), second: patch(false, writeLockNodeMapped)},
		{name: "patch then apply dry-run with nodes omitted", first: patch(false, writeLockNode), second: apply(true)},
		{name: "patch dry-run then apply on a different node", first: patch(true, writeLockNode), second: apply(false, writeLockOtherNode)},
		{name: "patch with nodes omitted then patch with nodes omitted", first: patch(true), second: patch(true)},
		{name: "apply dry-run then patch dry-run on the same node", first: apply(true, writeLockNode), second: patch(true, writeLockNode)},
		{name: "apply then patch on a different node", first: apply(false, writeLockNode), second: patch(false, writeLockOtherNode)},
		{name: "apply dry-run then apply dry-run on another spelling of the node", first: apply(true, writeLockNode), second: apply(true, writeLockNodeMapped)},
		{name: "apply then apply with nodes omitted", first: apply(false, writeLockNode), second: apply(false)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			hs := newWriteLockHarness(t, writeLockSetup{held: true})
			hs.holdFirst(t, tc.first)

			returned, err := hs.within(func(ctx context.Context) error { return hs.run(ctx, tc.second) })
			if !returned || !isWriteBusyRefusal(err) {
				t.Errorf("spec AC-4: case=%s expected=%s refused with an error containing %q and %q while %s runs got=returned:%t err:%v",
					tc.name, tc.second, writeLockBusyText, writeLockRetryText, tc.first, returned, err)
			}
		})
	}
}

func TestRefusedConfigWriteReachesNoTalosMethod(t *testing.T) {
	cases := []struct {
		name   string
		first  writeLockCall
		second writeLockCall
	}{
		{
			name:   "patch refused while a patch runs on the same node",
			first:  writeLockCall{tool: writeLockPatchTool, dryRun: true, nodes: []string{writeLockNode}},
			second: writeLockCall{tool: writeLockPatchTool, dryRun: true, nodes: []string{writeLockNode}},
		},
		{
			name:   "patch refused while an apply runs on a different node",
			first:  writeLockCall{tool: writeLockApplyTool, dryRun: false, nodes: []string{writeLockNode}},
			second: writeLockCall{tool: writeLockPatchTool, dryRun: false, nodes: []string{writeLockOtherNode}},
		},
		{
			name:   "apply refused while a patch runs with nodes omitted",
			first:  writeLockCall{tool: writeLockPatchTool, dryRun: true},
			second: writeLockCall{tool: writeLockApplyTool, dryRun: true},
		},
		{
			name:   "apply refused while an apply runs on another spelling of the node",
			first:  writeLockCall{tool: writeLockApplyTool, dryRun: true, nodes: []string{writeLockNode}},
			second: writeLockCall{tool: writeLockApplyTool, dryRun: false, nodes: []string{writeLockNodeMapped}},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			hs := newWriteLockHarness(t, writeLockSetup{held: true})
			hs.holdFirst(t, tc.first)
			before := hs.client.callCount()

			returned, err := hs.within(func(ctx context.Context) error { return hs.run(ctx, tc.second) })
			if !returned || err == nil || !strings.Contains(err.Error(), writeLockBusyText) {
				t.Fatalf("spec AC-26: case=%s precondition expected=%s refused with %q got=returned:%t err:%v",
					tc.name, tc.second, writeLockBusyText, returned, err)
			}

			if after := hs.client.callCount(); after != before {
				t.Errorf("spec AC-26: case=%s expected=0 Talos client calls by the refused write got=%d", tc.name, after-before)
			}
		})
	}
}

func TestMaintenanceModeApplyProceedsWhileAuthenticatedWriteRuns(t *testing.T) {
	hs := newWriteLockHarness(t, writeLockSetup{held: true})

	allowlist, err := talos.ParseNodeAllowlist(insecureTestEndpoint)
	if err != nil {
		t.Fatalf("setup: parse insecure allowlist: %v", err)
	}
	fake := &fakeMaintenanceClient{}
	hs.h.EnableInsecure = true
	hs.h.InsecureAllowedNodes = allowlist
	hs.h.dialMaintenance = (&recordingDialer{client: fake}).dial

	authenticated := writeLockCall{tool: writeLockPatchTool, dryRun: true, nodes: []string{writeLockNode}}
	hs.holdFirst(t, authenticated)

	returned, err := hs.within(func(ctx context.Context) error {
		_, _, err := hs.h.HandleApplyConfig(ctx, nil, ApplyConfigArgs{
			ConfigFile: hs.configFile,
			Insecure:   true,
			Endpoint:   insecureTestEndpoint,
		})
		return err
	})
	submitted := returned && fake.gotApply != nil
	if !returned || err != nil || !submitted {
		t.Errorf("spec AC-27: expected=maintenance-mode dry-run apply succeeds and reaches the maintenance client while %s runs got=returned:%t err:%v submitted:%t",
			authenticated, returned, err, submitted)
	}
}

func TestConfigWriteAcceptedAfterEarlierWriteReturns(t *testing.T) {
	cases := []struct {
		name           string
		setup          writeLockSetup
		earlier        writeLockCall
		wantEarlierErr bool
		later          writeLockCall
	}{
		{
			name:    "after a patch succeeded",
			earlier: writeLockCall{tool: writeLockPatchTool, dryRun: true, nodes: []string{writeLockNode}},
			later:   writeLockCall{tool: writeLockApplyTool, dryRun: true, nodes: []string{writeLockNode}},
		},
		{
			name:           "after a patch failed at ApplyConfiguration",
			setup:          writeLockSetup{failFirstApply: true},
			earlier:        writeLockCall{tool: writeLockPatchTool, dryRun: false, nodes: []string{writeLockNode}},
			wantEarlierErr: true,
			later:          writeLockCall{tool: writeLockPatchTool, dryRun: true, nodes: []string{writeLockNode}},
		},
		{
			name:           "after a patch failed fetching the machine config",
			setup:          writeLockSetup{noMachineConfig: true},
			earlier:        writeLockCall{tool: writeLockPatchTool, dryRun: true, nodes: []string{writeLockNode}},
			wantEarlierErr: true,
			later:          writeLockCall{tool: writeLockApplyTool, dryRun: true, nodes: []string{writeLockNode}},
		},
		{
			name:    "after an apply succeeded",
			earlier: writeLockCall{tool: writeLockApplyTool, dryRun: false, nodes: []string{writeLockNode}},
			later:   writeLockCall{tool: writeLockPatchTool, dryRun: true, nodes: []string{writeLockNode}},
		},
		{
			name:           "after an apply failed at ApplyConfiguration",
			setup:          writeLockSetup{failFirstApply: true},
			earlier:        writeLockCall{tool: writeLockApplyTool, dryRun: true},
			wantEarlierErr: true,
			later:          writeLockCall{tool: writeLockApplyTool, dryRun: false},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			hs := newWriteLockHarness(t, tc.setup)

			earlierErr := hs.run(context.Background(), tc.earlier)
			if (earlierErr != nil) != tc.wantEarlierErr {
				t.Fatalf("spec AC-28: case=%s precondition expected earlier %s error=%t got=%v",
					tc.name, tc.earlier, tc.wantEarlierErr, earlierErr)
			}

			returned, err := hs.within(func(ctx context.Context) error { return hs.run(ctx, tc.later) })
			if !returned || (err != nil && strings.Contains(err.Error(), writeLockBusyText)) {
				t.Errorf("spec AC-28: case=%s expected=%s not refused with %q got=returned:%t err:%v",
					tc.name, tc.later, writeLockBusyText, returned, err)
			}
		})
	}
}
