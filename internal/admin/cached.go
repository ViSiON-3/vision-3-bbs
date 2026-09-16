package admin

import (
	"sync"
	"time"
)

// CachedInt wraps an expensive counter so it is recomputed at most once per
// ttl. The server calls counter getters on every tick (once a second), which
// is far too often for anything that scans a message base or the user list.
// The first call computes synchronously; later calls within ttl return the
// cached value. Safe for concurrent use.
func CachedInt(ttl time.Duration, compute func() int) func() int {
	var (
		mu    sync.Mutex
		value int
		at    time.Time
	)
	return func() int {
		mu.Lock()
		defer mu.Unlock()
		now := timeNow()
		if at.IsZero() || now.Sub(at) >= ttl {
			value = compute()
			at = now
		}
		return value
	}
}
