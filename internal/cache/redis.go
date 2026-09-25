package cache

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"
)

// RedisCache stores OCR results in Redis.
type RedisCache struct {
	client  *redis.Client
	ttl     time.Duration
	enabled bool
	prefix  string
	hits    atomic.Int64
	misses  atomic.Int64
}

// RedisOptions configures the Redis cache.
type RedisOptions struct {
	Addr         string        // "localhost:6379"
	Password     string        // "" for no password
	DB           int           // 0 = default DB
	TTL          time.Duration // cache entry TTL
	Prefix       string        // key prefix, e.g. "ocr:"
	PoolSize     int           // connection pool size
	MinIdleConns int           // minimum idle connections
}

// NewRedis creates a Redis-backed cache.
func NewRedis(ctx context.Context, opts RedisOptions) (*RedisCache, error) {
	poolSize := opts.PoolSize
	if poolSize <= 0 {
		poolSize = 10
	}

	minIdle := opts.MinIdleConns
	if minIdle <= 0 {
		minIdle = 2
	}

	client := redis.NewClient(&redis.Options{
		Addr:         opts.Addr,
		Password:     opts.Password,
		DB:           opts.DB,
		PoolSize:     poolSize,
		MinIdleConns: minIdle,
		DialTimeout:  5 * time.Second,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,
	})

	// Verify connection
	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("redis ping: %w", err)
	}

	prefix := opts.Prefix
	if prefix == "" {
		prefix = "ocr:"
	}

	ttl := opts.TTL
	if ttl <= 0 {
		ttl = time.Hour
	}

	return &RedisCache{
		client:  client,
		ttl:     ttl,
		enabled: true,
		prefix:  prefix,
	}, nil
}

// Enabled reports whether caching is active.
func (c *RedisCache) Enabled() bool {
	return c != nil && c.enabled
}

// Get retrieves an entry by OCR text hash.
func (c *RedisCache) Get(ocrText string) (*Entry, bool) {
	if !c.Enabled() {
		return nil, false
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	key := c.key(ocrText)
	data, err := c.client.Get(ctx, key).Bytes()
	if err != nil {
		c.misses.Add(1)
		return nil, false
	}

	var entry Entry
	if err := json.Unmarshal(data, &entry); err != nil {
		c.misses.Add(1)
		return nil, false
	}

	c.hits.Add(1)

	return &entry, true
}

// Set stores an entry keyed by OCR text hash.
func (c *RedisCache) Set(ocrText string, entry *Entry) {
	if !c.Enabled() || entry == nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	entry.CreatedAt = time.Now()
	data, err := json.Marshal(entry)
	if err != nil {
		return
	}

	key := c.key(ocrText)
	_ = c.client.Set(ctx, key, data, c.ttl).Err()
}

// Stats returns cache statistics.
func (c *RedisCache) Stats() Stats {
	if !c.Enabled() {
		return Stats{}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	// Count keys with prefix using SCAN (non-blocking)
	var count int
	iter := c.client.Scan(ctx, 0, c.prefix+"*", 100).Iterator()
	for iter.Next(ctx) {
		count++
	}

	return Stats{
		Entries: count,
		Hits:    c.hits.Load(),
		Misses:  c.misses.Load(),
	}
}

// Ping checks Redis connectivity.
func (c *RedisCache) Ping(ctx context.Context) error {
	if c == nil || c.client == nil {
		return fmt.Errorf("redis client is nil")
	}

	return c.client.Ping(ctx).Err()
}

// Close shuts down the Redis client.
func (c *RedisCache) Close() error {
	if c == nil || c.client == nil {
		return nil
	}

	return c.client.Close()
}

func (c *RedisCache) key(text string) string {
	h := sha256.Sum256([]byte(text))
	return c.prefix + hex.EncodeToString(h[:])
}
