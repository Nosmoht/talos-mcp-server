package subscriptions

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/cosi-project/runtime/pkg/resource"
	"github.com/cosi-project/runtime/pkg/resource/meta"
	"github.com/cosi-project/runtime/pkg/resource/meta/spec"
	cosistate "github.com/cosi-project/runtime/pkg/state"

	"github.com/Nosmoht/talos-mcp-server/internal/talos"
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

// fakeClient is a Source that exposes a fakeCore via COSIState() and
// resolves resource kinds from a static map.
type fakeClient struct {
	state *cosistate.State
	kinds map[string]*meta.ResourceDefinition // rawType → canonical definition
}

var _ Source = (*fakeClient)(nil)

func (f *fakeClient) COSIState() cosistate.State { return *f.state }

func (f *fakeClient) ResolveResourceKind(_ context.Context, _ *resource.Namespace, rawType resource.Type) (*meta.ResourceDefinition, error) {
	if rd, ok := f.kinds[rawType]; ok {
		return rd, nil
	}
	return nil, fmt.Errorf("unknown resource type %q", rawType)
}

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
		synctest.Wait()
		select {
		case got := <-notified:
			if got != wantURI {
				t.Errorf("notification URI = %q, want %q", got, wantURI)
			}
		default:
			t.Fatalf("no notification (wanted %q)", wantURI)
		}
	}
	return m, fc, expect
}

// ── Tests ────────────────────────────────────────────────────────────────────

func TestSubscribe_SingleResource_Delivers(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		m, fc, expect := setup(t, 5)
		if err := m.subscribe(context.Background(), "sess-A", testURIItem); err != nil {
			t.Fatalf("subscribe: %v", err)
		}
		synctest.Wait()
		fc.emit(cosistate.Event{Type: cosistate.Updated})
		expect(testURIItem)
	})
}

func TestSubscribe_ListURI_DeliversAggregated(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		m, fc, expect := setup(t, 5)
		if err := m.subscribe(context.Background(), "sess-A", testURIList); err != nil {
			t.Fatalf("subscribe: %v", err)
		}
		synctest.Wait()
		fc.emitBatch([]cosistate.Event{{Type: cosistate.Updated}, {Type: cosistate.Created}})
		expect(testURIList)
		expect(testURIList)
	})
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
	synctest.Test(t, func(t *testing.T) {
		m, fc, expect := setup(t, 5)
		// "ms" resolves to MachineStatuses.runtime.talos.dev, which the allowed-type set contains.
		uri := "talos://192.0.2.10/resource/runtime/ms/node-1"
		if err := m.subscribe(context.Background(), "sess-A", uri); err != nil {
			t.Fatalf("alias subscribe should succeed: %v", err)
		}
		synctest.Wait()
		fc.emit(cosistate.Event{Type: cosistate.Updated})
		expect(uri)
	})
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
	synctest.Test(t, func(t *testing.T) {
		m, fc, _ := setup(t, 5)
		if err := m.subscribe(context.Background(), "sess-A", testURIItem); err != nil {
			t.Fatalf("subscribe: %v", err)
		}
		synctest.Wait()
		fc.emit(cosistate.Event{Type: cosistate.Bootstrapped})

		notified := make(chan struct{}, 1)
		fn := notifyFunc(func(context.Context, string) error {
			notified <- struct{}{}
			return nil
		})
		m.notify.Store(&fn)
		fc.emit(cosistate.Event{Type: cosistate.Bootstrapped})
		synctest.Wait()
		select {
		case <-notified:
			t.Error("Bootstrapped event was forwarded")
		default:
		}
	})
}

func TestErrored_StopsGoroutine(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		m, fc, _ := setup(t, 5)
		if err := m.subscribe(context.Background(), "sess-A", testURIItem); err != nil {
			t.Fatalf("subscribe: %v", err)
		}
		synctest.Wait()
		fc.emit(cosistate.Event{Type: cosistate.Errored, Error: errors.New("node down")})
		synctest.Wait()

		delivered := make(chan struct{}, 1)
		fn := notifyFunc(func(context.Context, string) error {
			delivered <- struct{}{}
			return nil
		})
		m.notify.Store(&fn)
		fc.emit(cosistate.Event{Type: cosistate.Updated})
		synctest.Wait()
		select {
		case <-delivered:
			t.Error("goroutine delivered after Errored — it should have exited")
		default:
		}
	})
}

func TestUnsubscribe_CancelsGoroutine(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		m, fc, _ := setup(t, 5)
		if err := m.subscribe(context.Background(), "sess-A", testURIItem); err != nil {
			t.Fatalf("subscribe: %v", err)
		}
		synctest.Wait()
		m.unsubscribe("sess-A", testURIItem)

		m.mu.Lock()
		_, present := m.subs[subKey{sessionID: "sess-A", uri: testURIItem}]
		m.mu.Unlock()
		if present {
			t.Error("subscription still present after unsubscribe")
		}
		delivered := make(chan struct{}, 1)
		fn := notifyFunc(func(context.Context, string) error {
			delivered <- struct{}{}
			return nil
		})
		m.notify.Store(&fn)
		synctest.Wait()
		select {
		case fc.singleCh <- cosistate.Event{Type: cosistate.Updated}:
		default:
		}
		synctest.Wait()
		select {
		case <-delivered:
			t.Error("delivered after unsubscribe")
		default:
		}
	})
}

func TestSupervisor_TearsDownAllSessionURIs(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		m, _, _ := setup(t, 5)
		ctx := context.Background()
		if err := m.subscribe(ctx, "sess-X", testURIItem); err != nil {
			t.Fatal(err)
		}
		if err := m.subscribe(ctx, "sess-X", testURIList); err != nil {
			t.Fatal(err)
		}
		synctest.Wait()

		m.cleanupSession("sess-X")

		m.mu.Lock()
		remaining := len(m.subs) + len(m.sessions)
		m.mu.Unlock()
		if remaining != 0 {
			t.Errorf("after cleanupSession, subs+sessions = %d, want 0", remaining)
		}
	})
}

func TestRateLimiter_DropsOverBurst(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		allow, _ := talos.ParseNodeAllowlist(testNode)
		fc := &fakeCore{}
		st := cosistate.WrapCore(fc)
		fake := &fakeClient{
			state: &st,
			kinds: map[string]*meta.ResourceDefinition{
				"MachineStatus": makeResourceDef("MachineStatuses.runtime.talos.dev"),
			},
		}
		// 1/s: the burst, not the rate, bounds deliveries.
		m := NewManager(fake, allow, time.Second, 3)
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
		synctest.Wait()

		for i := 0; i < 10; i++ {
			fc.emit(cosistate.Event{Type: cosistate.Updated})
		}
		synctest.Wait()
		if got := count.Load(); got > 4 {
			t.Errorf("delivered %d events, want ≤ 4 (burst 3 + at most 1 from rate)", got)
		}
	})
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
	synctest.Test(t, func(t *testing.T) {
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
		if err := m.subscribe(context.Background(), "sess-A", testURIItem); err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		fc.emit(cosistate.Event{Type: cosistate.Updated})

		done := make(chan struct{})
		go func() {
			defer close(done)
			select {
			case fc.singleCh <- cosistate.Event{Type: cosistate.Updated}:
			default:
			}
		}()
		synctest.Wait()
		select {
		case <-done:
		default:
			t.Error("second emit hung; goroutine did not exit after nil notifier")
		}
	})
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
