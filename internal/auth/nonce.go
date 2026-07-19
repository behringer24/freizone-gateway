package auth

import (
	"sync"
	"time"
)

// nonceCache is an in-memory, mutex-guarded replay guard. Unlike
// freizone-server's SQLite-backed nonce store, this never needs to
// survive a restart: the worst case after a restart is a replay window
// no wider than MaxClockSkew, which is an acceptable trade-off for a
// wake-only, non-sensitive action, and keeps the gateway free of any
// durable per-request state.
type nonceCache struct {
	mu   sync.Mutex
	seen map[string]time.Time // value is the entry's expiry time
}

func newNonceCache() *nonceCache {
	return &nonceCache{seen: map[string]time.Time{}}
}

// recordIfNew reports whether (keyID, nonce) has not been seen before,
// recording it with the given expiry as a side effect. A false return
// means this is a replay.
func (c *nonceCache) recordIfNew(keyID, nonce string, expiresAt time.Time) bool {
	key := keyID + "\x00" + nonce
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, exists := c.seen[key]; exists {
		return false
	}
	c.seen[key] = expiresAt
	return true
}

// sweep removes expired entries. Call this periodically (see
// cmd/gateway) so the cache doesn't grow unbounded.
func (c *nonceCache) sweep(now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for k, exp := range c.seen {
		if now.After(exp) {
			delete(c.seen, k)
		}
	}
}
