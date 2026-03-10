package transfer

import (
	"bytes"
	"crypto/rand"
	"io"
	"sync/atomic"
	"testing"
)

func TestAdaptiveCopy_basic(t *testing.T) {
	data := make([]byte, 100*1024) // 100 KB
	rand.Read(data)

	var dst bytes.Buffer
	n, err := adaptiveCopy(&dst, bytes.NewReader(data), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != int64(len(data)) {
		t.Errorf("want %d bytes, got %d", len(data), n)
	}
	if !bytes.Equal(dst.Bytes(), data) {
		t.Error("copied data does not match source")
	}
}

func TestAdaptiveCopy_empty(t *testing.T) {
	var dst bytes.Buffer
	n, err := adaptiveCopy(&dst, bytes.NewReader(nil), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 0 {
		t.Errorf("want 0 bytes, got %d", n)
	}
}

func TestAdaptiveCopy_ramps_up(t *testing.T) {
	// Write enough data that adaptiveCopy should ramp from 4K to 8K.
	// At 50 writes per level, 50 * 4096 = 200 KB to trigger first ramp.
	data := make([]byte, 300*1024)
	rand.Read(data)

	var dst bytes.Buffer
	n, err := adaptiveCopy(&dst, bytes.NewReader(data), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != int64(len(data)) {
		t.Errorf("want %d bytes, got %d", len(data), n)
	}
	if !bytes.Equal(dst.Bytes(), data) {
		t.Error("copied data does not match source")
	}
}

func TestAdaptiveCopy_backoff(t *testing.T) {
	data := make([]byte, 300*1024)
	rand.Read(data)

	var backoff atomic.Int32
	// Simulate a ZRPOS signal partway through the transfer.
	// We set it before starting — adaptiveCopy should detect it immediately.
	backoff.Store(1)

	var dst bytes.Buffer
	n, err := adaptiveCopy(&dst, bytes.NewReader(data), &backoff)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != int64(len(data)) {
		t.Errorf("want %d bytes, got %d", len(data), n)
	}
	if !bytes.Equal(dst.Bytes(), data) {
		t.Error("copied data does not match source after backoff")
	}
}

func TestAdaptiveCopy_multiple_backoffs(t *testing.T) {
	// Verify data integrity with multiple backoff signals.
	data := make([]byte, 500*1024)
	rand.Read(data)

	var backoff atomic.Int32
	backoff.Store(3) // simulate 3 ZRPOS events

	var dst bytes.Buffer
	n, err := adaptiveCopy(&dst, bytes.NewReader(data), &backoff)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != int64(len(data)) {
		t.Errorf("want %d bytes, got %d", len(data), n)
	}
	if !bytes.Equal(dst.Bytes(), data) {
		t.Error("copied data does not match source after multiple backoffs")
	}
}

func TestAdaptiveCopy_write_error(t *testing.T) {
	data := make([]byte, 10*1024)
	rand.Read(data)

	errWrite := io.ErrClosedPipe
	failWriter := &failAfterN{max: 4096, err: errWrite}

	_, err := adaptiveCopy(failWriter, bytes.NewReader(data), nil)
	if err != errWrite {
		t.Errorf("want %v, got %v", errWrite, err)
	}
}

// failAfterN writes up to max bytes then returns err.
type failAfterN struct {
	written int
	max     int
	err     error
}

func (f *failAfterN) Write(p []byte) (int, error) {
	if f.written+len(p) > f.max {
		return 0, f.err
	}
	f.written += len(p)
	return len(p), nil
}
