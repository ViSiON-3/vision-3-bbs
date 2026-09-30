package transfer

import (
	"bytes"
	"crypto/rand"
	"fmt"
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

// zrposHit is one header reported by zrposDetector.scan, with end made
// absolute within the whole stream.
type zrposHit struct {
	hex       bool
	end       int
	straddles bool
}

// scanReads feeds reads through a fresh zrposDetector and returns every hit.
func scanReads(reads ...[]byte) []zrposHit {
	var d zrposDetector
	var hits []zrposHit
	offset := 0
	for _, r := range reads {
		d.scan(r, func(hex bool, end int, straddles bool) {
			hits = append(hits, zrposHit{hex, offset + end, straddles})
		})
		offset += len(r)
	}
	return hits
}

// bytewise splits b into one-byte reads.
func bytewise(b []byte) [][]byte {
	reads := make([][]byte, len(b))
	for i := range b {
		reads[i] = b[i : i+1]
	}
	return reads
}

func TestZRPOSDetector_eachHeaderReportedOnce(t *testing.T) {
	for name, h := range map[string][]byte{"hex": zrposHexHeader, "binary": zrposBinHeader} {
		isHex := name == "hex"
		pre, post := []byte("lead-in data "), []byte(" trailing data")
		stream := append(append(append([]byte{}, pre...), h...), post...)
		end := len(pre) + len(h) - 1

		check := func(t *testing.T, hits []zrposHit, straddles bool) {
			t.Helper()
			if len(hits) != 1 {
				t.Fatalf("got %d hits %+v, want exactly 1", len(hits), hits)
			}
			if want := (zrposHit{isHex, end, straddles}); hits[0] != want {
				t.Errorf("hit = %+v, want %+v", hits[0], want)
			}
		}

		t.Run(name+"/in buffer", func(t *testing.T) {
			check(t, scanReads(stream), false)
		})
		// The header ends its read, so it is carried in the tail as well.
		t.Run(name+"/in tail", func(t *testing.T) {
			check(t, scanReads(stream[:end+1], stream[end+1:]), false)
		})
		t.Run(name+"/in tail then short reads", func(t *testing.T) {
			reads := append([][]byte{stream[:end+1]}, bytewise(stream[end+1:])...)
			check(t, scanReads(reads...), false)
		})
		for k := 1; k < len(h); k++ {
			split := len(pre) + k // k bytes of the header in the first read
			t.Run(fmt.Sprintf("%s/straddling %d+%d", name, k, len(h)-k), func(t *testing.T) {
				check(t, scanReads(stream[:split], stream[split:]), true)
			})
		}
		t.Run(name+"/one byte per read", func(t *testing.T) {
			check(t, scanReads(bytewise(stream)...), true)
		})
	}
}

func TestZRPOSDetector_multipleAndNonMatching(t *testing.T) {
	stream := []byte("x" + string(zrposHexHeader) + "**\x18B01" +
		string(zrposBinHeader) + string(zrposBinHeader) + "y")
	for split := 0; split <= len(stream); split++ {
		if hits := scanReads(stream[:split], stream[split:]); len(hits) != 3 {
			t.Errorf("split at %d: got %d hits %+v, want 3", split, len(hits), hits)
		}
	}
	if hits := scanReads(bytewise(stream)...); len(hits) != 3 {
		t.Errorf("one byte per read: got %d hits %+v, want 3", len(hits), hits)
	}
	// ZRINIT (hex type 01) and a binary ZDATA (0x0a) are not ZRPOS.
	if hits := scanReads([]byte("**\x18B01 *\x18A\x0a")); len(hits) != 0 {
		t.Errorf("non-ZRPOS headers reported: %+v", hits)
	}
}
