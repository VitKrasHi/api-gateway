package ratelimit

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupLimiter(t *testing.T, limit int64, window time.Duration) (*RedisLimiter, *miniredis.Miniredis) {
	t.Helper()
	mr, err := miniredis.Run()
	require.NoError(t, err)
	t.Cleanup(mr.Close)

	rl, err := NewRedisLimiter(RedisOptions{
		Addr:   mr.Addr(),
		Limit:  limit,
		Window: window,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = rl.Close() })

	return rl, mr
}

func TestRedisLimiter_AllowsUnderLimit(t *testing.T) {
	rl, _ := setupLimiter(t, 5, time.Second)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		allowed, remaining, _, err := rl.Allow(ctx, "user:1")
		require.NoError(t, err)
		assert.True(t, allowed, "request %d should be allowed", i+1)
		assert.Equal(t, int64(4-i), remaining)
	}
}

func TestRedisLimiter_BlocksOverLimit(t *testing.T) {
	rl, _ := setupLimiter(t, 3, time.Second)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		allowed, _, _, err := rl.Allow(ctx, "user:1")
		require.NoError(t, err)
		require.True(t, allowed)
	}

	allowed, remaining, retryAfter, err := rl.Allow(ctx, "user:1")
	require.NoError(t, err)
	assert.False(t, allowed)
	assert.Equal(t, int64(0), remaining)
	assert.Greater(t, retryAfter, time.Duration(0))
	assert.LessOrEqual(t, retryAfter, time.Second)
}

func TestRedisLimiter_KeysAreIsolated(t *testing.T) {
	rl, _ := setupLimiter(t, 2, time.Second)
	ctx := context.Background()

	// user:1 исчерпывает лимит
	_, _, _, _ = rl.Allow(ctx, "user:1")
	_, _, _, _ = rl.Allow(ctx, "user:1")
	allowed, _, _, _ := rl.Allow(ctx, "user:1")
	assert.False(t, allowed)

	// user:2 — свежий лимит
	allowed, remaining, _, err := rl.Allow(ctx, "user:2")
	require.NoError(t, err)
	assert.True(t, allowed)
	assert.Equal(t, int64(1), remaining)
}

func TestRedisLimiter_WindowSlides(t *testing.T) {
	rl, mr := setupLimiter(t, 2, time.Second)
	ctx := context.Background()

	_, _, _, _ = rl.Allow(ctx, "k")
	_, _, _, _ = rl.Allow(ctx, "k")
	allowed, _, _, _ := rl.Allow(ctx, "k")
	assert.False(t, allowed)

	// прокручиваем время miniredis — окно уходит
	mr.FastForward(1100 * time.Millisecond)

	allowed, remaining, _, err := rl.Allow(ctx, "k")
	require.NoError(t, err)
	assert.True(t, allowed)
	assert.Equal(t, int64(1), remaining)
}

func TestNewRedisLimiter_InvalidOptions(t *testing.T) {
	_, err := NewRedisLimiter(RedisOptions{Addr: "x", Limit: 0, Window: time.Second})
	require.Error(t, err)

	_, err = NewRedisLimiter(RedisOptions{Addr: "x", Limit: 1, Window: 0})
	require.Error(t, err)
}

func TestNewRedisLimiter_UnreachableRedis(t *testing.T) {
	_, err := NewRedisLimiter(RedisOptions{
		Addr:   "127.0.0.1:1", // заведомо закрытый порт
		Limit:  10,
		Window: time.Second,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "redis ping")
}
