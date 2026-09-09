package menu

import (
	"path/filepath"
	"testing"
)

// TestSysopNoticeQueueRoundTrip covers enqueue → drain: notices accumulate per
// user, drain returns them in order and clears the queue, and a second drain is
// empty. A missing file drains empty rather than erroring.
func TestSysopNoticeQueueRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sysop_notices.json")

	// Draining a never-written store is empty, not an error.
	if got, err := drainSysopNotices(path, 1); err != nil || len(got) != 0 {
		t.Fatalf("drain of empty store = (%v, %v), want (nil, no error)", got, err)
	}

	for _, text := range []string{"first", "second"} {
		if err := enqueueSysopNotice(path, 1, text); err != nil {
			t.Fatalf("enqueue %q: %v", text, err)
		}
	}
	// A different user's queue is independent.
	if err := enqueueSysopNotice(path, 2, "other"); err != nil {
		t.Fatalf("enqueue for user 2: %v", err)
	}

	got, err := drainSysopNotices(path, 1)
	if err != nil {
		t.Fatalf("drain user 1: %v", err)
	}
	if len(got) != 2 || got[0].Text != "first" || got[1].Text != "second" {
		t.Fatalf("drain user 1 = %v, want [first second] in order", got)
	}

	// Draining again yields nothing — each notice is shown once.
	if again, _ := drainSysopNotices(path, 1); len(again) != 0 {
		t.Errorf("second drain returned %v, want empty", again)
	}

	// User 2's queue is untouched by user 1's drain.
	if other, _ := drainSysopNotices(path, 2); len(other) != 1 || other[0].Text != "other" {
		t.Errorf("user 2 drain = %v, want [other]", other)
	}
}
