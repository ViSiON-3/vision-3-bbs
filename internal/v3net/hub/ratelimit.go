package hub

import (
	"sync"
	"time"
)

// rateLimiter implements a per-key rate limiter using a token bucket algorithm.
// Each key gets at most one token per interval. Stale entries are periodically
// evicted to prevent unbounded memory growth.
type rateLimiter struct {
	mu       sync.Mutex
	interval time.Duration
	ttl      time.Duration
	last     map[string]time.Time
	done     chan struct{}
}

func newRateLimiter(interval time.Duration) *rateLimiter {
	rl := &rateLimiter{
		interval: interval,
		ttl:      10 * interval, // evict entries older than 10× the interval
		last:     make(map[string]time.Time),
		done:     make(chan struct{}),
	}
	go rl.cleanupLoop()
	return rl
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

// Stop shuts down the background cleanup goroutine.
func (rl *rateLimiter) Stop() {
	close(rl.done)
}

func (rl *rateLimiter) cleanupLoop() {
	ticker := time.NewTicker(rl.ttl)
	defer ticker.Stop()
	for {
		select {
		case <-rl.done:
			return
		case <-ticker.C:
			rl.evict()
		}
	}
}

func (rl *rateLimiter) evict() {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	cutoff := time.Now().Add(-rl.ttl)
	for key, t := range rl.last {
		if t.Before(cutoff) {
			delete(rl.last, key)
		}
	}
}
