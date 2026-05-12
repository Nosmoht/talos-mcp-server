package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"golang.org/x/time/rate"
)

func TestValidateHTTPConfig(t *testing.T) {
	tests := []struct {
		name    string
		addr    string
		token   string
		wantErr bool
	}{
		{"stdio mode, no addr", "", "", false},
		{"http mode with token", ":8080", "secret", false},
		{"http mode without token", ":8080", "", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateHTTPConfig(tc.addr, tc.token)
			if (err != nil) != tc.wantErr {
				t.Errorf("validateHTTPConfig(%q, %q) error = %v, wantErr %v", tc.addr, tc.token, err, tc.wantErr)
			}
		})
	}
}

func TestBuildTokenVerifier(t *testing.T) {
	const secret = "test-secret-token"
	verifier := buildTokenVerifier(secret)

	// correct token
	info, err := verifier(context.Background(), secret, nil)
	if err != nil {
		t.Fatalf("expected no error for correct token, got %v", err)
	}
	if info.Expiration.IsZero() {
		t.Error("expected non-zero expiration")
	}
	if info.Expiration.Before(time.Now()) {
		t.Error("expected future expiration")
	}

	// wrong token
	_, err = verifier(context.Background(), "wrong", nil)
	if err == nil {
		t.Error("expected error for wrong token")
	}
	if !errors.Is(err, auth.ErrInvalidToken) {
		t.Errorf("expected auth.ErrInvalidToken for wrong token, got %v", err)
	}

	// empty token
	_, err = verifier(context.Background(), "", nil)
	if err == nil {
		t.Error("expected error for empty token")
	}
	if !errors.Is(err, auth.ErrInvalidToken) {
		t.Errorf("expected auth.ErrInvalidToken for empty token, got %v", err)
	}
}

// buildTestHandler assembles the full HTTP mux used in production (including
// /healthz routing) against a minimal MCP server, for integration testing.
// The health probe always returns nil (healthy) so existing tests are unaffected.
// Options are constructed via newStreamableHTTPOptions, the same path runServer
// uses in production — keeps test fixtures from drifting away from production
// security defaults (see issue #179 + PR #177).
func buildTestHandler(secret string, hc httpTransportConfig) http.Handler {
	server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0"}, nil)
	mcpHandler := mcp.NewStreamableHTTPHandler(func(_ *http.Request) *mcp.Server {
		return server
	}, newStreamableHTTPOptions(slog.Default()))
	return buildHTTPMux(mcpHandler, secret, hc, func(_ context.Context) error { return nil })
}

func TestHTTPHandler_Integration(t *testing.T) {
	const secret = "integration-test-token"
	hc := newHTTPTransportConfig()
	ts := httptest.NewServer(buildTestHandler(secret, hc))
	defer ts.Close()

	// 401 without token (rate limiter passes it through, auth rejects).
	resp, err := http.Get(ts.URL) //nolint:noctx
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401 without token, got %d", resp.StatusCode)
	}

	// non-401 with correct token.
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, ts.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+secret)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		t.Errorf("expected non-401 with valid token, got %d", resp.StatusCode)
	}
}

func TestHTTPHandler_RateLimit_Integration(t *testing.T) {
	const secret = "rate-limit-test-token"
	// Tight rate limit: 1 req/s burst 1 so the second immediate request is rejected.
	hc := newHTTPTransportConfig()
	hc.limiter = rate.NewLimiter(rate.Limit(1), 1)
	ts := httptest.NewServer(buildTestHandler(secret, hc))
	defer ts.Close()

	get := func() int {
		req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, ts.URL, nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+secret)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		return resp.StatusCode
	}

	first := get()
	if first == http.StatusTooManyRequests {
		t.Fatalf("first request should not be rate-limited, got 429")
	}
	second := get()
	if second != http.StatusTooManyRequests {
		t.Fatalf("second immediate request should be rate-limited, got %d", second)
	}
}

func buildMuxForTest(probe func(context.Context) error) (*httptest.Server, func()) {
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0"}, nil)
	mcpHandler := mcp.NewStreamableHTTPHandler(func(_ *http.Request) *mcp.Server {
		return srv
	}, newStreamableHTTPOptions(slog.Default()))
	ts := httptest.NewServer(buildHTTPMux(mcpHandler, "secret", newHTTPTransportConfig(), probe))
	return ts, ts.Close
}

