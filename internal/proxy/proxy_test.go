package proxy

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestProxy(t *testing.T, target string, maxRequests uint32, minRequests uint32) *Proxy {
	t.Helper()
	u, err := url.Parse(target)
	require.NoError(t, err)

	return New(Options{
		Name:            "test",
		Target:          u,
		DialTimeout:     2 * time.Second,
		ResponseTimeout: 3 * time.Second,

		BreakerMaxRequests:  maxRequests,
		BreakerInterval:     2 * time.Second,
		BreakerTimeout:      500 * time.Millisecond,
		BreakerFailureRatio: 0.5,
		BreakerMinRequests:  minRequests,
	})
}

func TestProxy_ForwardsRequest(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Upstream", "true")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"hello":"world"}`))
	}))
	defer upstream.Close()

	p := newTestProxy(t, upstream.URL, 5, 10)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/orders", nil)
	p.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusCreated, rr.Code)
	assert.Equal(t, "true", rr.Header().Get("X-Upstream"))
	assert.Contains(t, rr.Body.String(), "world")
}

func TestProxy_CircuitBreakerOpensAfterFailures(t *testing.T) {
	var hits int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer upstream.Close()

	p := newTestProxy(t, upstream.URL, 1, 4) // min 4 запроса, ratio 0.5

	// Разогреваем: 4 запроса — все 5xx
	for i := 0; i < 4; i++ {
		rr := httptest.NewRecorder()
		p.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/", nil))
	}

	// Следующий запрос — breaker открыт.
	rr := httptest.NewRecorder()
	p.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/", nil))

	assert.Equal(t, http.StatusServiceUnavailable, rr.Code)
	assert.Equal(t, "10", rr.Header().Get("Retry-After"))

	// Проверяем, что upstream не получал запросов после открытия.
	before := atomic.LoadInt32(&hits)
	time.Sleep(50 * time.Millisecond)
	p.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	assert.Equal(t, before, atomic.LoadInt32(&hits))
}

func TestProxy_CircuitBreakerHalfOpenThenClose(t *testing.T) {
	var fail atomic.Bool
	fail.Store(true)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if fail.Load() {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	p := newTestProxy(t, upstream.URL, 1, 2)

	for i := 0; i < 2; i++ {
		p.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	}
	// breaker открыт
	rr := httptest.NewRecorder()
	p.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/", nil))
	require.Equal(t, http.StatusServiceUnavailable, rr.Code)

	// Ждём timeout breaker'а → half-open.
	time.Sleep(600 * time.Millisecond)

	// Чиним upstream → half-open пропустит, breaker закроется.
	fail.Store(false)

	rr = httptest.NewRecorder()
	p.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/", nil))
	assert.Equal(t, http.StatusOK, rr.Code)
}

func TestProxy_UnreachableUpstream(t *testing.T) {
	// Порт, куда никто не слушает.
	p := newTestProxy(t, "http://127.0.0.1:1", 5, 10)

	rr := httptest.NewRecorder()
	p.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/", nil))

	assert.Equal(t, http.StatusBadGateway, rr.Code)
}

func TestProxy_NameAndTarget(t *testing.T) {
	u, _ := url.Parse("http://example.com")
	p := New(Options{Name: "svc", Target: u})
	assert.Equal(t, "svc", p.Name())
	assert.Equal(t, u, p.Target())
}
