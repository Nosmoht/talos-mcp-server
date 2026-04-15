package subscriptions

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cosi-project/runtime/pkg/resource"
	"github.com/cosi-project/runtime/pkg/resource/meta"
	"github.com/cosi-project/runtime/pkg/resource/meta/spec"
	cosistate "github.com/cosi-project/runtime/pkg/state"
	"google.golang.org/grpc"

	clusterapi "github.com/siderolabs/talos/pkg/machinery/api/cluster"
	commonapi "github.com/siderolabs/talos/pkg/machinery/api/common"
	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	talosclient "github.com/siderolabs/talos/pkg/machinery/client"
	"io"

	"github.com/Nosmoht/talos-mcp-server/internal/talos"
	"github.com/Nosmoht/talos-mcp-server/internal/version"
)

// ── Fakes ────────────────────────────────────────────────────────────────────

// fakeCore implements state.CoreState enough for Manager's Watch paths.
// All other methods panic — tests never reach them.
type fakeCore struct {
	mu                sync.Mutex
	singleCh          chan<- cosistate.Event
	aggCh             chan<- []cosistate.Event
	watchStartErr     error
	watchKindStartErr error
}

func (f *fakeCore) emit(ev cosistate.Event) {
	f.mu.Lock()
	ch := f.singleCh
	f.mu.Unlock()
	if ch != nil {
		ch <- ev
	}
}

func (f *fakeCore) emitBatch(evs []cosistate.Event) {
	f.mu.Lock()
	ch := f.aggCh
	f.mu.Unlock()
	if ch != nil {
		ch <- evs
	}
}

func (f *fakeCore) Get(context.Context, resource.Pointer, ...cosistate.GetOption) (resource.Resource, error) {
	panic("Get not used in tests")
}

func (f *fakeCore) List(context.Context, resource.Kind, ...cosistate.ListOption) (resource.List, error) {
	panic("List not used in tests")
}

func (f *fakeCore) Create(context.Context, resource.Resource, ...cosistate.CreateOption) error {
	panic("Create not used in tests")
}

func (f *fakeCore) Update(context.Context, resource.Resource, ...cosistate.UpdateOption) error {
	panic("Update not used in tests")
}

func (f *fakeCore) Destroy(context.Context, resource.Pointer, ...cosistate.DestroyOption) error {
	panic("Destroy not used in tests")
}

func (f *fakeCore) Watch(ctx context.Context, _ resource.Pointer, ch chan<- cosistate.Event, _ ...cosistate.WatchOption) error {
	if f.watchStartErr != nil {
		return f.watchStartErr
	}
	f.mu.Lock()
	f.singleCh = ch
	f.mu.Unlock()
	go func() {
		<-ctx.Done()
	}()
	return nil
}

func (f *fakeCore) WatchKind(_ context.Context, _ resource.Kind, _ chan<- cosistate.Event, _ ...cosistate.WatchKindOption) error {
	return errors.New("WatchKind not used")
}

func (f *fakeCore) WatchKindAggregated(ctx context.Context, _ resource.Kind, ch chan<- []cosistate.Event, _ ...cosistate.WatchKindOption) error {
	if f.watchKindStartErr != nil {
		return f.watchKindStartErr
	}
	f.mu.Lock()
	f.aggCh = ch
	f.mu.Unlock()
	go func() {
		<-ctx.Done()
	}()
	return nil
}

// fakeClient is a minimal talos.ClientInterface that exposes a fakeCore via
// COSIState() and resolves resource kinds from a static map.
type fakeClient struct {
	state *cosistate.State
	kinds map[string]*meta.ResourceDefinition // rawType → canonical definition
}

var _ talos.ClientInterface = (*fakeClient)(nil)

func (f *fakeClient) COSIState() cosistate.State { return *f.state }

func (f *fakeClient) ResolveResourceKind(_ context.Context, _ *resource.Namespace, rawType resource.Type) (*meta.ResourceDefinition, error) {
	if rd, ok := f.kinds[rawType]; ok {
		return rd, nil
	}
	return nil, fmt.Errorf("unknown resource type %q", rawType)
}

