package config

import (
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// helper: устанавливает минимально необходимые переменные.
func setRequiredEnv(t *testing.T) {
	t.Helper()
	t.Setenv("JWT_PUBLIC_KEY_PATH", "/tmp/key.pem")
	t.Setenv("AUTH_URL", "http://auth:8080")
	t.Setenv("CATALOG_URL", "http://catalog:8080")
	t.Setenv("ORDER_URL", "http://order:8080")
	t.Setenv("PAYMENT_URL", "http://payment:8080")
}

func TestLoad_Defaults(t *testing.T) {
	setRequiredEnv(t)

	cfg, err := Load()
	require.NoError(t, err)

	assert.Equal(t, "dev", cfg.Env)
	assert.Equal(t, "8080", cfg.Port)
	assert.Equal(t, 100, int(cfg.RateLimit))
	assert.Equal(t, time.Minute, cfg.RateLimitWindow)
	assert.Equal(t, 5*time.Second, cfg.ReadHeaderTimeout)
	assert.Equal(t, 15*time.Second, cfg.ShutdownTimeout)
	assert.Equal(t, "auth-service", cfg.JWTIssuer)
	assert.Equal(t, "localhost:6379", cfg.RedisAddr)
}

func TestLoad_OverridesFromEnv(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("PORT", "9090")
	t.Setenv("LOG_LEVEL", "debug")
	t.Setenv("RATE_LIMIT", "500")
	t.Setenv("RATE_LIMIT_WINDOW", "30s")
	t.Setenv("REDIS_ADDR", "redis.internal:6380")
	t.Setenv("BREAKER_FAILURE_RATIO", "0.75")

	cfg, err := Load()
	require.NoError(t, err)

	assert.Equal(t, "9090", cfg.Port)
	assert.Equal(t, slog.LevelDebug, cfg.LogLevel)
	assert.Equal(t, int64(500), cfg.RateLimit)
	assert.Equal(t, 30*time.Second, cfg.RateLimitWindow)
	assert.Equal(t, "redis.internal:6380", cfg.RedisAddr)
	assert.InDelta(t, 0.75, cfg.BreakerFailureRatio, 0.001)
}

func TestLoad_InvalidValuesFallBackToDefaults(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("RATE_LIMIT", "not-a-number")
	t.Setenv("RATE_LIMIT_WINDOW", "not-a-duration")

	cfg, err := Load()
	require.NoError(t, err)

	assert.Equal(t, int64(100), cfg.RateLimit)
	assert.Equal(t, time.Minute, cfg.RateLimitWindow)
}

func TestValidate_MissingUpstreamURL(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("AUTH_URL", "")

	_, err := Load()
	// mustGetEnv паникует — перехватываем.
	require.Error(t, err)
}

func TestValidate_InvalidUpstreamScheme(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("AUTH_URL", "auth:8080") // нет http://

	_, err := Load()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must be http")
}

func TestUpstreams(t *testing.T) {
	setRequiredEnv(t)
	cfg, err := Load()
	require.NoError(t, err)

	ups := cfg.Upstreams()
	assert.Len(t, ups, 4)
	assert.Equal(t, "http://auth:8080", ups["auth"])
}
