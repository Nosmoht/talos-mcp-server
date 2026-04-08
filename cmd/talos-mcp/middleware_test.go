package main

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/sync/semaphore"
	"golang.org/x/time/rate"
)

func TestRateLimit(t *testing.T) {
	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := RateLimit(rate.NewLimiter(1, 1))(ok)

	// First request: burst of 1 is available.
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("first request: got %d, want 200", rr.Code)
	}

	// Second request (burst exhausted): rejected.
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusTooManyRequests {
		t.Fatalf("second request: got %d, want 429", rr.Code)
	}
	if rr.Header().Get("Retry-After") == "" {
		t.Error("second request: missing Retry-After header")
	}
}

func TestLimitRequestBody(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := io.ReadAll(r.Body); err != nil {
			var maxErr *http.MaxBytesError
			if errors.As(err, &maxErr) {
				http.Error(w, "too large", http.StatusRequestEntityTooLarge)
				return
			}
			http.Error(w, "read error", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	handler := LimitRequestBody(10)(inner)

	// POST within limit.
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("short"))
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("small POST: got %d, want 200", rr.Code)
	}

	// POST exceeding limit: inner handler detects MaxBytesError.
	req = httptest.NewRequest(http.MethodPost, "/", strings.NewReader("this body is definitely longer than ten bytes"))
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("large POST: got %d, want 413", rr.Code)
	}

	// GET passes through unmodified.
	req = httptest.NewRequest(http.MethodGet, "/", nil)
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("GET: got %d, want 200", rr.Code)
	}
}

func TestLimitConcurrency(t *testing.T) {
	block := make(chan struct{})
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		<-block
		w.WriteHeader(http.StatusOK)
	})
	handler := LimitConcurrency(semaphore.NewWeighted(1))(inner)

	// Start a POST that blocks inside the inner handler.
	req1 := httptest.NewRequest(http.MethodPost, "/", nil)
	rr1 := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		handler.ServeHTTP(rr1, req1)
		close(done)
	}()
	// Give the goroutine time to acquire the semaphore.
	time.Sleep(10 * time.Millisecond)

	// Second POST while first is in-flight: expect 503.
	req2 := httptest.NewRequest(http.MethodPost, "/", nil)
	rr2 := httptest.NewRecorder()
	handler.ServeHTTP(rr2, req2)
	if rr2.Code != http.StatusServiceUnavailable {
		t.Fatalf("concurrent POST: got %d, want 503", rr2.Code)
	}
	if rr2.Header().Get("Retry-After") == "" {
		t.Error("concurrent POST: missing Retry-After header")
	}

	// Unblock the first POST and verify it completed successfully.
	close(block)
	<-done
	if rr1.Code != http.StatusOK {
		t.Fatalf("first POST (after unblock): got %d, want 200", rr1.Code)
	}

	// GET bypasses the semaphore entirely.
	nonBlock := LimitConcurrency(semaphore.NewWeighted(1))(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req3 := httptest.NewRequest(http.MethodGet, "/", nil)
	rr3 := httptest.NewRecorder()
	nonBlock.ServeHTTP(rr3, req3)
	if rr3.Code != http.StatusOK {
		t.Fatalf("GET: got %d, want 200", rr3.Code)
	}
}
