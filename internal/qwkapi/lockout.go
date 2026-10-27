package qwkapi

import (
	"strings"
	"sync"
	"time"
)

// LoginGuard is the BBS-wide failed-login and IP blocklist state (satisfied by
// the ConnectionTracker in cmd/vision3). Sharing it means a password guessed at
// over the API counts toward the same per-IP lockout as the telnet/SSH login
// prompt, and an IP the BBS has blocked cannot use the API either.
type LoginGuard interface {
	// IsBlocked reports whether ip is on the blocklist (and not allowlisted).
	IsBlocked(ip string) bool
	IsIPLockedOut(ip string) (bool, time.Time, int)
	RecordFailedLoginAttempt(ip string) bool
	ClearFailedLoginAttempts(ip string)
}

// Per-handle lockout thresholds. The per-IP lockout does nothing against
// guesses spread across many addresses, so failures are also counted per
// handle. The limit is looser than the per-IP default because anyone can trip
// it for someone else's handle; it only affects API logins, so the worst an
// attacker can do is pause that user's mobile sync, not lock them off the BBS.
const (
	handleMaxFailures = 10
	handleLockout     = 15 * time.Minute
)

// handleLockouts counts failed API logins per handle. Handles are tracked
// whether or not the account exists, so a lockout reveals nothing about which
// handles are real.
type handleLockouts struct {
	mu      sync.Mutex
	max     int
	lockout time.Duration
	m       map[string]*handleFailures
}

type handleFailures struct {
	count       int
	last        time.Time
	lockedUntil time.Time
}

func newHandleLockouts(max int, lockout time.Duration) *handleLockouts {
	return &handleLockouts{max: max, lockout: lockout, m: make(map[string]*handleFailures)}
}

// lockedUntil returns when the lockout on handle ends; ok is false if it is
// not locked.
func (hl *handleLockouts) lockedUntil(handle string) (until time.Time, ok bool) {
	hl.mu.Lock()
	defer hl.mu.Unlock()
	f, exists := hl.m[strings.ToLower(handle)]
	if !exists || !time.Now().Before(f.lockedUntil) {
		return time.Time{}, false
	}
	return f.lockedUntil, true
}

// fail records a failed login for handle and reports whether it just became
// locked. Failures older than the lockout period are forgotten, so the count
// is of recent failures rather than all-time ones.
func (hl *handleLockouts) fail(handle string) bool {
	now := time.Now()
	key := strings.ToLower(handle)
	hl.mu.Lock()
	defer hl.mu.Unlock()
	f, exists := hl.m[key]
	if !exists || now.Sub(f.last) >= hl.lockout {
		f = &handleFailures{}
		hl.m[key] = f
	}
	f.count++
	f.last = now
	if f.count >= hl.max {
		f.lockedUntil = now.Add(hl.lockout)
		f.count = 0
		return true
	}
	return false
}

// clear forgets the failures for handle after a successful login.
func (hl *handleLockouts) clear(handle string) {
	hl.mu.Lock()
	delete(hl.m, strings.ToLower(handle))
	hl.mu.Unlock()
}

// sweep removes entries that are neither locked nor holding recent failures,
// bounding map growth.
func (hl *handleLockouts) sweep() {
	now := time.Now()
	hl.mu.Lock()
	for k, f := range hl.m {
		if !now.Before(f.lockedUntil) && now.Sub(f.last) >= hl.lockout {
			delete(hl.m, k)
		}
	}
	hl.mu.Unlock()
}

// size reports the number of tracked handles (test helper).
func (hl *handleLockouts) size() int {
	hl.mu.Lock()
	defer hl.mu.Unlock()
	return len(hl.m)
}
