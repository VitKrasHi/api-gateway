package middleware

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func newTestLogger(buf *bytes.Buffer) *slog.Logger {
	return slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

func TestLogger_LogsRequest(t *testing.T) {
	var buf bytes.Buffer
	log := newTestLogger(&buf)

	h := Logger(log)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
		_, _ = w.Write([]byte("hello"))
	}))

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/foo?x=1", nil)
	req.RemoteAddr = "1.2.3.4:5678"

	h.ServeHTTP(rr, req)

	out := buf.String()
	assert.Contains(t, out, "http_request")
	assert.Contains(t, out, "method=GET")
	assert.Contains(t, out, "path=/foo")
	assert.Contains(t, out, "status=418")
	assert.Contains(t, out, "bytes=5")
	assert.Contains(t, out, "remote_addr=1.2.3.4:5678")
	assert.True(t, strings.Contains(out, "level=WARN")) // 418 → warn
}

func TestLogger_Levels(t *testing.T) {
	cases := []struct {
		status int
		level  string
	}{
		{200, "level=INFO"},
		{404, "level=WARN"},
		{500, "level=ERROR"},
	}
	for _, c := range cases {
		t.Run(http.StatusText(c.status), func(t *testing.T) {
			var buf bytes.Buffer
			log := newTestLogger(&buf)

			h := Logger(log)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(c.status)
			}))

			h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))

			assert.Contains(t, buf.String(), c.level)
		})
	}
}

func TestLevelFor(t *testing.T) {
	assert.Equal(t, slog.LevelError, levelFor(500))
	assert.Equal(t, slog.LevelError, levelFor(503))
	assert.Equal(t, slog.LevelWarn, levelFor(400))
	assert.Equal(t, slog.LevelWarn, levelFor(404))
	assert.Equal(t, slog.LevelInfo, levelFor(200))
	assert.Equal(t, slog.LevelInfo, levelFor(301))
}
