package router

import (
	"log/slog"
	"net/http"
	"net/url"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"api-gateway/internal/auth"
	"api-gateway/internal/config"
	"api-gateway/internal/middleware"
	"api-gateway/internal/proxy"
	"api-gateway/internal/ratelimit"
)

type Deps struct {
	Config  *config.Config
	Logger  *slog.Logger
	Keys    auth.KeyProvider
	Limiter ratelimit.Limiter
}

func New(deps Deps) http.Handler {
	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(chimw.RealIP)
	r.Use(middleware.Logger(deps.Logger))
	r.Use(middleware.Recovery(deps.Logger))
	r.Use(middleware.Metrics)
	r.Use(middleware.Auth(middleware.AuthConfig{
		Provider:          deps.Keys,
		PublicRoutes:      publicRoutes(),
		ProtectedPrefixes: []string{"/api/v1/"}, // только API защищаем
	}))
	r.Use(middleware.RateLimit(deps.Limiter))

	r.Handle("/metrics", promhttp.Handler())
	r.Get("/healthz", okHandler(`{"status":"ok"}`))
	r.Get("/readyz", okHandler(`{"status":"ready"}`))

	for _, u := range upstreams(deps.Config) {
		target, err := url.Parse(u.url)
		if err != nil {
			deps.Logger.Error("invalid upstream URL", "service", u.name, "url", u.url, "err", err)
			continue
		}
		p := proxy.New(proxy.Options{
			Name:                u.name,
			Target:              target,
			DialTimeout:         deps.Config.ProxyDialTimeout,
			ResponseTimeout:     deps.Config.ProxyResponseTimeout,
			BreakerMaxRequests:  deps.Config.BreakerMaxRequests,
			BreakerInterval:     deps.Config.BreakerInterval,
			BreakerTimeout:      deps.Config.BreakerTimeout,
			BreakerFailureRatio: deps.Config.BreakerFailureRatio,
			BreakerMinRequests:  deps.Config.BreakerMinRequests,
		})
		r.Handle(u.prefix+"/*", http.StripPrefix(u.prefix, p))
		r.Handle(u.prefix, http.StripPrefix(u.prefix, p))
	}

	r.NotFound(jsonError(http.StatusNotFound, "not_found", "route not found"))
	r.MethodNotAllowed(jsonError(http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed"))

	return r
}

// ---------- helpers ----------

type upstream struct {
	prefix string
	name   string
	url    string
}

func upstreams(c *config.Config) []upstream {
	return []upstream{
		{"/api/v1/auth", "auth-service", c.AuthURL},
		{"/api/v1/catalog", "catalog-service", c.CatalogURL},
		{"/api/v1/orders", "order-service", c.OrderURL},
		{"/api/v1/pay", "payment-service", c.PaymentURL},
	}
}

func publicRoutes() []middleware.PublicRoute {
	return []middleware.PublicRoute{
		{Method: http.MethodGet, Prefix: "/healthz", Exact: true},
		{Method: http.MethodGet, Prefix: "/readyz", Exact: true},
		{Method: http.MethodGet, Prefix: "/metrics", Exact: true},
		{Method: http.MethodPost, Prefix: "/api/v1/auth/register", Exact: true},
		{Method: http.MethodPost, Prefix: "/api/v1/auth/login", Exact: true},
		{Method: http.MethodPost, Prefix: "/api/v1/auth/refresh", Exact: true},
		{Method: http.MethodGet, Prefix: "/api/v1/catalog/products"},
		{Method: http.MethodGet, Prefix: "/api/v1/catalog/categories"},
	}
}

func okHandler(body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}
}

func jsonError(status int, code, msg string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(`{"code":"` + code + `","message":"` + msg + `"}`))
	}
}
