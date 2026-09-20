package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"api-gateway/internal/auth"
	"api-gateway/internal/config"
	"api-gateway/internal/logger"
	"api-gateway/internal/ratelimit"
	"api-gateway/internal/router"
	"api-gateway/internal/server"
)

func main() {
	if err := run(); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	log := logger.New(cfg.LogLevel, cfg.Env)
	slog.SetDefault(log)

	// JWT public key (RSA) — загружается из PEM-файла.
	keyProvider, err := auth.NewFileKeyProvider(cfg.JWTPublicKeyPath, cfg.JWTIssuer)
	if err != nil {
		return err
	}

	// Redis + rate limiter
	rl, err := ratelimit.NewRedisLimiter(ratelimit.RedisOptions{
		Addr:     cfg.RedisAddr,
		Password: cfg.RedisPassword,
		DB:       cfg.RedisDB,
		Limit:    cfg.RateLimit,
		Window:   cfg.RateLimitWindow,
	})
	if err != nil {
		return err
	}
	defer func() { _ = rl.Close() }()

	handler := router.New(router.Deps{
		Config:  cfg,
		Logger:  log,
		Keys:    keyProvider,
		Limiter: rl,
	})

	srv := server.New(cfg, handler)

	// Запуск в фоне
	errCh := make(chan error, 1)
	go func() {
		log.Info("api-gateway starting",
			"addr", srv.Addr(),
			"env", cfg.Env,
			"upstreams", cfg.Upstreams(),
		)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	// Ожидание сигнала или ошибки
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-errCh:
		return err
	case sig := <-stop:
		log.Info("shutdown signal received", "signal", sig.String())
	}

	ctx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Error("graceful shutdown failed", "err", err)
		return err
	}
	log.Info("api-gateway stopped")
	return nil
}
