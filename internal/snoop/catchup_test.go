package snoop

import (
	"bytes"
	"testing"
)

func TestCatchupKeepsBytesSinceLastClear(t *testing.T) {
	c := newCatchup(64)
	c.write([]byte("old screen"))
	c.write([]byte("junk\x1b[2Jnew "))
	c.write([]byte("screen"))
	got, over := c.snapshot()
	if want := "\x1b[2Jnew screen"; string(got) != want || over {
		t.Fatalf("snapshot = %q, %v; want %q, false", got, over, want)
	}
}

func TestCatchupClearSplitAcrossWrites(t *testing.T) {
	c := newCatchup(64)
	c.write([]byte("old\x1b["))
	c.write([]byte("2Jnew"))
	got, _ := c.snapshot()
	if !bytes.HasPrefix(got, []byte("\x1b[2J")) || !bytes.HasSuffix(got, []byte("new")) {
		t.Fatalf("snapshot = %q; want to start at the clear", got)
	}
}

func TestCatchupOverflowKeepsTail(t *testing.T) {
	c := newCatchup(8)
	c.write([]byte("0123456789AB"))
	got, over := c.snapshot()
	if string(got) != "456789AB" || !over {
		t.Fatalf("snapshot = %q, %v; want %q, true", got, over, "456789AB")
	}
	c.write([]byte("\x1b[2J"))
	if _, over := c.snapshot(); over {
		t.Fatal("clear-screen must reset the overflow flag")
	}
}

func TestCatchupSnapshotIsACopy(t *testing.T) {
	c := newCatchup(64)
	c.write([]byte("abc"))
	got, _ := c.snapshot()
	got[0] = 'X'
	again, _ := c.snapshot()
	if string(again) != "abc" {
		t.Fatalf("snapshot aliased internal buffer: %q", again)
	}
}
