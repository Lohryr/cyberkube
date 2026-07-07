package oci

import "sync"

// cache is a concurrency-safe in-memory blob cache keyed by sha256 digest.
// Content is immutable (addressed by digest), so entries never need
// invalidation.
type cache struct {
	mu   sync.RWMutex
	blob map[string][]byte
}

func newCache() *cache {
	return &cache{blob: map[string][]byte{}}
}

func (c *cache) get(digest string) ([]byte, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	b, ok := c.blob[digest]
	return b, ok
}

func (c *cache) put(digest string, b []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.blob[digest] = b
}
