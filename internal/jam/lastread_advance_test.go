package jam

import (
	"path/filepath"
	"testing"
)

// TestAdvanceLastRead covers the forward-only pointer the message reader uses:
// reading an older message must not rewind lastread, or the login new-mail
// scan (which counts from lastread+1) reports already-read mail as new.
func TestAdvanceLastRead(t *testing.T) {
	b, err := Open(filepath.Join(t.TempDir(), "advance"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer b.Close()

	check := func(step string, wantLast, wantHigh uint32) {
		t.Helper()
		lr, err := b.GetLastRead("sysop")
		if err != nil {
			t.Fatalf("%s: GetLastRead: %v", step, err)
		}
		if lr.LastReadMsg != wantLast || lr.HighReadMsg != wantHigh {
			t.Fatalf("%s: lastread = %d/%d, want %d/%d", step, lr.LastReadMsg, lr.HighReadMsg, wantLast, wantHigh)
		}
	}

	if err := b.AdvanceLastRead("sysop", 3); err != nil {
		t.Fatalf("first advance: %v", err)
	}
	check("no record yet", 3, 3)

	if err := b.AdvanceLastRead("sysop", 7); err != nil {
		t.Fatalf("forward: %v", err)
	}
	check("forward", 7, 7)

	if err := b.AdvanceLastRead("sysop", 2); err != nil {
		t.Fatalf("backward: %v", err)
	}
	check("backward is a no-op", 7, 7)

	// A pointer deliberately set back (newscan jump, "update pointers: no"
	// restore) keeps its high-water mark when the reader then advances it.
	if err := b.SetLastRead("sysop", 4, 7); err != nil {
		t.Fatalf("SetLastRead: %v", err)
	}
	if err := b.AdvanceLastRead("sysop", 5); err != nil {
		t.Fatalf("advance after rewind: %v", err)
	}
	check("advance after rewind", 5, 7)
}

// Each node opens its own Base handle, so AdvanceLastRead must serialize on
// the file lock: concurrent advances through separate handles must still end
// at the highest message number.
func TestAdvanceLastRead_ConcurrentHandles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "race")
	seed, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	seed.Close()

	const n = 20
	errs := make(chan error, n)
	for i := 1; i <= n; i++ {
		go func(msgNum int) {
			b, err := Open(path)
			if err != nil {
				errs <- err
				return
			}
			defer b.Close()
			errs <- b.AdvanceLastRead("sysop", msgNum)
		}(i)
	}
	for i := 0; i < n; i++ {
		if err := <-errs; err != nil {
			t.Fatalf("AdvanceLastRead: %v", err)
		}
	}

	b, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer b.Close()
	lr, err := b.GetLastRead("sysop")
	if err != nil {
		t.Fatalf("GetLastRead: %v", err)
	}
	if lr.LastReadMsg != n {
		t.Errorf("LastReadMsg = %d, want %d", lr.LastReadMsg, n)
	}
}