// All other ClientInterface methods are unused; unreachable panics keep
// tests honest.
func (f *fakeClient) Version(context.Context, ...grpc.CallOption) (*machineapi.VersionResponse, error) {
	panic("Version unused")
}
func (f *fakeClient) ServiceList(context.Context, ...grpc.CallOption) (*machineapi.ServiceListResponse, error) {
	panic("ServiceList unused")
}
func (f *fakeClient) ServiceStart(context.Context, string, ...grpc.CallOption) (*machineapi.ServiceStartResponse, error) {
	panic("ServiceStart unused")
}
func (f *fakeClient) ServiceStop(context.Context, string, ...grpc.CallOption) (*machineapi.ServiceStopResponse, error) {
	panic("ServiceStop unused")
}
func (f *fakeClient) ServiceRestart(context.Context, string, ...grpc.CallOption) (*machineapi.ServiceRestartResponse, error) {
	panic("ServiceRestart unused")
}
func (f *fakeClient) Containers(context.Context, string, commonapi.ContainerDriver, ...grpc.CallOption) (*machineapi.ContainersResponse, error) {
	panic("Containers unused")
}
func (f *fakeClient) Processes(context.Context, ...grpc.CallOption) (*machineapi.ProcessesResponse, error) {
	panic("Processes unused")
}
func (f *fakeClient) ClusterHealthCheck(context.Context, time.Duration, *clusterapi.ClusterInfo) (clusterapi.ClusterService_HealthCheckClient, error) {
	panic("ClusterHealthCheck unused")
}
func (f *fakeClient) Reboot(context.Context, ...talosclient.RebootMode) error { panic("Reboot unused") }
func (f *fakeClient) UpgradeWithOptions(context.Context, ...talosclient.UpgradeOption) (*machineapi.UpgradeResponse, error) {
	panic("UpgradeWithOptions unused")
}
func (f *fakeClient) Rollback(context.Context) error { panic("Rollback unused") }
func (f *fakeClient) ApplyConfiguration(context.Context, *machineapi.ApplyConfigurationRequest, ...grpc.CallOption) (*machineapi.ApplyConfigurationResponse, error) {
	panic("ApplyConfiguration unused")
}
func (f *fakeClient) ResetGenericWithResponse(context.Context, *machineapi.ResetRequest) (*machineapi.ResetResponse, error) {
	panic("ResetGenericWithResponse unused")
}
func (f *fakeClient) Logs(context.Context, string, commonapi.ContainerDriver, string, bool, int32) (machineapi.MachineService_LogsClient, error) {
	panic("Logs unused")
}
func (f *fakeClient) Dmesg(context.Context, bool, bool) (machineapi.MachineService_DmesgClient, error) {
	panic("Dmesg unused")
}
func (f *fakeClient) EventsWatch(context.Context, func(<-chan talosclient.Event), ...talosclient.EventsOptionFunc) error {
	panic("EventsWatch unused")
}
func (f *fakeClient) LS(context.Context, *machineapi.ListRequest) (machineapi.MachineService_ListClient, error) {
	panic("LS unused")
}
func (f *fakeClient) Read(context.Context, string) (io.ReadCloser, error) { panic("Read unused") }
func (f *fakeClient) EtcdMemberList(context.Context, *machineapi.EtcdMemberListRequest, ...grpc.CallOption) (*machineapi.EtcdMemberListResponse, error) {
	panic("EtcdMemberList unused")
}
func (f *fakeClient) EtcdStatus(context.Context, ...grpc.CallOption) (*machineapi.EtcdStatusResponse, error) {
	panic("EtcdStatus unused")
}
func (f *fakeClient) EtcdSnapshot(context.Context, *machineapi.EtcdSnapshotRequest, ...grpc.CallOption) (io.ReadCloser, error) {
	panic("EtcdSnapshot unused")
}
func (f *fakeClient) GetNodeVersion(context.Context, string) (*version.TalosVersion, error) {
	panic("GetNodeVersion unused")
}
func (f *fakeClient) GetClusterVersion(context.Context) (*version.TalosVersion, error) {
	panic("GetClusterVersion unused")
}
func (f *fakeClient) InvalidateVersionCache()    { panic("InvalidateVersionCache unused") }
func (f *fakeClient) Ping(context.Context) error { panic("Ping unused") }

