package main

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const httpRegressionToken = "regression-test-bearer-token"

// connectLegacyClient connects an SDK client pinned to the initialize-based
// revision and fails the test if the session negotiates anything else.
func connectLegacyClient(t *testing.T, transport mcp.Transport, acID string) *mcp.ClientSession {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), compatIOTimeout)
	defer cancel()
	client := mcp.NewClient(&mcp.Implementation{Name: "regression-legacy-client", Version: "0.0.0"}, nil)
	session, err := client.Connect(ctx, transport, &mcp.ClientSessionOptions{ProtocolVersion: compatRevisionLegacy})
	if err != nil {
		t.Fatalf("%s precondition: legacy client connect: %v", acID, err)
	}
	got := ""
	if res := session.InitializeResult(); res != nil {
		got = res.ProtocolVersion
	}
	if got != compatRevisionLegacy {
		_ = session.Close()
		t.Fatalf("%s precondition: negotiated protocol version expected=%s got=%q", acID, compatRevisionLegacy, got)
	}
	return session
}

// bearerRoundTripper adds the bearer token the production HTTP mux requires.
type bearerRoundTripper struct {
	token string
	next  http.RoundTripper
}

func (b bearerRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.Header.Set("Authorization", "Bearer "+b.token)
	return b.next.RoundTrip(req)
}

// Predicate (AC-1): a client pinned to revision 2025-11-25 on the stdio
// transport class completes the initialize handshake and is served the tool set.
func TestGivenLegacyStdioClient_WhenListingTools_ThenToolSetIsServed(t *testing.T) {
	c2s, s2c := serveOverPipes(t, newCompatServer(t, newFakeBackend(t)))
	session := connectLegacyClient(t, &mcp.IOTransport{Reader: s2c, Writer: c2s}, "AC-1")
	t.Cleanup(func() { _ = session.Close() })

	names := listToolNames(t, session)
	if !slices.Contains(names, compatToolName) {
		t.Errorf("AC-1: tools/list expected to contain %s got %v", compatToolName, names)
	}
}

// Predicate (AC-3): when a 2025-11-25 client holding a resources/subscribe
// subscription drops its stream without a graceful close, the server-side COSI
// watch for that subscription ends within the teardown bound.
func TestGivenLegacyStdioSubscription_WhenClientStreamDropsWithoutClose_ThenWatchEnds(t *testing.T) {
	backend := newFakeBackend(t)
	c2s, s2c := serveOverPipes(t, newCompatServer(t, backend))
	session := connectLegacyClient(t, &mcp.IOTransport{Reader: s2c, Writer: c2s}, "AC-3")

	subCtx, cancelSub := context.WithTimeout(context.Background(), compatIOTimeout)
	defer cancelSub()
	if err := session.Subscribe(subCtx, &mcp.SubscribeParams{URI: compatSubscribeURI}); err != nil {
		t.Fatalf("AC-3 precondition: subscribe to %s failed: %v", compatSubscribeURI, err)
	}

	select {
	case <-backend.watchStarted:
	case <-time.After(compatTeardownBound):
		t.Fatalf("AC-3 precondition: server opened no COSI watch for %s within %s", compatSubscribeURI, compatTeardownBound)
	}

	select {
	case <-backend.watchEnded:
		t.Fatalf("AC-3 precondition: expected the COSI watch to be open before the client stream drops, got already ended")
	default:
	}

	_ = c2s.Close()
	_ = s2c.Close()

	select {
	case <-backend.watchEnded:
	case <-time.After(compatTeardownBound):
		t.Errorf("AC-3: expected the COSI watch context to be done within %s after the client stream dropped, got still open", compatTeardownBound)
	}
}

// Predicate (AC-6): the production stateful streamable-HTTP mux, with a valid
// bearer token, serves the tool set to a client pinned to revision 2025-11-25.
func TestGivenStatefulHTTPWithToken_WhenLegacyClientListsTools_ThenToolSetIsServed(t *testing.T) {
	server := newCompatServer(t, newFakeBackend(t))
	mux := buildHTTPMux(newProtectedMCPHandler(server, slog.Default()), httpRegressionToken, newHTTPTransportConfig(),
		func(context.Context) error { return nil })
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)

	transport := &mcp.StreamableClientTransport{
		Endpoint:   ts.URL,
		HTTPClient: &http.Client{Transport: bearerRoundTripper{token: httpRegressionToken, next: http.DefaultTransport}},
	}
	session := connectLegacyClient(t, transport, "AC-6")
	t.Cleanup(func() { _ = session.Close() })

	names := listToolNames(t, session)
	if !slices.Contains(names, compatToolName) {
		t.Errorf("AC-6: tools/list expected to contain %s got %v", compatToolName, names)
	}
}
