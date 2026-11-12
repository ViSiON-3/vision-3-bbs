package qwkapi

import (
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/logging"
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
	// LoginAttempt waits for ip's turn to check a login and returns done,
	// which ends it. A login holds its turn from the lockout check until its
	// outcome is recorded, so logins from one IP, here or at the terminal,
	// are checked one at a time and can't all pass the check together.
	LoginAttempt(ip string) (done func())
	// AdmitLogin clears ip's failed attempts for a login that checked out,
	// unless ip is locked out, and then reports false: a lockout another
	// login started while this one was being checked stays.
	AdmitLogin(ip string) bool
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
	start       time.Time // first failure in the current counting window
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
// locked. Failures are counted in fixed windows of the lockout period starting
// at the first failure, so the limit means that many failures within one
// period; a slow trickle of failures never adds up to a lock.
func (hl *handleLockouts) fail(handle string) bool {
	now := time.Now()
	key := strings.ToLower(handle)
	hl.mu.Lock()
	defer hl.mu.Unlock()
	f, exists := hl.m[key]
	if !exists {
		f = &handleFailures{start: now}
		hl.m[key] = f
	}
	// Reset the count in place: replacing the entry would drop a lock that a
	// concurrent request installed.
	if now.Sub(f.start) >= hl.lockout {
		f.count = 0
		f.start = now
	}
	f.count++
	if f.count >= hl.max {
		f.lockedUntil = now.Add(hl.lockout)
		f.count = 0
		f.start = now
		return true
	}
	return false
}

// clear forgets the failures for handle after a successful login. An active
// lock is kept: one can only exist here if failures racing this login
// installed it after the login passed its lock check, and wiping it would
// reset the guess limit for whoever made them.
func (hl *handleLockouts) clear(handle string) {
	key := strings.ToLower(handle)
	hl.mu.Lock()
	defer hl.mu.Unlock()
	if f, exists := hl.m[key]; exists && time.Now().Before(f.lockedUntil) {
		return
	}
	delete(hl.m, key)
}

// sweep removes entries that are neither locked nor in a counting window,
// bounding map growth.
func (hl *handleLockouts) sweep() {
	now := time.Now()
	hl.mu.Lock()
	for k, f := range hl.m {
		if !now.Before(f.lockedUntil) && now.Sub(f.start) >= hl.lockout {
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

// takeLoginTurn waits for ip's login turn and checks the IP's lockout again
// inside it: a login that waited may find the IP locked by the logins ahead of
// it. It returns done, which ends the turn; when the IP is locked out it has
// answered 429, logged event, ended the turn, and ok is false. With no Guard
// there is no turn to take.
func (s *Server) takeLoginTurn(w http.ResponseWriter, ip, event string) (done func(), ok bool) {
	if s.deps.Guard == nil {
		return func() {}, true
	}
	done = s.deps.Guard.LoginAttempt(ip)
	if locked, until, _ := s.deps.Guard.IsIPLockedOut(ip); locked {
		done()
		logging.Security(event, "remote", ip, "locked_until", until.Format(time.RFC3339))
		writeLockedOut(w, until, "too many failed logins from this address")
		return nil, false
	}
	return done, true
}

// admitLogin admits a login whose credentials checked out, clearing ip's
// failed attempts. If another login locked ip out meanwhile, the lockout
// stays: it answers 429 and reports false. event names the login in the log.
func (s *Server) admitLogin(w http.ResponseWriter, ip, event string) bool {
	if s.deps.Guard == nil || s.deps.Guard.AdmitLogin(ip) {
		return true
	}
	_, until, _ := s.deps.Guard.IsIPLockedOut(ip)
	logging.Security(event+" refused: IP locked out during the check", "remote", ip, "locked_until", until.Format(time.RFC3339))
	writeLockedOut(w, until, "too many failed logins from this address")
	return false
}