// ── Test setup helpers ──────────────────────────────────────────────────────

const (
	testNode       = "192.0.2.10"
	testURIList    = "talos://192.0.2.10/resource/runtime/MachineStatus"
	testURIItem    = "talos://192.0.2.10/resource/runtime/MachineStatus/node-1"
	testClusterURI = "talos://cluster/version"
)

func makeResourceDef(canonical string) *meta.ResourceDefinition {
	rd, err := meta.NewResourceDefinition(spec.ResourceDefinitionSpec{
		Type:             canonical,
		DefaultNamespace: "runtime",
	})
	if err != nil {
		panic(err)
	}
	return rd
}

// setup builds a Manager + fakeCore pair with allowedNodes set to testNode.
// rateEvery is very tight (1 µs) so tests don't rely on wall-clock timing.
func setup(t *testing.T, burst int) (*Manager, *fakeCore, func(uri string)) {
	t.Helper()

	allow, err := talos.ParseNodeAllowlist(testNode)
	if err != nil {
		t.Fatalf("ParseNodeAllowlist: %v", err)
	}

	fc := &fakeCore{}
	st := cosistate.WrapCore(fc)

	kinds := map[string]*meta.ResourceDefinition{
		"MachineStatus": makeResourceDef("MachineStatuses.runtime.talos.dev"),
		"ms":            makeResourceDef("MachineStatuses.runtime.talos.dev"),
		"Member":        makeResourceDef("Members.cluster.talos.dev"),
		"NodeAddress":   makeResourceDef("NodeAddresses.net.talos.dev"),
		"Service":       makeResourceDef("Services.v1alpha1.talos.dev"),
		"LinkStatus":    makeResourceDef("LinkStatuses.net.talos.dev"), // NOT in allowlist
	}

	fake := &fakeClient{state: &st, kinds: kinds}
	m := NewManager(fake, allow, time.Microsecond, burst)

	notified := make(chan string, 64)
	fn := notifyFunc(func(_ context.Context, uri string) error {
		notified <- uri
		return nil
	})
	m.notify.Store(&fn)

	t.Cleanup(func() { m.Shutdown() })

	expect := func(wantURI string) {
		t.Helper()
		select {
		case got := <-notified:
			if got != wantURI {
				t.Errorf("notification URI = %q, want %q", got, wantURI)
			}
		case <-time.After(500 * time.Millisecond):
			t.Fatalf("no notification within timeout (wanted %q)", wantURI)
		}
	}
	return m, fc, expect
}

// ── Tests ────────────────────────────────────────────────────────────────────

func TestSubscribe_SingleResource_Delivers(t *testing.T) {
	m, fc, expect := setup(t, 5)
	if err := m.subscribe(context.Background(), "sess-A", testURIItem); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	// Give runWatch a tick to call Watch and record singleCh.
	time.Sleep(10 * time.Millisecond)
	fc.emit(cosistate.Event{Type: cosistate.Updated})
	expect(testURIItem)
}

func TestSubscribe_ListURI_DeliversAggregated(t *testing.T) {
	m, fc, expect := setup(t, 5)
	if err := m.subscribe(context.Background(), "sess-A", testURIList); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	time.Sleep(10 * time.Millisecond)
	fc.emitBatch([]cosistate.Event{{Type: cosistate.Updated}, {Type: cosistate.Created}})
	expect(testURIList)
	expect(testURIList)
}

func TestSubscribe_ClusterURI_Rejected(t *testing.T) {
	m, _, _ := setup(t, 5)
	err := m.subscribe(context.Background(), "sess-A", testClusterURI)
	if err == nil || !strings.Contains(err.Error(), "invalid URI") {
		t.Errorf("cluster URI should reject with 'invalid URI': %v", err)
	}
}

func TestSubscribe_DisallowedType_Rejected(t *testing.T) {
	m, _, _ := setup(t, 5)
	uri := "talos://192.0.2.10/resource/net/LinkStatus/eth0"
	err := m.subscribe(context.Background(), "sess-A", uri)
	if err == nil || !strings.Contains(err.Error(), "not subscribable") {
		t.Errorf("LinkStatus should reject with 'not subscribable': %v", err)
	}
}

