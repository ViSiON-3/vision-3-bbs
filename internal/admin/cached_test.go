package admin

import (
	"testing"
	"time"
)

func TestCachedIntRecomputesAfterTTL(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	timeNow = func() time.Time { return now }
	defer func() { timeNow = time.Now }()

	calls := 0
	get := CachedInt(10*time.Second, func() int { calls++; return calls })

	if get() != 1 || calls != 1 {
		t.Fatalf("first call must compute: calls=%d", calls)
	}
	if get() != 1 || calls != 1 {
		t.Fatalf("second call must hit the cache: calls=%d", calls)
	}
	now = now.Add(9 * time.Second)
	if get() != 1 || calls != 1 {
		t.Fatalf("within ttl must not recompute: calls=%d", calls)
	}
	now = now.Add(time.Second)
	if get() != 2 || calls != 2 {
		t.Fatalf("after ttl must recompute: calls=%d", calls)
	}
}
