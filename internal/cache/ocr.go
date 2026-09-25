package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"sync"
	"sync/atomic"
	"time"
)

// MemoryCache stores processed OCR results keyed by text hash in memory.
type MemoryCache struct {
	entries sync.Map // map[string]*Entry
	ttl     time.Duration
	enabled bool
	hits    atomic.Int64
	misses  atomic.Int64
}

// New creates an in-memory cache. If ttl <= 0, entries never expire.
func New(ttl time.Duration, enabled bool) *MemoryCache {
	return &MemoryCache{
		ttl:     ttl,
		enabled: enabled,
	}
}

// Enabled reports whether caching is active.
func (c *MemoryCache) Enabled() bool {
	return c != nil && c.enabled
}

// Get retrieves an entry by OCR text hash. Returns nil, false on miss or expiry.
func (c *MemoryCache) Get(ocrText string) (*Entry, bool) {
	if !c.Enabled() {
		return nil, false
	}

	key := c.hash(ocrText)
	val, ok := c.entries.Load(key)
	if !ok {
		c.misses.Add(1)
		return nil, false
	}

	entry := val.(*Entry)
	if c.ttl > 0 && time.Since(entry.CreatedAt) > c.ttl {
		c.entries.Delete(key)
		c.misses.Add(1)

		return nil, false
	}

	c.hits.Add(1)

	return entry, true
}

// Set stores an entry keyed by OCR text hash.
func (c *MemoryCache) Set(ocrText string, entry *Entry) {
	if !c.Enabled() || entry == nil {
		return
	}

	entry.CreatedAt = time.Now()
	c.entries.Store(c.hash(ocrText), entry)
}

func (c *MemoryCache) hash(text string) string {
	h := sha256.Sum256([]byte(text))

	return hex.EncodeToString(h[:])
}

// Stats returns current cache statistics.
func (c *MemoryCache) Stats() Stats {
	if !c.Enabled() {
		return Stats{}
	}

	var count int
	c.entries.Range(func(_, _ any) bool {
		count++

		return true
	})

	return Stats{
		Entries: count,
		Hits:    c.hits.Load(),
		Misses:  c.misses.Load(),
	}
}
