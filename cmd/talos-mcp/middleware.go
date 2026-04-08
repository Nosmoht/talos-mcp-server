package main

import (
	"log"
	"math"
	"net/http"
	"os"
	"strconv"

	"golang.org/x/sync/semaphore"
	"golang.org/x/time/rate"
)

// httpTransportConfig holds HTTP-mode middleware parameters.
type httpTransportConfig struct {
	limiter *rate.Limiter
	sem     *semaphore.Weighted
	maxBody int64
}

// newHTTPTransportConfig reads HTTP middleware configuration from environment
// variables and returns a populated httpTransportConfig.
//
//   - TALOS_MCP_RATE_LIMIT  float64  requests/second  default 10
//   - TALOS_MCP_RATE_BURST  int      burst size        default 20
//   - TALOS_MCP_MAX_BODY_SIZE int64  bytes per POST    default 4194304 (4 MiB)
//   - TALOS_MCP_MAX_CONCURRENT int64 concurrent POSTs  default 20
func newHTTPTransportConfig() httpTransportConfig {
	rateLimit := envFloat64("TALOS_MCP_RATE_LIMIT", 10.0)
	rateBurst := envInt("TALOS_MCP_RATE_BURST", 20)
	maxBody := envInt64("TALOS_MCP_MAX_BODY_SIZE", 4*1024*1024)
	maxConc := envInt64("TALOS_MCP_MAX_CONCURRENT", 20)
	return httpTransportConfig{
		limiter: rate.NewLimiter(rate.Limit(rateLimit), rateBurst),
		sem:     semaphore.NewWeighted(maxConc),
		maxBody: maxBody,
	}
}

// RateLimit returns middleware that enforces a global token-bucket rate limit.
// Requests that exceed the burst capacity receive HTTP 429 Too Many Requests
// with a Retry-After header. All HTTP methods are subject to the limit.
func RateLimit(limiter *rate.Limiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !limiter.Allow() {
				w.Header().Set("Retry-After", "1")
				http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// LimitRequestBody returns middleware that caps POST request body size using
// http.MaxBytesReader. GET (SSE streams) and DELETE (session teardown) pass
// through unmodified because they carry no body.
func LimitRequestBody(maxBytes int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodPost {
				r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
			}
			next.ServeHTTP(w, r)
		})
	}
}

// LimitConcurrency returns middleware that limits the number of concurrent POST
// handlers via a weighted semaphore. GET (SSE) and DELETE (session teardown)
// pass through unmodified — SSE connections are long-lived and must not consume
// semaphore slots. Returns HTTP 503 immediately on overload (fail-fast /
// load-shedding per Google SRE guidance).
func LimitConcurrency(sem *semaphore.Weighted) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				next.ServeHTTP(w, r)
				return
			}
			if !sem.TryAcquire(1) {
				w.Header().Set("Retry-After", "1")
				http.Error(w, "server busy, try again later", http.StatusServiceUnavailable)
				return
			}
			defer sem.Release(1)
			next.ServeHTTP(w, r)
		})
	}
}

// envInt64 reads an integer environment variable, returning fallback on parse
// error or when the variable is unset.
func envInt64(key string, fallback int64) int64 {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		log.Printf("WARNING: invalid %s=%q (expected integer): %v; using default %d", key, v, err, fallback) //nolint:gosec // G706: key is a hardcoded constant; v is operator-supplied config, not user input
		return fallback
	}
	return n
}

// envInt reads an integer environment variable and safely converts it to int,
// returning fallback when the variable is unset, unparseable, or outside the
// valid int range (guards against CWE-190 on 32-bit platforms).
func envInt(key string, fallback int) int {
	n := envInt64(key, int64(fallback))
	if n < math.MinInt || n > math.MaxInt {
		log.Printf("WARNING: %s=%d out of int range [%d, %d]; using default %d", key, n, math.MinInt, math.MaxInt, fallback) //nolint:gosec // G706: key is a hardcoded constant; n is operator-supplied config, not user input
		return fallback
	}
	return int(n)
}

// envFloat64 reads a float environment variable, returning fallback on parse
// error or when the variable is unset.
func envFloat64(key string, fallback float64) float64 {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		log.Printf("WARNING: invalid %s=%q (expected float): %v; using default %g", key, v, err, fallback) //nolint:gosec // G706: key is a hardcoded constant; v is operator-supplied config, not user input
		return fallback
	}
	return f
}
