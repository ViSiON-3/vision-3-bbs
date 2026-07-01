package qwkapi

import (
	"testing"
	"time"
)

func TestLimiter_AllowsThenBlocks(t *testing.T) {
	l := newLimiter(2, time.Minute)
	if !l.allow("a") || !l.allow("a") {
		t.Fatal("first two should be allowed")
	}
	if l.allow("a") {
		t.Error("third should be blocked")
	}
	if !l.allow("b") {
		t.Error("different key should be allowed")
	}
}
