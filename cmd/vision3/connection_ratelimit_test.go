package main

import (
	"net"
	"testing"
	"time"
)

func addr(ip string) net.Addr {
	return &net.TCPAddr{IP: net.ParseIP(ip), Port: 12345}
}

func newRateTracker() *ConnectionTracker {
	return &ConnectionTracker{
		activeConnections: make(map[string]int),
		failedLogins:      make(map[string]*IPLockoutTracker),
		connAttempts:      make(map[string][]time.Time),
		connTempBans:      make(map[string]time.Time),
	}
}

func TestConnRateLimitBansAfterThreshold(t *testing.T) {
	ct := newRateTracker()
	ct.SetConnRateLimit(true, 3, 10, 90) // 3 hits / 10s -> ban 90m
	a := addr("203.0.113.5")
	// First 3 accepted, 3rd trips the ban on record.
	for i := 0; i < 2; i++ {
		if ok, _ := ct.TryAccept(a); !ok {
			t.Fatalf("attempt %d rejected early", i+1)
		}
	}
	if ok, _ := ct.TryAccept(a); ok {
		t.Fatal("3rd attempt should have been banned")
	}
	// Now temp-banned: further attempts rejected by canAcceptLocked.
	if ok, reason := ct.CanAccept(a); ok {
		t.Fatalf("banned IP accepted; reason=%q", reason)
	}
}

func TestConnRateLimitBanExpires(t *testing.T) {
	ct := newRateTracker()
	ct.SetConnRateLimit(true, 1, 10, 90)
	a := addr("203.0.113.6")
	ct.TryAccept(a) // 1 hit -> immediately banned
	ct.connRateBan = 40 * time.Millisecond
	ct.mu.Lock()
	ct.connTempBans["203.0.113.6"] = time.Now().Add(40 * time.Millisecond)
	ct.mu.Unlock()
	if ok, _ := ct.CanAccept(a); ok {
		t.Fatal("should still be banned")
	}
	time.Sleep(60 * time.Millisecond)
	if ok, reason := ct.CanAccept(a); !ok {
		t.Fatalf("ban should have expired; reason=%q", reason)
	}
}

func TestConnRateLimitDisabledNoop(t *testing.T) {
	ct := newRateTracker()
	ct.SetConnRateLimit(false, 1, 10, 90)
	a := addr("203.0.113.7")
	for i := 0; i < 5; i++ {
		if ok, _ := ct.TryAccept(a); !ok {
			t.Fatalf("attempt %d rejected while disabled", i+1)
		}
		ct.RemoveConnection(a)
	}
}

func TestConnRateLimitAllowlistExempt(t *testing.T) {
	ct := newRateTracker()
	ct.SetConnRateLimit(true, 3, 10, 90) // 3 hits / 10s -> ban 90m
	ip := "203.0.113.9"
	ct.allowlist = &IPList{ips: map[string]bool{ip: true}}
	a := addr(ip)
	for i := 0; i < 10; i++ {
		if ok, reason := ct.TryAccept(a); !ok {
			t.Fatalf("attempt %d rejected for allowlisted IP; reason=%q", i+1, reason)
		}
		ct.RemoveConnection(a)
	}
}

func TestConnRateLimitPrunesStaleState(t *testing.T) {
	ct := newRateTracker()
	ct.SetConnRateLimit(true, 1000, 10, 90)
	ct.connRateWindow = 20 * time.Millisecond
	old := time.Now().Add(-time.Hour)
	for i := 0; i < connRateSweepThreshold+10; i++ {
		ip := net.IPv4(203, 0, byte(i/256), byte(i%256)).String()
		ct.connAttempts[ip] = []time.Time{old}
	}
	ct.connTempBans["203.0.113.100"] = time.Now().Add(-time.Minute)
	ct.connTempBans["203.0.113.101"] = time.Now().Add(-time.Minute)

	time.Sleep(30 * time.Millisecond) // let the window pass for the stale entries

	// A single record call on a fresh IP should trigger the sweep, since the
	// map is already well past connRateSweepThreshold.
	ct.mu.Lock()
	before := len(ct.connAttempts)
	ct.recordConnAttemptLocked("198.51.100.1")
	after := len(ct.connAttempts)
	bansAfter := len(ct.connTempBans)
	ct.mu.Unlock()

	if after >= before {
		t.Fatalf("expected stale connAttempts entries to be pruned: before=%d after=%d", before, after)
	}
	if bansAfter != 0 {
		t.Fatalf("expected expired connTempBans to be pruned, got %d remaining", bansAfter)
	}
}

func TestConnRateLimitWindowSlides(t *testing.T) {
	ct := newRateTracker()
	ct.SetConnRateLimit(true, 3, 10, 90)
	ct.connRateWindow = 30 * time.Millisecond
	a := addr("203.0.113.8")
	ct.TryAccept(a)
	ct.RemoveConnection(a)
	time.Sleep(40 * time.Millisecond) // first attempt ages out of window
	ct.TryAccept(a)
	ct.RemoveConnection(a)
	if ok, _ := ct.CanAccept(a); !ok {
		t.Fatal("attempts spread beyond window should not ban")
	}
}