func TestSubscribe_Alias_ResolvesThenAllowlisted(t *testing.T) {
	m, fc, expect := setup(t, 5)
	// Alias "ms" resolves to canonical MachineStatuses.runtime.talos.dev which IS in AllowedTypes.
	uri := "talos://192.0.2.10/resource/runtime/ms/node-1"
	if err := m.subscribe(context.Background(), "sess-A", uri); err != nil {
		t.Fatalf("alias subscribe should succeed: %v", err)
	}
	time.Sleep(10 * time.Millisecond)
	fc.emit(cosistate.Event{Type: cosistate.Updated})
	expect(uri)
}

func TestSubscribe_NodeNotAllowed_Rejected(t *testing.T) {
	m, _, _ := setup(t, 5)
	uri := "talos://203.0.113.1/resource/runtime/MachineStatus/node-1"
	err := m.subscribe(context.Background(), "sess-A", uri)
	if err == nil || !strings.Contains(err.Error(), "not in the allowed nodes") {
		t.Errorf("non-allowlist node should reject: %v", err)
	}
}

func TestBootstrapped_Dropped(t *testing.T) {
	m, fc, _ := setup(t, 5)
	if err := m.subscribe(context.Background(), "sess-A", testURIItem); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	time.Sleep(10 * time.Millisecond)
	fc.emit(cosistate.Event{Type: cosistate.Bootstrapped})

	// Bootstrapped must NOT produce a notification.
	notified := make(chan struct{}, 1)
	fn := notifyFunc(func(context.Context, string) error {
		notified <- struct{}{}
		return nil
	})
	m.notify.Store(&fn)
	fc.emit(cosistate.Event{Type: cosistate.Bootstrapped})
	select {
	case <-notified:
		t.Error("Bootstrapped event was forwarded")
	case <-time.After(100 * time.Millisecond):
		// good — dropped
	}
}

func TestErrored_StopsGoroutine(t *testing.T) {
	m, fc, _ := setup(t, 5)
	if err := m.subscribe(context.Background(), "sess-A", testURIItem); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	time.Sleep(10 * time.Millisecond)
	fc.emit(cosistate.Event{Type: cosistate.Errored, Error: errors.New("node down")})
	// Give the goroutine a moment to exit; subsequent events must not deliver.
	time.Sleep(20 * time.Millisecond)

	delivered := make(chan struct{}, 1)
	fn := notifyFunc(func(context.Context, string) error {
		delivered <- struct{}{}
		return nil
	})
	m.notify.Store(&fn)
	fc.emit(cosistate.Event{Type: cosistate.Updated})
	select {
	case <-delivered:
		t.Error("goroutine delivered after Errored — it should have exited")
	case <-time.After(100 * time.Millisecond):
		// good
	}
}

func TestUnsubscribe_CancelsGoroutine(t *testing.T) {
	m, fc, _ := setup(t, 5)
	if err := m.subscribe(context.Background(), "sess-A", testURIItem); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	time.Sleep(10 * time.Millisecond)
	m.unsubscribe("sess-A", testURIItem)

	// Verify the sub is gone from the map.
	m.mu.Lock()
	_, present := m.subs[subKey{sessionID: "sess-A", uri: testURIItem}]
	m.mu.Unlock()
	if present {
		t.Error("subscription still present after unsubscribe")
	}
	// Event after cancel must not deliver (channel send into closed ctx is drained by goroutine exit).
	delivered := make(chan struct{}, 1)
	fn := notifyFunc(func(context.Context, string) error {
		delivered <- struct{}{}
		return nil
	})
	m.notify.Store(&fn)
	time.Sleep(10 * time.Millisecond)
	select {
	case fc.singleCh <- cosistate.Event{Type: cosistate.Updated}:
		// non-blocking send: channel buffer absorbs; goroutine has exited so no read happens.
	default:
	}
	select {
	case <-delivered:
		t.Error("delivered after unsubscribe")
	case <-time.After(50 * time.Millisecond):
		// good
	}
}

