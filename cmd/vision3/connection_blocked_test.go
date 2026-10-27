package main

import "testing"

func TestIsBlocked(t *testing.T) {
	ct := newRateTracker()
	if ct.IsBlocked("203.0.113.9") {
		t.Fatal("blocked with no blocklist loaded")
	}
	ct.blocklist = &IPList{ips: map[string]bool{"203.0.113.9": true, "203.0.113.10": true}}
	if !ct.IsBlocked("203.0.113.9") {
		t.Error("blocklisted IP not blocked")
	}
	if ct.IsBlocked("198.51.100.1") {
		t.Error("unlisted IP blocked")
	}
	// The allowlist wins, as it does at connection accept.
	ct.allowlist = &IPList{ips: map[string]bool{"203.0.113.10": true}}
	if ct.IsBlocked("203.0.113.10") {
		t.Error("allowlisted IP blocked")
	}
}
