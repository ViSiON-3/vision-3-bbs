package timeleft

import (
	"testing"
	"time"
)

func TestRemaining(t *testing.T) {
	start := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	now := start.Add(20 * time.Minute)
	if _, ok := Remaining(0, start, now, 0); ok {
		t.Fatal("no limit reported as limited")
	}
	if got, _ := Remaining(30, start, now, 0); got != 10*time.Minute {
		t.Errorf("no credit: %v", got)
	}
	if got, _ := Remaining(30, start, now, 5*time.Minute); got != 15*time.Minute {
		t.Errorf("credit: %v", got)
	}
	if got, _ := Remaining(10, start, now, 0); got != 0 {
		t.Errorf("past limit: %v", got)
	}
}

func TestMinutes(t *testing.T) {
	start := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	now := start.Add(20 * time.Minute)
	if got, _ := Minutes(30, start, now, 5*time.Minute); got != 15 {
		t.Errorf("credit: %d", got)
	}
	if got, _ := Minutes(10, start, now, 0); got != 0 {
		t.Errorf("past limit: %d", got)
	}
	if _, ok := Minutes(-1, start, now, 0); ok {
		t.Fatal("no limit reported as limited")
	}
}
