//go:build integration

package cache_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/williamkoller/invoice-ocr-poc/internal/cache"
	"github.com/williamkoller/invoice-ocr-poc/internal/extract"
	"github.com/williamkoller/invoice-ocr-poc/internal/jev"
)

func redisAddr() string {
	if addr := os.Getenv("REDIS_ADDR"); addr != "" {
		return addr
	}
	return "localhost:6379"
}

func TestRedisCache_GetSet(t *testing.T) {
	ctx := t.Context()

	c, err := cache.NewRedis(ctx, cache.RedisOptions{
		Addr:   redisAddr(),
		TTL:    time.Minute,
		Prefix: "test-getset:",
	})
	require.NoError(t, err)
	defer c.Close()

	text := "Cafe 2x 5,00 10,00\nTotal R$ 10,00"

	// Should miss initially
	_, ok := c.Get(text)
	require.False(t, ok, "expected miss")

	entry := &cache.Entry{
		OCRText:       text,
		OCRConfidence: 0.95,
		Parsed: extract.Result{
			Items:              []extract.Item{{Description: "Cafe", Quantity: 2, UnitAmount: 5, LineTotal: 10}},
			EstimatedTotal:     10,
			ComputedItemsTotal: 10,
		},
		Judgment: jev.Judgment{DocumentType: "receipt"},
	}

	c.Set(text, entry)

	got, ok := c.Get(text)
	require.True(t, ok, "expected hit")
	require.Equal(t, text, got.OCRText)
	require.Equal(t, 0.95, got.OCRConfidence)
	require.Equal(t, "receipt", got.Judgment.DocumentType)
	require.Len(t, got.Parsed.Items, 1)
}

func TestRedisCache_TTL(t *testing.T) {
	ctx := t.Context()

	c, err := cache.NewRedis(ctx, cache.RedisOptions{
		Addr:   redisAddr(),
		TTL:    100 * time.Millisecond,
		Prefix: "test-ttl:",
	})
	require.NoError(t, err)
	defer c.Close()

	text := "expires soon"
	c.Set(text, &cache.Entry{OCRText: text})

	// Should exist immediately
	_, ok := c.Get(text)
	require.True(t, ok, "expected hit before TTL")

	// Wait for TTL
	time.Sleep(150 * time.Millisecond)

	// Should be gone
	_, ok = c.Get(text)
	require.False(t, ok, "expected miss after TTL")
}

func TestRedisCache_Stats(t *testing.T) {
	ctx := t.Context()

	c, err := cache.NewRedis(ctx, cache.RedisOptions{
		Addr:   redisAddr(),
		TTL:    time.Minute,
		Prefix: "test-stats:",
	})
	require.NoError(t, err)
	defer c.Close()

	// Set some entries
	for i := range 5 {
		c.Set(string(rune('a'+i)), &cache.Entry{OCRText: string(rune('a' + i))})
	}

	// Get some (hits)
	c.Get("a")
	c.Get("b")

	// Get some (misses)
	c.Get("z")
	c.Get("y")

	stats := c.Stats()
	require.GreaterOrEqual(t, stats.Entries, 5)
	require.Equal(t, int64(2), stats.Hits)
	require.Equal(t, int64(2), stats.Misses)
}

func TestRedisCache_Ping(t *testing.T) {
	ctx := t.Context()

	c, err := cache.NewRedis(ctx, cache.RedisOptions{
		Addr:   redisAddr(),
		TTL:    time.Minute,
		Prefix: "test-ping:",
	})
	require.NoError(t, err)
	defer c.Close()

	err = c.Ping(ctx)
	require.NoError(t, err)
}

func TestRedisCache_ConnectionFailure(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()

	_, err := cache.NewRedis(ctx, cache.RedisOptions{
		Addr:   "localhost:59999", // Invalid port
		TTL:    time.Minute,
		Prefix: "test-fail:",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "redis ping")
}
