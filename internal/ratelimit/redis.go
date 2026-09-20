package ratelimit

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

//go:embed sliding_window.lua
var slidingWindowScript string

type Limiter interface {
	// Allow возвращает allowed=true, если запрос разрешён.
	// remaining — сколько осталось до лимита, retryAfter — если отклонён.
	Allow(ctx context.Context, key string) (allowed bool, remaining int64, retryAfter time.Duration, err error)
	Close() error
}

type RedisOptions struct {
	Addr     string
	Password string
	DB       int
	Limit    int64
	Window   time.Duration
}

type RedisLimiter struct {
	rdb    *redis.Client
	script *redis.Script
	limit  int64
	window time.Duration
}

func NewRedisLimiter(opts RedisOptions) (*RedisLimiter, error) {
	if opts.Limit <= 0 {
		return nil, errors.New("rate limit must be > 0")
	}
	if opts.Window <= 0 {
		return nil, errors.New("rate limit window must be > 0")
	}
	rdb := redis.NewClient(&redis.Options{
		Addr:         opts.Addr,
		Password:     opts.Password,
		DB:           opts.DB,
		DialTimeout:  2 * time.Second,
		ReadTimeout:  1 * time.Second,
		WriteTimeout: 1 * time.Second,
		PoolSize:     20,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := rdb.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("redis ping: %w", err)
	}

	return &RedisLimiter{
		rdb:    rdb,
		script: redis.NewScript(slidingWindowScript),
		limit:  opts.Limit,
		window: opts.Window,
	}, nil
}

func (l *RedisLimiter) Allow(ctx context.Context, key string) (bool, int64, time.Duration, error) {
	now := time.Now().UnixMilli()
	windowMs := l.window.Milliseconds()

	raw, err := l.script.Run(ctx, l.rdb,
		[]string{"rl:" + key},
		now, windowMs, l.limit,
	).Result()
	if err != nil {
		return false, 0, 0, fmt.Errorf("rate limiter script: %w", err)
	}

	res, ok := raw.([]any)
	if !ok || len(res) < 3 {
		return false, 0, 0, fmt.Errorf("unexpected script reply: %v", raw)
	}

	allowed := toInt(res[0]) == 1
	remaining := toInt(res[1])
	oldestMs := toInt(res[2])

	var retryAfter time.Duration
	if !allowed && oldestMs > 0 {
		retryAfter = time.Duration(oldestMs+windowMs-now) * time.Millisecond
		if retryAfter < 0 {
			retryAfter = 0
		}
	}
	return allowed, remaining, retryAfter, nil
}

func (l *RedisLimiter) Close() error { return l.rdb.Close() }

func toInt(v any) int64 {
	switch n := v.(type) {
	case int64:
		return n
	case int:
		return int64(n)
	case float64:
		return int64(n)
	default:
		return 0
	}
}
