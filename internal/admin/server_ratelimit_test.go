package admin

import (
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/session"
)

// countingRegistry counts how many times the server polls it.
type countingRegistry struct{ polls int }

func (r *countingRegistry) ListActive() []*session.BbsSession {
	r.polls++
	return nil
}

// TestExecuteRefreshRateLimited verifies that a client spamming
// CommandRefresh cannot force snapshot rebuilds faster than the configured
// refresh interval: the command still succeeds, but the poll is skipped when
// the last tick is fresh enough.
func TestExecuteRefreshRateLimited(t *testing.T) {
	base := time.Now()
	now := base
	timeNow = func() time.Time { return now }
	defer func() { timeNow = time.Now }()

	reg := &countingRegistry{}
	srv := NewServer(ServerConfig{Reg: reg, Refresh: time.Second, MaxEvents: 4})

	for i := 0; i < 5; i++ {
		if r, err := srv.Execute(AdminCommand{Command: CommandRefresh}); err != nil || !r.OK {
			t.Fatalf("refresh %d should succeed: %v %+v", i, err, r)
		}
	}
	if reg.polls != 1 {
		t.Fatalf("5 rapid refreshes polled registry %d times, want 1", reg.polls)
	}

	now = base.Add(2 * time.Second)
	if r, err := srv.Execute(AdminCommand{Command: CommandRefresh}); err != nil || !r.OK {
		t.Fatalf("refresh after interval should succeed: %v %+v", err, r)
	}
	if reg.polls != 2 {
		t.Fatalf("refresh after interval polled registry %d times, want 2", reg.polls)
	}
}

// TestLastTickIsMonotonic verifies that an out-of-order tick cannot move
// lastTick backwards. tick() captures its `now` before contending for tickMu,
// so a refresh-forced tick can land after a later periodic tick; if the older
// timestamp won, the rate limit would be weakened or defeated.
func TestLastTickIsMonotonic(t *testing.T) {
	base := time.Now()
	reg := &countingRegistry{}
	srv := NewServer(ServerConfig{Reg: reg, Refresh: time.Second, MaxEvents: 4})

	srv.tick(base.Add(10 * time.Second)) // later tick lands first
	srv.tick(base)                       // stale tick applied afterwards

	srv.mu.RLock()
	last := srv.lastTick
	srv.mu.RUnlock()
	if last.Before(base.Add(10 * time.Second)) {
		t.Fatalf("lastTick moved backwards to %v, want >= %v", last, base.Add(10*time.Second))
	}
}
