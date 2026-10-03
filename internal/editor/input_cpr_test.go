package editor

import (
	"bytes"
	"io"
	"testing"
	"time"
)

func TestReadCursorReport(t *testing.T) {
	ih := NewInputHandler(bytes.NewReader([]byte("\x1b[5;2R")))
	row, col, ok := ih.ReadCursorReport(time.Second)
	if !ok || row != 5 || col != 2 {
		t.Fatalf("ReadCursorReport = (%d, %d, %v), want (5, 2, true)", row, col, ok)
	}
}

// Keys the caller typed before the report arrived are left for the next read.
func TestReadCursorReportKeepsEarlierKeys(t *testing.T) {
	ih := NewInputHandler(bytes.NewReader([]byte("ab\x1b[5;4Rc")))
	if _, col, ok := ih.ReadCursorReport(time.Second); !ok || col != 4 {
		t.Fatalf("ReadCursorReport col = %d, ok = %v; want 4, true", col, ok)
	}
	for _, want := range []int{'a', 'b', 'c'} {
		if k, err := ih.ReadKey(); err != nil || k != want {
			t.Fatalf("ReadKey = %q, err = %v; want %q", k, err, want)
		}
	}
}

// A terminal that never answers costs the timeout and nothing more, and any
// bytes it did send stay readable.
func TestReadCursorReportTimesOut(t *testing.T) {
	pr, pw := io.Pipe()
	defer pw.Close()
	go func() { _, _ = pw.Write([]byte("x")) }()
	ih := NewInputHandler(pr)

	start := time.Now()
	_, _, ok := ih.ReadCursorReport(100 * time.Millisecond)
	if ok {
		t.Fatal("ReadCursorReport reported a position with no reply")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("ReadCursorReport took %v, want about 100ms", elapsed)
	}
	if k, err := ih.ReadKey(); err != nil || k != 'x' {
		t.Fatalf("ReadKey = %q, err = %v; want 'x'", k, err)
	}
}

func TestReadCursorReportEOF(t *testing.T) {
	ih := NewInputHandler(bytes.NewReader(nil))
	if _, _, ok := ih.ReadCursorReport(time.Second); ok {
		t.Fatal("ReadCursorReport reported a position at EOF")
	}
}
