package qwkapi

import (
	"sync"
	"time"
)

// limiter is a fixed-window counter: at most max events per window per key.
type limiter struct {
	mu     sync.Mutex
	max    int
	window time.Duration
	hits   map[string]*window
}

type window struct {
	start time.Time
	count int
}

func newLimiter(max int, dur time.Duration) *limiter {
	return &limiter{max: max, window: dur, hits: make(map[string]*window)}
}

// allow records an event for key and reports whether it is within the limit.
func (l *limiter) allow(key string) bool {
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	w, ok := l.hits[key]
	if !ok || now.Sub(w.start) >= l.window {
		l.hits[key] = &window{start: now, count: 1}
		return true
	}
	if w.count >= l.max {
		return false
	}
	w.count++
	return true
}

// sweep removes fully-elapsed windows, bounding map growth.
func (l *limiter) sweep() {
	now := time.Now()
	l.mu.Lock()
	for k, w := range l.hits {
		if now.Sub(w.start) >= l.window {
			delete(l.hits, k)
		}
	}
	l.mu.Unlock()
}

// size reports the number of tracked windows (test helper).
func (l *limiter) size() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.hits)
}
