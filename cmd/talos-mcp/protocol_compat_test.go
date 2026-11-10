package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cosi-project/runtime/pkg/resource"
	"github.com/cosi-project/runtime/pkg/resource/meta"
	"github.com/cosi-project/runtime/pkg/resource/meta/spec"
	cosistate "github.com/cosi-project/runtime/pkg/state"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	"google.golang.org/grpc"

	"github.com/Nosmoht/talos-mcp-server/internal/tools"
)

// Capabilities are read as JSON keys because go-sdk v1.8.0 deprecates
// ServerCapabilities.Logging.

const (
	compatRevisionLegacy  = "2025-11-25"
	compatRevisionCurrent = "2026-07-28"
	compatSubscribeURI    = "talos://192.0.2.10/resource/runtime/MachineStatus"
	compatToolName        = "talos_version"
	compatTeardownBound   = 5 * time.Second
	compatIOTimeout       = 10 * time.Second
)

// fakeBackend is the Talos API the server talks to. Only the methods below are
// reached by these tests; the embedded nil tools.Client panics on anything else.
type fakeBackend struct {
	tools.Client

	watchStarted chan struct{}
	watchEnded   chan struct{}
	startOnce    sync.Once
	endOnce      sync.Once
}

func newFakeBackend(t *testing.T) *fakeBackend {
	t.Helper()
	return &fakeBackend{
		watchStarted: make(chan struct{}),
		watchEnded:   make(chan struct{}),
	}
}

func (b *fakeBackend) Version(_ context.Context, _ ...grpc.CallOption) (*machineapi.VersionResponse, error) {
	return nil, errors.New("fake backend: Version is unavailable")
}

func (b *fakeBackend) ResolveResourceKind(_ context.Context, _ *resource.Namespace, rawType resource.Type) (*meta.ResourceDefinition, error) {
	if rawType != "MachineStatus" && rawType != "MachineStatuses.runtime.talos.dev" {
		return nil, fmt.Errorf("fake backend: unknown resource type %q", rawType)
	}
	return meta.NewResourceDefinition(spec.ResourceDefinitionSpec{
		Type:             "MachineStatuses.runtime.talos.dev",
		DefaultNamespace: "runtime",
	})
}

func (b *fakeBackend) COSIState() cosistate.State {
	return cosistate.WrapCore(&fakeWatchCore{backend: b})
}

// observeWatch marks the watch as started and closes watchEnded only once the
// context the server passed to the watch is done; a watch call that merely
// returns does not count as ended.
func (b *fakeBackend) observeWatch(ctx context.Context) {
	b.startOnce.Do(func() { close(b.watchStarted) })
	go func() {
		<-ctx.Done()
		b.endOnce.Do(func() { close(b.watchEnded) })
	}()
}

type fakeWatchCore struct {
	backend *fakeBackend
}

func (c *fakeWatchCore) Get(_ context.Context, _ resource.Pointer, _ ...cosistate.GetOption) (resource.Resource, error) {
	return nil, errors.New("fake core: Get is not supported")
}

func (c *fakeWatchCore) List(_ context.Context, _ resource.Kind, _ ...cosistate.ListOption) (resource.List, error) {
	return resource.List{}, errors.New("fake core: List is not supported")
}

func (c *fakeWatchCore) Create(_ context.Context, _ resource.Resource, _ ...cosistate.CreateOption) error {
	return errors.New("fake core: Create is not supported")
}

func (c *fakeWatchCore) Update(_ context.Context, _ resource.Resource, _ ...cosistate.UpdateOption) error {
	return errors.New("fake core: Update is not supported")
}

func (c *fakeWatchCore) Destroy(_ context.Context, _ resource.Pointer, _ ...cosistate.DestroyOption) error {
	return errors.New("fake core: Destroy is not supported")
}

func (c *fakeWatchCore) Watch(ctx context.Context, _ resource.Pointer, _ chan<- cosistate.Event, _ ...cosistate.WatchOption) error {
	c.backend.observeWatch(ctx)
	return nil
}

func (c *fakeWatchCore) WatchKind(_ context.Context, _ resource.Kind, _ chan<- cosistate.Event, _ ...cosistate.WatchKindOption) error {
	return errors.New("fake core: WatchKind is not supported")
}

func (c *fakeWatchCore) WatchKindAggregated(ctx context.Context, _ resource.Kind, _ chan<- []cosistate.Event, _ ...cosistate.WatchKindOption) error {
	c.backend.observeWatch(ctx)
	return nil
}

