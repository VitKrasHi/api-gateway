package config

import (
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Env  string
	Port string

	LogLevel slog.Level

	// JWT
	JWTPublicKeyPath string
	JWTIssuer        string

	// Redis / rate-limit
	RedisAddr       string
	RedisPassword   string
	RedisDB         int
	RateLimit       int64
	RateLimitWindow time.Duration

	// Upstreams
	AuthURL    string
	CatalogURL string
	OrderURL   string
	PaymentURL string

	// Server
	ReadHeaderTimeout time.Duration
	ReadTimeout       time.Duration
	WriteTimeout      time.Duration
	IdleTimeout       time.Duration
	ShutdownTimeout   time.Duration

	// Proxy
	ProxyDialTimeout     time.Duration
	ProxyResponseTimeout time.Duration
	BreakerMaxRequests   uint32
	BreakerInterval      time.Duration
	BreakerTimeout       time.Duration
	BreakerFailureRatio  float64
	BreakerMinRequests   uint32
}

// Upstreams возвращает карту prefix→URL для логов/метрик.
func (c *Config) Upstreams() map[string]string {
	return map[string]string{
		"auth":    c.AuthURL,
		"catalog": c.CatalogURL,
		"order":   c.OrderURL,
		"payment": c.PaymentURL,
	}
}

func Load() (*Config, error) {
	cfg := &Config{
		Env:      getEnv("APP_ENV", "dev"),
		Port:     getEnv("PORT", "8080"),
		LogLevel: parseLevel(getEnv("LOG_LEVEL", "info")),

		JWTIssuer: getEnv("JWT_ISSUER", "auth-service"),

		RedisAddr:       getEnv("REDIS_ADDR", "localhost:6379"),
		RedisPassword:   getEnv("REDIS_PASSWORD", ""),
		RedisDB:         getEnvInt("REDIS_DB", 0),
		RateLimit:       int64(getEnvInt("RATE_LIMIT", 100)),
		RateLimitWindow: getEnvDuration("RATE_LIMIT_WINDOW", time.Minute),

		ReadHeaderTimeout: getEnvDuration("SERVER_READ_HEADER_TIMEOUT", 5*time.Second),
		ReadTimeout:       getEnvDuration("SERVER_READ_TIMEOUT", 15*time.Second),
		WriteTimeout:      getEnvDuration("SERVER_WRITE_TIMEOUT", 30*time.Second),
		IdleTimeout:       getEnvDuration("SERVER_IDLE_TIMEOUT", 60*time.Second),
		ShutdownTimeout:   getEnvDuration("SERVER_SHUTDOWN_TIMEOUT", 15*time.Second),

		ProxyDialTimeout:     getEnvDuration("PROXY_DIAL_TIMEOUT", 5*time.Second),
		ProxyResponseTimeout: getEnvDuration("PROXY_RESPONSE_TIMEOUT", 20*time.Second),
		BreakerMaxRequests:   uint32(getEnvInt("BREAKER_MAX_REQUESTS", 5)),
		BreakerInterval:      getEnvDuration("BREAKER_INTERVAL", 10*time.Second),
		BreakerTimeout:       getEnvDuration("BREAKER_TIMEOUT", 30*time.Second),
		BreakerFailureRatio:  getEnvFloat("BREAKER_FAILURE_RATIO", 0.5),
		BreakerMinRequests:   uint32(getEnvInt("BREAKER_MIN_REQUESTS", 10)),
	}

	// Обязательные переменные — через error, а не panic.
	required := map[string]*string{
		"JWT_PUBLIC_KEY_PATH": &cfg.JWTPublicKeyPath,
		"AUTH_URL":            &cfg.AuthURL,
		"CATALOG_URL":         &cfg.CatalogURL,
		"ORDER_URL":           &cfg.OrderURL,
		"PAYMENT_URL":         &cfg.PaymentURL,
	}
	for name, dst := range required {
		v := os.Getenv(name)
		if v == "" {
			return nil, fmt.Errorf("environment variable %s is required", name)
		}
		*dst = v
	}

	return cfg, cfg.Validate()
}

func (c *Config) Validate() error {
	if c.Port == "" {
		return fmt.Errorf("PORT is empty")
	}
	if c.JWTPublicKeyPath == "" {
		return fmt.Errorf("JWT_PUBLIC_KEY_PATH is required")
	}
	for _, u := range c.Upstreams() {
		if !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
			return fmt.Errorf("upstream URL must be http(s): %q", u)
		}
	}
	return nil
}

// ---------- helpers ----------

func getEnv(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}

func mustGetEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		panic(fmt.Sprintf("environment variable %s is required", key))
	}
	return v
}

func getEnvInt(key string, def int) int {
	if v, ok := os.LookupEnv(key); ok {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func getEnvFloat(key string, def float64) float64 {
	if v, ok := os.LookupEnv(key); ok {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return def
}

func getEnvDuration(key string, def time.Duration) time.Duration {
	if v, ok := os.LookupEnv(key); ok {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}

func parseLevel(s string) slog.Level {
	switch strings.ToLower(s) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
