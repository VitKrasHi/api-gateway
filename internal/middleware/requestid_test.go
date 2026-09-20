package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	"api-gateway/internal/transport"
)

func TestRequestID_GeneratesWhenAbsent(t *testing.T) {
	var seen string
	h := RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = transport.RequestID(r.Context())
	}))

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)

	h.ServeHTTP(rr, req)

	assert.NotEmpty(t, seen)
	assert.Equal(t, seen, rr.Header().Get(HeaderRequestID))
	assert.Len(t, seen, 32) // 16 байт → 32 hex-символа
}

func TestRequestID_PreservesClientID(t *testing.T) {
	const clientID = "abc-123"
	var seen string
	h := RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = transport.RequestID(r.Context())
	}))

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(HeaderRequestID, clientID)

	h.ServeHTTP(rr, req)

	assert.Equal(t, clientID, seen)
	assert.Equal(t, clientID, rr.Header().Get(HeaderRequestID))
}