func newCompatServer(t *testing.T, backend *fakeBackend) *mcp.Server {
	t.Helper()
	server, subMgr := newServer(&tools.Handlers{Client: backend}, serverConfig{
		SubscriptionRate:  time.Microsecond,
		SubscriptionBurst: 1,
	})
	t.Cleanup(subMgr.Shutdown)
	return server
}

// serveOverPipes runs server on the stdio transport class (newline-delimited
// JSON-RPC over a byte stream) and returns the client ends of both pipes.
// Closing both models a client process that dies.
func serveOverPipes(t *testing.T, server *mcp.Server) (c2s *io.PipeWriter, s2c *io.PipeReader) {
	t.Helper()
	c2sReader, c2sWriter := io.Pipe()
	s2cReader, s2cWriter := io.Pipe()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = server.Run(ctx, &mcp.IOTransport{Reader: c2sReader, Writer: s2cWriter})
	}()

	t.Cleanup(func() {
		cancel()
		_ = c2sWriter.Close()
		_ = s2cReader.Close()
		select {
		case <-done:
		case <-time.After(compatTeardownBound):
		}
	})
	return c2sWriter, s2cReader
}

func connectUnpinnedClient(t *testing.T, c2s *io.PipeWriter, s2c *io.PipeReader) *mcp.ClientSession {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), compatIOTimeout)
	defer cancel()
	client := mcp.NewClient(&mcp.Implementation{Name: "compat-client", Version: "0.0.0"}, nil)
	session, err := client.Connect(ctx, &mcp.IOTransport{Reader: s2c, Writer: c2s}, nil)
	if err != nil {
		t.Fatalf("setup: unpinned client connect: %v", err)
	}
	return session
}

func requireNegotiatedCurrentRevision(t *testing.T, session *mcp.ClientSession, acID string) {
	t.Helper()
	got := ""
	if res := session.InitializeResult(); res != nil {
		got = res.ProtocolVersion
	}
	if got != compatRevisionCurrent {
		t.Fatalf("%s: negotiated protocol version expected=%s got=%q", acID, compatRevisionCurrent, got)
	}
}

func listToolNames(t *testing.T, session *mcp.ClientSession) []string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), compatIOTimeout)
	defer cancel()
	var names []string
	params := &mcp.ListToolsParams{}
	for {
		res, err := session.ListTools(ctx, params)
		if err != nil {
			t.Fatalf("tools/list: %v", err)
		}
		for _, tool := range res.Tools {
			names = append(names, tool.Name)
		}
		if res.NextCursor == "" {
			return names
		}
		params = &mcp.ListToolsParams{Cursor: res.NextCursor}
	}
}

