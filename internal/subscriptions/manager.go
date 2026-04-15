// Package subscriptions implements the MCP resources/subscribe handler for
// talos-mcp, backed by COSI Watch and WatchKindAggregated.
//
// The Manager owns one goroutine per (session, URI) subscription. Each
// goroutine is rooted in a Manager-scoped context so it survives the
// SubscribeRequest ctx (which ends the moment the JSON-RPC request returns)
// and is torn down either by an explicit Unsubscribe, by the per-session
// supervisor when the client disconnects, or by Shutdown signalling the
// root context when the server itself terminates.
//
// Bootstrapped events from COSI Watch are intentionally dropped: the MCP
// client is expected to call resources/read once after subscribe to obtain
// initial state. Forwarding Bootstrapped risks a race against the SDK's
// internal session-registration map (go-sdk mcp/server.go:892-909: the
// SubscribeHandler returns before the session is inserted into
// resourceSubscriptions, so a very-early ResourceUpdated call would be
// dropped silently by the SDK).
package subscriptions

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cosi-project/runtime/pkg/resource"
	cosistate "github.com/cosi-project/runtime/pkg/state"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"golang.org/x/time/rate"

	"github.com/Nosmoht/talos-mcp-server/internal/resources"
	"github.com/Nosmoht/talos-mcp-server/internal/talos"
)

// AllowedTypes is the set of canonical COSI resource types that clients may
// subscribe to. Extending this set is cheap (single-line change) but each
// new type should be reviewed for churn characteristics — NodeAddress on a
// DHCP cluster is already close to the per-URI rate ceiling.
var AllowedTypes = map[string]struct{}{
	"MachineStatuses.runtime.talos.dev": {},
	"Members.cluster.talos.dev":         {},
	"NodeAddresses.net.talos.dev":       {},
	"Services.v1alpha1.talos.dev":       {},
}

// notifyFunc is the injection seam that decouples the Manager from
// *mcp.Server in tests. Production binds it to Server.ResourceUpdated
// via BindServer.
type notifyFunc func(ctx context.Context, uri string) error

// Manager serializes subscribe/unsubscribe/supervise operations and owns the
// long-lived goroutines that fan COSI Watch events into MCP notifications.
type Manager struct {
	client       talos.ClientInterface
	allowedNodes *talos.NodeAllowlist
	rateEvery    time.Duration
	rateBurst    int
	notify       atomic.Pointer[notifyFunc]

	rootCtx    context.Context //nolint:containedctx // long-lived parent for every watcher; see package doc
	rootCancel context.CancelFunc

	mu       sync.Mutex
	subs     map[subKey]*subscription
	sessions map[string]map[string]struct{}
}

type subKey struct {
	sessionID string
	uri       string
}

type subscription struct {
	cancel  context.CancelFunc
	limiter *rate.Limiter
}

// NewManager builds a Manager rooted in a Background-derived context.
// Call BindServer after the MCP server is constructed; call Shutdown from
// main.go's cleanup path to drain all goroutines.
func NewManager(client talos.ClientInterface, allowedNodes *talos.NodeAllowlist, rateEvery time.Duration, rateBurst int) *Manager {
	root, cancel := context.WithCancel(context.Background())
	return &Manager{
		client:       client,
		allowedNodes: allowedNodes,
		rateEvery:    rateEvery,
		rateBurst:    rateBurst,
		rootCtx:      root,
		rootCancel:   cancel,
		subs:         make(map[subKey]*subscription),
		sessions:     make(map[string]map[string]struct{}),
	}
}

// BindServer installs the MCP server reference used to send
// resources/updated notifications. It must be called before any session
// can issue resources/subscribe. Because the SDK does not start sessions
// until Server.Run / StreamableHTTPHandler is called, wiring BindServer
// immediately after mcp.NewServer happens-before any subscribe call.
func (m *Manager) BindServer(s *mcp.Server) {
	fn := notifyFunc(func(ctx context.Context, uri string) error {
		return s.ResourceUpdated(ctx, &mcp.ResourceUpdatedNotificationParams{URI: uri})
	})
	m.notify.Store(&fn)
}

