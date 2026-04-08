package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
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

// buildTestHandler assembles the full middleware chain used in production,
// against a minimal MCP server, for integration testing.
func buildTestHandler(secret string, hc httpTransportConfig) http.Handler {
	server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0"}, nil)
	mcpHandler := mcp.NewStreamableHTTPHandler(func(_ *http.Request) *mcp.Server {
		return server
	}, &mcp.StreamableHTTPOptions{DisableLocalhostProtection: true})
	verifier := buildTokenVerifier(secret)
	handler := LimitConcurrency(hc.sem)(mcpHandler)
	handler = auth.RequireBearerToken(verifier, nil)(handler)
	handler = LimitRequestBody(hc.maxBody)(handler)
	handler = RateLimit(hc.limiter)(handler)
	return handler
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