type wireError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type wireFrame struct {
	ID     json.RawMessage `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *wireError      `json:"error,omitempty"`

	raw string
}

// legacyWireClient is a newline-delimited JSON-RPC client that speaks the
// initialize-based protocol and records every frame it reads, including the
// frames it skips while waiting for a response.
type legacyWireClient struct {
	t      *testing.T
	out    io.Writer
	frames chan wireFrame
	stop   chan struct{}
	nextID int

	mu   sync.Mutex
	seen []wireFrame

	initResult json.RawMessage
}

func rawLegacySession(t *testing.T, c2s *io.PipeWriter, s2c *io.PipeReader) *legacyWireClient {
	t.Helper()
	w := &legacyWireClient{
		t:      t,
		out:    c2s,
		frames: make(chan wireFrame, 256),
		stop:   make(chan struct{}),
	}
	t.Cleanup(func() { close(w.stop) })
	go w.pump(s2c)

	result, rpcErr := w.call("initialize", map[string]any{
		"protocolVersion": compatRevisionLegacy,
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "compat-legacy-client", "version": "0.0.0"},
	})
	if rpcErr != nil {
		t.Fatalf("setup: legacy initialize failed: code=%d message=%q", rpcErr.Code, rpcErr.Message)
	}
	w.initResult = result
	return w
}

func (w *legacyWireClient) pump(r io.Reader) {
	defer close(w.frames)
	dec := json.NewDecoder(r)
	for {
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return
		}
		var f wireFrame
		_ = json.Unmarshal(raw, &f)
		f.raw = string(raw)
		w.mu.Lock()
		w.seen = append(w.seen, f)
		w.mu.Unlock()
		select {
		case w.frames <- f:
		case <-w.stop:
			return
		}
	}
}

func (w *legacyWireClient) send(msg map[string]any) {
	w.t.Helper()
	b, err := json.Marshal(msg)
	if err != nil {
		w.t.Fatalf("legacy client: encode %v: %v", msg["method"], err)
	}
	b = append(b, '\n')
	written := make(chan error, 1)
	go func() {
		_, werr := w.out.Write(b)
		written <- werr
	}()
	select {
	case err := <-written:
		if err != nil {
			w.t.Fatalf("legacy client: write %v: %v", msg["method"], err)
		}
	case <-time.After(compatIOTimeout):
		w.t.Fatalf("legacy client: write %v timed out after %s", msg["method"], compatIOTimeout)
	}
}

func (w *legacyWireClient) notify(method string, params any) {
	w.t.Helper()
	w.send(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
}

func (w *legacyWireClient) call(method string, params any) (json.RawMessage, *wireError) {
	w.t.Helper()
	w.nextID++
	id := w.nextID
	w.send(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})

	want := strconv.Itoa(id)
	deadline := time.After(compatIOTimeout)
	for {
		select {
		case f, ok := <-w.frames:
			if !ok {
				w.t.Fatalf("legacy client: stream closed before the %s response", method)
			}
			if f.Method != "" || string(f.ID) != want {
				continue
			}
			return f.Result, f.Error
		case <-deadline:
			w.t.Fatalf("legacy client: no %s response within %s", method, compatIOTimeout)
		}
	}
}

// closeAndDrain closes the client's write end, reads what the server still
// sends until EOF or a quiet bound, and returns every frame read in the session.
func (w *legacyWireClient) closeAndDrain(c2s *io.PipeWriter) []wireFrame {
	w.t.Helper()
	_ = c2s.Close()
	deadline := time.After(compatTeardownBound)
	for drained := false; !drained; {
		select {
		case _, ok := <-w.frames:
			drained = !ok
		case <-deadline:
			drained = true
		}
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return slices.Clone(w.seen)
}

func legacyCapabilities(t *testing.T) map[string]json.RawMessage {
	t.Helper()
	c2s, s2c := serveOverPipes(t, newCompatServer(t, newFakeBackend(t)))
	session := rawLegacySession(t, c2s, s2c)
	var res struct {
		ProtocolVersion string                     `json:"protocolVersion"`
		Capabilities    map[string]json.RawMessage `json:"capabilities"`
	}
	if err := json.Unmarshal(session.initResult, &res); err != nil {
		t.Fatalf("setup: decode legacy initialize result: %v", err)
	}
	if res.ProtocolVersion != compatRevisionLegacy {
		t.Fatalf("setup: legacy initialize negotiated %q, want %s", res.ProtocolVersion, compatRevisionLegacy)
	}
	return res.Capabilities
}

func currentRevisionCapabilities(t *testing.T) map[string]json.RawMessage {
	t.Helper()
	c2s, s2c := serveOverPipes(t, newCompatServer(t, newFakeBackend(t)))
	session := connectUnpinnedClient(t, c2s, s2c)
	t.Cleanup(func() { _ = session.Close() })
	requireNegotiatedCurrentRevision(t, session, "AC-16/AC-17 precondition")
	encoded, err := json.Marshal(session.InitializeResult().Capabilities)
	if err != nil {
		t.Fatalf("setup: encode advertised capabilities: %v", err)
	}
	var caps map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &caps); err != nil {
		t.Fatalf("setup: decode advertised capabilities: %v", err)
	}
	return caps
}

type capabilityCase struct {
	id           string
	capabilities func(t *testing.T) map[string]json.RawMessage
}

func capabilityCases() []capabilityCase {
	return []capabilityCase{
		{id: "legacy_2025_11_25_initialize", capabilities: legacyCapabilities},
		{id: "revision_2026_07_28_discover", capabilities: currentRevisionCapabilities},
	}
}

func capabilityKeys(caps map[string]json.RawMessage) string {
	keys := make([]string, 0, len(caps))
	for k := range caps {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return strings.Join(keys, ",")
}

// Predicate (AC-2): an unpinned client on the stdio transport class negotiates
// revision 2026-07-28 and is served the tool set.
func TestGivenRevision20260728StdioClient_WhenListingTools_ThenToolSetIsServed(t *testing.T) {
	c2s, s2c := serveOverPipes(t, newCompatServer(t, newFakeBackend(t)))
	session := connectUnpinnedClient(t, c2s, s2c)
	t.Cleanup(func() { _ = session.Close() })

	requireNegotiatedCurrentRevision(t, session, "AC-2")

	names := listToolNames(t, session)
	if !slices.Contains(names, compatToolName) {
		t.Errorf("AC-2: tools/list expected to contain %s got %v", compatToolName, names)
	}
}

// Predicate (AC-4): when a 2026-07-28 client holding a subscriptions/listen
// subscription drops its stream without a graceful close, the server-side COSI
// watch for that subscription ends within the teardown bound.
func TestGivenRevision20260728StdioSubscription_WhenClientStreamDropsWithoutClose_ThenWatchEnds(t *testing.T) {
	backend := newFakeBackend(t)
	c2s, s2c := serveOverPipes(t, newCompatServer(t, backend))
	session := connectUnpinnedClient(t, c2s, s2c)

	requireNegotiatedCurrentRevision(t, session, "AC-4")

	subCtx, cancelSub := context.WithCancel(context.Background())
	t.Cleanup(cancelSub)
	subErr := make(chan error, 1)
	go func() {
		subErr <- session.Subscribe(subCtx, &mcp.SubscribeParams{URI: compatSubscribeURI})
	}()

	startDeadline := time.After(compatTeardownBound)
	for started := false; !started; {
		select {
		case <-backend.watchStarted:
			started = true
		case err := <-subErr:
			if err != nil {
				t.Fatalf("AC-4 precondition: subscribe to %s failed: %v", compatSubscribeURI, err)
			}
			subErr = nil
		case <-startDeadline:
			t.Fatalf("AC-4 precondition: server opened no COSI watch for %s within %s", compatSubscribeURI, compatTeardownBound)
		}
	}

	select {
	case <-backend.watchEnded:
		t.Fatalf("AC-4 precondition: expected the COSI watch to be open before the client stream drops, got already ended")
	default:
	}

	_ = c2s.Close()
	_ = s2c.Close()

	select {
	case <-backend.watchEnded:
	case <-time.After(compatTeardownBound):
		t.Errorf("AC-4: expected the COSI watch context to be done within %s after the client stream dropped, got still open", compatTeardownBound)
	}
}

// Predicate (AC-5): a legacy client that set the log level to debug and called a
// tool that writes an audit record receives no notifications/message frame at
// any point of the session.
func TestGivenLegacyStdioSessionAtDebugLevel_WhenToolIsCalled_ThenNoLogNotificationIsSent(t *testing.T) {
	c2s, s2c := serveOverPipes(t, newCompatServer(t, newFakeBackend(t)))
	session := rawLegacySession(t, c2s, s2c)

	session.notify("notifications/initialized", map[string]any{})
	if _, rpcErr := session.call("logging/setLevel", map[string]any{"level": "debug"}); rpcErr != nil {
		t.Fatalf("AC-5 precondition: logging/setLevel failed: code=%d message=%q", rpcErr.Code, rpcErr.Message)
	}
	// The tool's backend call fails by design; the audit record is written before it.
	_, _ = session.call("tools/call", map[string]any{"name": compatToolName, "arguments": map[string]any{}})

	var logFrames []string
	for _, f := range session.closeAndDrain(c2s) {
		if f.Method == "notifications/message" {
			logFrames = append(logFrames, f.raw)
		}
	}
	if len(logFrames) != 0 {
		t.Errorf("AC-5: expected zero notifications/message frames in the session, got %d: %s", len(logFrames), strings.Join(logFrames, " | "))
	}
}

// Predicate (AC-16): no session, in either protocol generation, is advertised
// the logging server capability.
func TestGivenEitherProtocolGeneration_WhenSessionStarts_ThenNoLoggingCapabilityIsAdvertised(t *testing.T) {
	for _, tc := range capabilityCases() {
		t.Run(tc.id, func(t *testing.T) {
			caps := tc.capabilities(t)
			if _, present := caps["logging"]; present {
				t.Errorf("AC-16: case=%s expected=no logging capability got=[%s]", tc.id, capabilityKeys(caps))
			}
		})
	}
}

// Predicate (AC-17): every session, in either protocol generation, is advertised
// the tools capability and the resources capability with subscribe enabled.
func TestGivenEitherProtocolGeneration_WhenSessionStarts_ThenToolsAndResourceSubscribeCapabilitiesAreAdvertised(t *testing.T) {
	for _, tc := range capabilityCases() {
		t.Run(tc.id, func(t *testing.T) {
			caps := tc.capabilities(t)
			if _, present := caps["tools"]; !present {
				t.Errorf("AC-17: case=%s expected=tools capability got=[%s]", tc.id, capabilityKeys(caps))
			}
			var res struct {
				Subscribe bool `json:"subscribe"`
			}
			raw, present := caps["resources"]
			if !present || json.Unmarshal(raw, &res) != nil || !res.Subscribe {
				t.Errorf("AC-17: case=%s expected=resources.subscribe=true got=resources:%s", tc.id, string(raw))
			}
		})
	}
}
