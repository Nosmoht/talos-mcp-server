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

func TestHTTPHandler_Integration(t *testing.T) {
	// Build a minimal MCP server (no tools needed for auth test).
	server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0"}, nil)

	const secret = "integration-test-token"

	mcpHandler := mcp.NewStreamableHTTPHandler(func(_ *http.Request) *mcp.Server {
		return server
	}, &mcp.StreamableHTTPOptions{DisableLocalhostProtection: true})

	verifier := buildTokenVerifier(secret)
	authedHandler := auth.RequireBearerToken(verifier, nil)(mcpHandler)

	ts := httptest.NewServer(authedHandler)
	defer ts.Close()

	// 401 without token
	resp, err := http.Get(ts.URL) //nolint:noctx
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401 without token, got %d", resp.StatusCode)
	}

	// non-401 with correct token
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
