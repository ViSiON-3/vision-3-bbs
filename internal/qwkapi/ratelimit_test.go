package qwkapi

import (
	"testing"
	"time"
)

func TestLimiter_AllowsThenBlocks(t *testing.T) {
	l := newLimiter(2, time.Minute)
	if !l.allow("a") {
		t.Fatal("first should be allowed")
	}
	if !l.allow("a") {
		t.Fatal("second should be allowed")
	}
	if l.allow("a") {
		t.Error("third should be blocked")
	}
	if !l.allow("b") {
		t.Error("different key should be allowed")
	}
}

func TestLimiter_SweepPrunesElapsedWindows(t *testing.T) {
	l := newLimiter(5, 15*time.Millisecond)
	l.allow("a")
	l.allow("b")
	if l.size() != 2 {
		t.Fatalf("size = %d, want 2", l.size())
	}
	time.Sleep(25 * time.Millisecond) // let the windows elapse
	l.sweep()
	if l.size() != 0 {
		t.Errorf("after sweep size = %d, want 0", l.size())
	}
}
