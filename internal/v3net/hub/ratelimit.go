package hub

import (
	"sync"
	"time"
)

// rateLimiter is a per-key rate limiter. Each key earns one request per
// interval and may save up to burst of them, so after a quiet spell it can
// send burst requests back to back. It uses the generic cell rate algorithm:
// rather than counting tokens it stores, per key, the time at which the key's
// allowance would be fully used up. Stale entries are periodically evicted to
// prevent unbounded memory growth.
type rateLimiter struct {
	mu       sync.Mutex
	interval time.Duration
	burst    int
	ttl      time.Duration
	tat      map[string]time.Time // key → theoretical arrival time
	done     chan struct{}
	stopOnce sync.Once
}

// newRateLimiter returns a limiter allowing each key one request per interval.
func newRateLimiter(interval time.Duration) *rateLimiter {
	return newBurstRateLimiter(interval, 1)
}

// newBurstRateLimiter returns a limiter allowing each key one request per
// interval on average, with up to burst requests at once.
func newBurstRateLimiter(interval time.Duration, burst int) *rateLimiter {
	if burst < 1 {
		burst = 1
	}
	rl := &rateLimiter{
		interval: interval,
		burst:    burst,
		// Evict entries older than 10× the time a full burst takes to refill.
		ttl:  10 * time.Duration(burst) * interval,
		tat:  make(map[string]time.Time),
		done: make(chan struct{}),
	}
	go rl.cleanupLoop()
	return rl
}

// Allow reports whether the key may proceed now, and if so takes one request
// from its allowance.
func (rl *rateLimiter) Allow(key string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	next, ok := rl.admitLocked(key, time.Now())
	if ok {
		rl.tat[key] = next
	}
	return ok
}

// allowBoth admits a request only if key a may proceed on limiter a and key
// b on limiter b, taking from both allowances or from neither. A request
// refused by one limiter therefore costs nothing on the other, and leaves
// no new entry in either. Callers must always pass the same two limiters
// in the same order, since both locks are held at once.
func allowBoth(a *rateLimiter, keyA string, b *rateLimiter, keyB string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	b.mu.Lock()
	defer b.mu.Unlock()

	now := time.Now()
	nextA, ok := a.admitLocked(keyA, now)
	if !ok {
		return false
	}
	nextB, ok := b.admitLocked(keyB, now)
	if !ok {
		return false
	}
	a.tat[keyA] = nextA
	b.tat[keyB] = nextB
	return true
}

// admitLocked reports whether key may proceed at now and, if so, the
// theoretical arrival time to store once the request is taken. It does not
// change the limiter. Caller must hold rl.mu.
func (rl *rateLimiter) admitLocked(key string, now time.Time) (time.Time, bool) {
	tat, ok := rl.tat[key]
	if !ok || tat.Before(now) {
		tat = now
	}
	// The key is over its limit if granting this request would put its
	// allowance more than burst intervals in the future.
	if tat.Sub(now) > time.Duration(rl.burst-1)*rl.interval {
		return time.Time{}, false
	}
	return tat.Add(rl.interval), true
}

// Stop shuts down the background cleanup goroutine. It is safe to call more
// than once.
func (rl *rateLimiter) Stop() {
	rl.stopOnce.Do(func() { close(rl.done) })
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
	for key, t := range rl.tat {
		if t.Before(cutoff) {
			delete(rl.tat, key)
		}
	}
}
