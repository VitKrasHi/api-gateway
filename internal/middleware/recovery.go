package middleware

import (
	"log/slog"
	"net/http"
	"runtime/debug"

	apierr "api-gateway/internal/errors"
	"api-gateway/internal/transport"
)

func Recovery(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					log.Error("panic recovered",
						"request_id", transport.RequestID(r.Context()),
						"panic", rec,
						"stack", string(debug.Stack()),
						"path", r.URL.Path,
					)
					apierr.Internal(w)
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}