// Shutdown cancels the root context; every watcher goroutine observes the
// cancel on its next select and exits. Shutdown does not block on goroutine
// completion — the caller (main.go's deferred teardown) exits the process
// immediately after, and the Go runtime reaps remaining goroutines. Safe to
// call multiple times.
func (m *Manager) Shutdown() { m.rootCancel() }

// Subscribe implements ServerOptions.SubscribeHandler.
func (m *Manager) Subscribe(ctx context.Context, req *mcp.SubscribeRequest) error {
	return m.subscribe(ctx, req.Session.ID(), req.Params.URI)
}

// Unsubscribe implements ServerOptions.UnsubscribeHandler.
func (m *Manager) Unsubscribe(_ context.Context, req *mcp.UnsubscribeRequest) error {
	m.unsubscribe(req.Session.ID(), req.Params.URI)
	return nil
}

// SuperviseSession blocks on ss.Wait() and tears down every subscription
// owned by the session when it disconnects. One supervisor goroutine per
// session; installed from the wrapped InitializedHandler in main.go.
func (m *Manager) SuperviseSession(ss *mcp.ServerSession) {
	_ = ss.Wait()
	m.cleanupSession(ss.ID())
}

// subscribe is the testable core of Subscribe, taking the session ID as an
// explicit parameter so tests do not need a real *mcp.ServerSession.
func (m *Manager) subscribe(ctx context.Context, sessionID, uri string) error {
	node, ns, rawType, id, err := resources.ParseCOSIURI(uri)
	if err != nil {
		return fmt.Errorf("reject subscription: invalid URI %q: %w", uri, err)
	}
	if err := m.allowedNodes.CheckNode(node); err != nil {
		return fmt.Errorf("reject subscription: %w", err)
	}

	namespace := resource.Namespace(ns)
	rd, err := m.client.ResolveResourceKind(ctx, &namespace, rawType)
	if err != nil {
		return fmt.Errorf("reject subscription: resolve resource kind %q: %w", rawType, err)
	}
	canonical := rd.TypedSpec().Type
	if _, ok := AllowedTypes[canonical]; !ok {
		return fmt.Errorf("reject subscription: resource type %q is not subscribable", canonical)
	}

	key := subKey{sessionID: sessionID, uri: uri}

	m.mu.Lock()
	if _, dup := m.subs[key]; dup {
		m.mu.Unlock()
		slog.Debug("duplicate subscribe ignored", "session", sessionID, "uri", uri)
		return nil
	}

	watchCtx, cancel := context.WithCancel(m.rootCtx) //nolint:gosec // G118: cancel is stored on sub and invoked by unsubscribe/cleanupSession/Shutdown
	sub := &subscription{
		cancel:  cancel,
		limiter: rate.NewLimiter(rate.Every(m.rateEvery), m.rateBurst),
	}
	m.subs[key] = sub
	if m.sessions[sessionID] == nil {
		m.sessions[sessionID] = make(map[string]struct{})
	}
	m.sessions[sessionID][uri] = struct{}{}
	m.mu.Unlock()

	slog.Info("subscription opened", "session", sessionID, "uri", uri, "type", canonical)
	go m.runWatch(watchCtx, key, namespace, canonical, id, sub.limiter)
	return nil
}

// unsubscribe cancels and removes the (sessionID, uri) subscription if present.
func (m *Manager) unsubscribe(sessionID, uri string) {
	key := subKey{sessionID: sessionID, uri: uri}
	m.mu.Lock()
	sub, ok := m.subs[key]
	if ok {
		delete(m.subs, key)
		if uris := m.sessions[sessionID]; uris != nil {
			delete(uris, uri)
			if len(uris) == 0 {
				delete(m.sessions, sessionID)
			}
		}
	}
	m.mu.Unlock()
	if ok {
		sub.cancel()
		slog.Info("subscription closed", "session", sessionID, "uri", uri)
	}
}