func TestHealthzEndpoint(t *testing.T) {
	t.Run("returns 200 when probe succeeds", func(t *testing.T) {
		ts, cleanup := buildMuxForTest(func(_ context.Context) error { return nil })
		defer cleanup()

		req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, ts.URL+"/healthz", nil)
		if err != nil {
			t.Fatal(err)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("expected 200 from healthy probe, got %d", resp.StatusCode)
		}
	})

	t.Run("returns 503 when probe fails", func(t *testing.T) {
		ts, cleanup := buildMuxForTest(func(_ context.Context) error { return errors.New("gRPC down") })
		defer cleanup()

		req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, ts.URL+"/healthz", nil)
		if err != nil {
			t.Fatal(err)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusServiceUnavailable {
			t.Errorf("expected 503 from failing probe, got %d", resp.StatusCode)
		}
	})

	t.Run("no auth required on /healthz", func(t *testing.T) {
		ts, cleanup := buildMuxForTest(func(_ context.Context) error { return nil })
		defer cleanup()

		// Deliberately omit Authorization header — /healthz must still return 200.
		req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, ts.URL+"/healthz", nil)
		if err != nil {
			t.Fatal(err)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("expected 200 without auth on /healthz, got %d", resp.StatusCode)
		}
	})

	t.Run("auth still required on non-healthz path", func(t *testing.T) {
		ts, cleanup := buildMuxForTest(func(_ context.Context) error { return nil })
		defer cleanup()

		req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, ts.URL+"/mcp", nil)
		if err != nil {
			t.Fatal(err)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("expected 401 without token on /mcp, got %d", resp.StatusCode)
		}
	})

	t.Run("method not allowed on /healthz for non-GET/HEAD", func(t *testing.T) {
		ts, cleanup := buildMuxForTest(func(_ context.Context) error { return nil })
		defer cleanup()

		for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
			req, err := http.NewRequestWithContext(context.Background(), method, ts.URL+"/healthz", nil)
			if err != nil {
				t.Fatal(err)
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			_ = resp.Body.Close()
			if resp.StatusCode != http.StatusMethodNotAllowed {
				t.Errorf("method %s: expected 405 on /healthz, got %d", method, resp.StatusCode)
			}
		}
	})
}

// TestHTTPHandler_CrossOriginProtection_Integration guards against the failure mode
// from PR #177: go-sdk silently dropping the CrossOriginProtection default. If the
// SDK's zero-value http.CrossOriginProtection stops denying cross-origin non-safe
// requests, this test fires. See issue #179.
//
// The test fixture goes through buildTestHandler → newStreamableHTTPOptions, the
// same code path runServer uses in production. A regression that removes
// CrossOriginProtection from the helper trips the assertions here; a regression
// that inlines a divergent literal back into runServer is not caught by this
// test (residual risk documented in #179's plan).
func TestHTTPHandler_CrossOriginProtection_Integration(t *testing.T) {
	const secret = "csrf-test-token"

	// Compile-time binding: the test depends on the shared helper. If
	// newStreamableHTTPOptions is renamed or removed, this stops compiling.
	_ = newStreamableHTTPOptions

	hc := newHTTPTransportConfig()
	ts := httptest.NewServer(buildTestHandler(secret, hc))
	defer ts.Close()

	// Statuses that indicate the request reached MCP-layer processing or a
	// legitimate non-CSRF rejection by earlier middleware. Anything outside
	// this set on a "not denied" expectation indicates the assertion may be
	// masking the cross-origin layer's behavior.
	mcpReachable := map[int]struct{}{
		http.StatusOK:                   {},
		http.StatusAccepted:             {},
		http.StatusNoContent:            {},
		http.StatusBadRequest:           {},
		http.StatusUnauthorized:         {},
		http.StatusMethodNotAllowed:     {},
		http.StatusNotAcceptable:        {},
		http.StatusUnsupportedMediaType: {},
		http.StatusUnprocessableEntity:  {},
		http.StatusNotImplemented:       {},
	}

	cases := []struct {
		name       string
		method     string
		headers    map[string]string
		wantDenied bool
	}{
		{
			name:       "POST cross-origin via Sec-Fetch-Site only",
			method:     http.MethodPost,
			headers:    map[string]string{"Sec-Fetch-Site": "cross-site"},
			wantDenied: true,
		},
		{
			name:       "POST cross-origin via Origin Host mismatch only",
			method:     http.MethodPost,
			headers:    map[string]string{"Origin": "https://evil.example"},
			wantDenied: true,
		},
		{
			name:       "POST cross-origin via both signals",
			method:     http.MethodPost,
			headers:    map[string]string{"Origin": "https://evil.example", "Sec-Fetch-Site": "cross-site"},
			wantDenied: true,
		},
		{
			name:       "PUT cross-origin via Sec-Fetch-Site",
			method:     http.MethodPut,
			headers:    map[string]string{"Sec-Fetch-Site": "cross-site"},
			wantDenied: true,
		},
		{
			name:       "POST same-origin no cross-origin headers",
			method:     http.MethodPost,
			headers:    map[string]string{},
			wantDenied: false,
		},
		{
			name:       "GET cross-origin safe method must not be denied",
			method:     http.MethodGet,
			headers:    map[string]string{"Sec-Fetch-Site": "cross-site", "Origin": "https://evil.example"},
			wantDenied: false,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req, err := http.NewRequestWithContext(context.Background(), c.method, ts.URL, strings.NewReader(`{}`))
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Authorization", "Bearer "+secret)
			req.Header.Set("Content-Type", "application/json")
			for k, v := range c.headers {
				req.Header.Set(k, v)
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()

			if c.wantDenied {
				if resp.StatusCode != http.StatusForbidden {
					t.Errorf("expected 403 (cross-origin denial), got %d", resp.StatusCode)
				}
				return
			}
			if resp.StatusCode == http.StatusForbidden {
				t.Errorf("got 403 from cross-origin layer; expected request to reach MCP-layer or earlier non-cross-origin rejection")
				return
			}
			if _, ok := mcpReachable[resp.StatusCode]; !ok {
				t.Errorf("got %d which is neither 403 nor a documented MCP-reachable status; assertion may be masking cross-origin behavior", resp.StatusCode)
			}
		})
	}
}
