package editor

import (
	"io"
	"testing"
	"time"
)

// PendingByte shows the next byte without taking it: the read after it
// still returns the key.
func TestPendingByteLeavesTheKey(t *testing.T) {
	pr, pw := io.Pipe()
	ih := NewInputHandler(pr)
	defer ih.Close()

	if _, ok := ih.PendingByte(20 * time.Millisecond); ok {
		t.Fatal("a byte was pending before anything was typed")
	}
	go func() { _, _ = pw.Write([]byte("x")) }()
	b, ok := ih.PendingByte(time.Second)
	if !ok || b != 'x' {
		t.Fatalf("PendingByte = %q, %v; want 'x', true", b, ok)
	}
	if b, ok := ih.PendingByte(0); !ok || b != 'x' {
		t.Fatalf("second PendingByte = %q, %v; want 'x' again", b, ok)
	}
	key, err := ih.ReadKey()
	if err != nil || key != 'x' {
		t.Fatalf("ReadKey = %q, %v; want 'x'", key, err)
	}
	_ = pw.Close()
	if _, ok := ih.PendingByte(time.Second); ok {
		t.Error("a closed connection reported a pending byte")
	}
}
