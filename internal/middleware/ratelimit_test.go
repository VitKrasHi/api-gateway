package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"api-gateway/internal/transport"
)

// fakeLimiter — детерминированный мок.
type fakeLimiter struct {
	allowed    bool
	remaining  int64
	retryAfter time.Duration
	err        error
	lastKey    string
}

func (f *fakeLimiter) Allow(_ context.Context, key string) (bool, int64, time.Duration, error) {
	f.lastKey = key
	return f.allowed, f.remaining, f.retryAfter, f.err
}
func (f *fakeLimiter) Close() error { return nil }

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

func TestRateLimit_Allowed(t *testing.T) {
	fl := &fakeLimiter{allowed: true, remaining: 42}
	h := RateLimit(fl)(okHandler())

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	h.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, "42", rr.Header().Get("X-RateLimit-Remaining"))
}

func TestRateLimit_Blocked(t *testing.T) {
	fl := &fakeLimiter{allowed: false, retryAfter: 7 * time.Second}
	h := RateLimit(fl)(okHandler())

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	h.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusTooManyRequests, rr.Code)
	assert.Equal(t, "8", rr.Header().Get("Retry-After"))
	assert.Contains(t, rr.Body.String(), "rate_limited")
}

func TestRateLimit_FailOpen(t *testing.T) {
	fl := &fakeLimiter{err: errors.New("redis down")}
	h := RateLimit(fl)(okHandler())

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/", nil))

	// Redis недоступен → пропускаем (fail-open).
	assert.Equal(t, http.StatusOK, rr.Code)
}

func TestRateLimit_KeyByUser(t *testing.T) {
	fl := &fakeLimiter{allowed: true}
	h := RateLimit(fl)(okHandler())

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req = req.WithContext(transport.WithUser(req.Context(), "user-99", nil))
	h.ServeHTTP(httptest.NewRecorder(), req)

	assert.Equal(t, "u:user-99", fl.lastKey)
}

func TestRateLimit_KeyByIP(t *testing.T) {
	fl := &fakeLimiter{allowed: true}
	h := RateLimit(fl)(okHandler())

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.5:1234"
	h.ServeHTTP(httptest.NewRecorder(), req)

	assert.Equal(t, "ip:10.0.0.5", fl.lastKey)
}

func TestClientIP_Precedence(t *testing.T) {
	cases := []struct {
		name    string
		headers map[string]string
		ra      string
		want    string
	}{
		{"x-forwarded-for single", map[string]string{"X-Forwarded-For": "1.2.3.4"}, "", "1.2.3.4"},
		{"x-forwarded-for chain", map[string]string{"X-Forwarded-For": "1.2.3.4, 5.6.7.8"}, "", "1.2.3.4"},
		{"x-real-ip", map[string]string{"X-Real-IP": "9.9.9.9"}, "", "9.9.9.9"},
		{"remote addr fallback", nil, "10.0.0.5:1234", "10.0.0.5"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			for k, v := range c.headers {
				req.Header.Set(k, v)
			}
			if c.ra != "" {
				req.RemoteAddr = c.ra
			}
			assert.Equal(t, c.want, clientIP(req))
		})
	}
}
