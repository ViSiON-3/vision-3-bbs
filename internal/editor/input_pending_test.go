package editor

import (
	"errors"
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

	if _, ok, err := ih.PendingByte(20 * time.Millisecond); ok || err != nil {
		t.Fatalf("before anything was typed: ok %v, err %v", ok, err)
	}
	go func() { _, _ = pw.Write([]byte("x")) }()
	b, ok, err := ih.PendingByte(time.Second)
	if !ok || b != 'x' || err != nil {
		t.Fatalf("PendingByte = %q, %v, %v; want 'x', true, nil", b, ok, err)
	}
	if b, ok, _ := ih.PendingByte(0); !ok || b != 'x' {
		t.Fatalf("second PendingByte = %q, %v; want 'x' again", b, ok)
	}
	key, err := ih.ReadKey()
	if err != nil || key != 'x' {
		t.Fatalf("ReadKey = %q, %v; want 'x'", key, err)
	}
	_ = pw.Close()
	if _, ok, err := ih.PendingByte(time.Second); ok || !errors.Is(err, io.EOF) {
		t.Errorf("closed connection: ok %v, err %v; want false, io.EOF", ok, err)
	}
	// and again, without waiting
	if _, _, err := ih.PendingByte(0); !errors.Is(err, io.EOF) {
		t.Errorf("closed connection, second look: err %v; want io.EOF", err)
	}
}

// A session ended with Fail reports its error rather than a pending key.
func TestPendingByteReportsFail(t *testing.T) {
	pr, pw := io.Pipe()
	defer pw.Close()
	ih := NewInputHandler(pr)
	defer ih.Close()
	go func() { _, _ = pw.Write([]byte("x")) }()
	if _, ok, _ := ih.PendingByte(time.Second); !ok {
		t.Fatal("no byte pending")
	}
	ih.Fail(ErrTimeLimit)
	if _, ok, err := ih.PendingByte(0); ok || !errors.Is(err, ErrTimeLimit) {
		t.Errorf("after Fail: ok %v, err %v; want false, ErrTimeLimit", ok, err)
	}
}