// cleanupSession tears down every subscription owned by the session.
// Cancels are collected under the mutex then invoked after the mutex is
// released to avoid serializing the cancel chain behind the manager lock.
func (m *Manager) cleanupSession(sessionID string) {
	m.mu.Lock()
	uris := m.sessions[sessionID]
	cancels := make([]context.CancelFunc, 0, len(uris))
	for uri := range uris {
		if sub, ok := m.subs[subKey{sessionID: sessionID, uri: uri}]; ok {
			cancels = append(cancels, sub.cancel)
			delete(m.subs, subKey{sessionID: sessionID, uri: uri})
		}
	}
	delete(m.sessions, sessionID)
	m.mu.Unlock()
	for _, c := range cancels {
		c()
	}
	if len(cancels) > 0 {
		slog.Info("session subscriptions torn down", "session", sessionID, "count", len(cancels))
	}
}

// runWatch is the per-subscription goroutine. resourceID == "" selects the
// list flavour via WatchKindAggregated; a non-empty ID selects single-resource
// Watch. Channel buffers (32 for single, 8 for aggregated batches) prevent
// the COSI publisher from blocking on downstream stalls.
func (m *Manager) runWatch(ctx context.Context, key subKey, ns resource.Namespace, canonicalType, resourceID string, limiter *rate.Limiter) {
	st := m.client.COSIState()

	if resourceID == "" {
		chAgg := make(chan []cosistate.Event, 8)
		kind := resource.NewMetadata(ns, canonicalType, "", resource.VersionUndefined)
		if err := st.WatchKindAggregated(ctx, kind, chAgg); err != nil {
			if !errors.Is(err, context.Canceled) {
				slog.Warn("watch kind start failed", "uri", key.uri, "error", err)
			}
			return
		}
		for {
			select {
			case <-ctx.Done():
				return
			case batch := <-chAgg:
				for _, ev := range batch {
					if !m.forwardEvent(ctx, key.uri, ev, limiter) {
						return
					}
				}
			}
		}
	}

	ch := make(chan cosistate.Event, 32)
	ptr := resource.NewMetadata(ns, canonicalType, resourceID, resource.VersionUndefined)
	if err := st.Watch(ctx, ptr, ch); err != nil {
		if !errors.Is(err, context.Canceled) {
			slog.Warn("watch start failed", "uri", key.uri, "error", err)
		}
		return
	}
	for {
		select {
		case <-ctx.Done():
			return
		case ev := <-ch:
			if !m.forwardEvent(ctx, key.uri, ev, limiter) {
				return
			}
		}
	}
}

// forwardEvent classifies a single COSI event and (optionally) delivers an
// MCP notification. Returns false when the goroutine should exit (Errored).
func (m *Manager) forwardEvent(ctx context.Context, uri string, ev cosistate.Event, limiter *rate.Limiter) bool {
	switch ev.Type {
	case cosistate.Bootstrapped:
		// Drop — initial state belongs to resources/read, and forwarding here
		// risks the SDK register-subscriber race described in the package doc.
		return true
	case cosistate.Errored:
		slog.Warn("watch error", "uri", uri, "error", ev.Error)
		return false
	case cosistate.Created, cosistate.Updated, cosistate.Destroyed, cosistate.Noop:
		if ev.Type == cosistate.Noop {
			return true // Noop is a no-change marker; clients already have the current state.
		}
		if !limiter.Allow() {
			slog.Debug("notification dropped by rate limiter", "uri", uri)
			return true
		}
		fn := m.notify.Load()
		if fn == nil {
			slog.Warn("ResourceUpdated skipped: server not bound", "uri", uri)
			return false
		}
		if err := (*fn)(ctx, uri); err != nil {
			slog.Warn("ResourceUpdated failed", "uri", uri, "error", err)
		}
	}
	return true
}
