package router

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"api-gateway/internal/auth"
	"api-gateway/internal/config"
	"api-gateway/internal/ratelimit"
)

// ---------- окружение ----------

type env struct {
	router     http.Handler
	priv       *rsa.PrivateKey
	authSrv    *httptest.Server
	catalogSrv *httptest.Server
	orderSrv   *httptest.Server
	paymentSrv *httptest.Server
}

func setupEnv(t *testing.T) *env {
	t.Helper()

	// 1. RSA-пара + PEM-файл публичного ключа
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	der, err := x509.MarshalPKIXPublicKey(&priv.PublicKey)
	require.NoError(t, err)
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})
	keyPath := filepath.Join(t.TempDir(), "jwt-public.pem")
	require.NoError(t, os.WriteFile(keyPath, pemBytes, 0o600))

	// 2. Fake upstream-сервисы
	fake := func(name string) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Service", name)
			w.Header().Set("X-Upstream-Path", r.URL.Path)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"service":"` + name + `"}`))
		}))
	}
	authSrv := fake("auth")
	catalogSrv := fake("catalog")
	orderSrv := fake("order")
	paymentSrv := fake("payment")

	t.Cleanup(func() {
		authSrv.Close()
		catalogSrv.Close()
		orderSrv.Close()
		paymentSrv.Close()
	})

	// 3. miniredis + лимитер
	mr, err := miniredis.Run()
	require.NoError(t, err)
	t.Cleanup(mr.Close)

	limiter, err := ratelimit.NewRedisLimiter(ratelimit.RedisOptions{
		Addr: mr.Addr(), Limit: 1000, Window: time.Minute,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = limiter.Close() })

	// 4. Конфиг
	cfg := &config.Config{
		Env:                  "test",
		Port:                 "0",
		LogLevel:             slog.LevelError,
		JWTPublicKeyPath:     keyPath,
		JWTIssuer:            "auth-service",
		AuthURL:              authSrv.URL,
		CatalogURL:           catalogSrv.URL,
		OrderURL:             orderSrv.URL,
		PaymentURL:           paymentSrv.URL,
		ReadHeaderTimeout:    time.Second,
		ReadTimeout:          time.Second,
		WriteTimeout:         time.Second,
		IdleTimeout:          time.Second,
		ShutdownTimeout:      time.Second,
		ProxyDialTimeout:     time.Second,
		ProxyResponseTimeout: 2 * time.Second,
		BreakerMaxRequests:   10,
		BreakerInterval:      5 * time.Second,
		BreakerTimeout:       5 * time.Second,
		BreakerFailureRatio:  0.5,
		BreakerMinRequests:   20,
	}

	// 5. KeyProvider
	kp, err := auth.NewFileKeyProvider(keyPath, cfg.JWTIssuer)
	require.NoError(t, err)

	// 6. Роутер
	handler := New(Deps{
		Config:  cfg,
		Logger:  slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})),
		Keys:    kp,
		Limiter: limiter,
	})

	return &env{
		router:     handler,
		priv:       priv,
		authSrv:    authSrv,
		catalogSrv: catalogSrv,
		orderSrv:   orderSrv,
		paymentSrv: paymentSrv,
	}
}

func (e *env) token(t *testing.T, sub string, roles []string) string {
	t.Helper()
	claims := jwt.MapClaims{
		"iss": "auth-service", "sub": sub, "roles": roles,
		"exp": time.Now().Add(time.Hour).Unix(),
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	s, err := tok.SignedString(e.priv)
	require.NoError(t, err)
	return s
}

// ---------- tests ----------

func TestRouter_Healthz(t *testing.T) {
	e := setupEnv(t)
	rr := httptest.NewRecorder()
	e.router.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Contains(t, rr.Body.String(), "ok")
}

func TestRouter_Readyz(t *testing.T) {
	e := setupEnv(t)
	rr := httptest.NewRecorder()
	e.router.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	assert.Equal(t, http.StatusOK, rr.Code)
}

func TestRouter_Metrics(t *testing.T) {
	e := setupEnv(t)
	rr := httptest.NewRecorder()
	e.router.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Contains(t, rr.Body.String(), "http_requests_total")
}

func TestRouter_LoginIsPublic(t *testing.T) {
	e := setupEnv(t)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", nil)

	e.router.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, "auth", rr.Header().Get("X-Service"))
}

func TestRouter_CatalogGetIsPublic(t *testing.T) {
	e := setupEnv(t)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/catalog/products", nil)

	e.router.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, "catalog", rr.Header().Get("X-Service"))
}

func TestRouter_OrdersRequireAuth(t *testing.T) {
	e := setupEnv(t)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/orders", nil)

	e.router.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusUnauthorized, rr.Code)
}

func TestRouter_OrdersWithValidToken(t *testing.T) {
	e := setupEnv(t)
	tok := e.token(t, "user-1", []string{"user"})

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/orders", nil)
	req.Header.Set("Authorization", "Bearer "+tok)

	e.router.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, "order", rr.Header().Get("X-Service"))
}

func TestRouter_PrefixIsStripped(t *testing.T) {
	e := setupEnv(t)
	tok := e.token(t, "u", nil)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/orders/123", nil)
	req.Header.Set("Authorization", "Bearer "+tok)

	e.router.ServeHTTP(rr, req)

	// upstream должен получить /123
	assert.Equal(t, "/123", rr.Header().Get("X-Upstream-Path"))
}

func TestRouter_PaymentRoute(t *testing.T) {
	e := setupEnv(t)
	tok := e.token(t, "u", nil)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/pay/charge", nil)
	req.Header.Set("Authorization", "Bearer "+tok)

	e.router.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, "payment", rr.Header().Get("X-Service"))
}

func TestRouter_NotFound(t *testing.T) {
	e := setupEnv(t)
	rr := httptest.NewRecorder()
	e.router.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/nope", nil))

	assert.Equal(t, http.StatusNotFound, rr.Code)
	assert.Contains(t, rr.Body.String(), "not_found")
}

func TestRouter_MethodNotAllowed(t *testing.T) {
	e := setupEnv(t)
	rr := httptest.NewRecorder()
	// healthz — только GET
	e.router.ServeHTTP(rr, httptest.NewRequest(http.MethodDelete, "/healthz", nil))

	assert.Equal(t, http.StatusMethodNotAllowed, rr.Code)
}

func TestRouter_RequestIDPropagated(t *testing.T) {
	e := setupEnv(t)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req.Header.Set("X-Request-ID", "trace-42")

	e.router.ServeHTTP(rr, req)

	assert.Equal(t, "trace-42", rr.Header().Get("X-Request-ID"))
}
