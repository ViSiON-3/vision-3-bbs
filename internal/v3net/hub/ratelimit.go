package hub

import (
	"sync"
	"time"
)

// rateLimiter implements a per-key rate limiter using a token bucket algorithm.
// Each key gets at most one token per interval.
type rateLimiter struct {
	mu       sync.Mutex
	interval time.Duration
	last     map[string]time.Time
}

func newRateLimiter(interval time.Duration) *rateLimiter {
	return &rateLimiter{
		interval: interval,
		last:     make(map[string]time.Time),
	}
}

// Allow returns true if the key is allowed to proceed (enough time has passed
// since the last allowed request).
func (rl *rateLimiter) Allow(key string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	if last, ok := rl.last[key]; ok {
		if now.Sub(last) < rl.interval {
			return false
		}
	}
	rl.last[key] = now
	return true
}