func TestSupervisor_TearsDownAllSessionURIs(t *testing.T) {
	m, _, _ := setup(t, 5)
	ctx := context.Background()
	if err := m.subscribe(ctx, "sess-X", testURIItem); err != nil {
		t.Fatal(err)
	}
	if err := m.subscribe(ctx, "sess-X", testURIList); err != nil {
		t.Fatal(err)
	}
	time.Sleep(10 * time.Millisecond)

	m.cleanupSession("sess-X")

	m.mu.Lock()
	remaining := len(m.subs) + len(m.sessions)
	m.mu.Unlock()
	if remaining != 0 {
		t.Errorf("after cleanupSession, subs+sessions = %d, want 0", remaining)
	}
}

func TestRateLimiter_DropsOverBurst(t *testing.T) {
	// Much slower rate than the other tests so we can observe drops.
	allow, _ := talos.ParseNodeAllowlist(testNode)
	fc := &fakeCore{}
	st := cosistate.WrapCore(fc)
	fake := &fakeClient{
		state: &st,
		kinds: map[string]*meta.ResourceDefinition{
			"MachineStatus": makeResourceDef("MachineStatuses.runtime.talos.dev"),
		},
	}
	m := NewManager(fake, allow, time.Second, 3) // 1/s, burst 3
	t.Cleanup(m.Shutdown)

	var count atomic.Int32
	fn := notifyFunc(func(context.Context, string) error {
		count.Add(1)
		return nil
	})
	m.notify.Store(&fn)

	if err := m.subscribe(context.Background(), "sess-A", testURIItem); err != nil {
		t.Fatal(err)
	}
	time.Sleep(10 * time.Millisecond)

	for i := 0; i < 10; i++ {
		fc.emit(cosistate.Event{Type: cosistate.Updated})
	}
	time.Sleep(50 * time.Millisecond)
	if got := count.Load(); got > 4 {
		t.Errorf("delivered %d events, want ≤ 4 (burst 3 + at most 1 from rate)", got)
	}
}

func TestDuplicateSubscribe_Idempotent(t *testing.T) {
	m, _, _ := setup(t, 5)
	if err := m.subscribe(context.Background(), "sess-A", testURIItem); err != nil {
		t.Fatal(err)
	}
	if err := m.subscribe(context.Background(), "sess-A", testURIItem); err != nil {
		t.Errorf("duplicate subscribe should be idempotent, got: %v", err)
	}
	m.mu.Lock()
	got := len(m.subs)
	m.mu.Unlock()
	if got != 1 {
		t.Errorf("sub count = %d, want 1 (no double entry)", got)
	}
}

func TestNotifierUnboundExitsGoroutine(t *testing.T) {
	// A fresh manager without notify bound.
	allow, _ := talos.ParseNodeAllowlist(testNode)
	fc := &fakeCore{}
	st := cosistate.WrapCore(fc)
	fake := &fakeClient{
		state: &st,
		kinds: map[string]*meta.ResourceDefinition{
			"MachineStatus": makeResourceDef("MachineStatuses.runtime.talos.dev"),
		},
	}
	m := NewManager(fake, allow, time.Microsecond, 5)
	t.Cleanup(m.Shutdown)
	// notify NOT set
	if err := m.subscribe(context.Background(), "sess-A", testURIItem); err != nil {
		t.Fatal(err)
	}
	time.Sleep(10 * time.Millisecond)
	fc.emit(cosistate.Event{Type: cosistate.Updated})

	// After the event, the goroutine should have exited (warned + returned false).
	// Verify by emitting a second event — it must not block forever.
	done := make(chan struct{})
	go func() {
		defer close(done)
		select {
		case fc.singleCh <- cosistate.Event{Type: cosistate.Updated}:
		default:
		}
	}()
	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		t.Error("second emit hung; goroutine did not exit after nil notifier")
	}
}

func TestReSubscribeAfterUnsubscribe(t *testing.T) {
	m, _, _ := setup(t, 5)
	if err := m.subscribe(context.Background(), "sess-A", testURIItem); err != nil {
		t.Fatal(err)
	}
	m.unsubscribe("sess-A", testURIItem)
	if err := m.subscribe(context.Background(), "sess-A", testURIItem); err != nil {
		t.Errorf("re-subscribe after unsubscribe failed: %v", err)
	}
}
