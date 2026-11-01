package editor

import (
	"io"
	"testing"
	"time"
)

// stallingReader hands out its chunks, pausing before each chunk after the
// first, as a stalled reader goroutine or a slow link would. A chunk larger
// than the caller's buffer is handed out over several reads with no pause.
type stallingReader struct {
	chunks []string
	pause  time.Duration
	n      int
	rest   string
}

func (r *stallingReader) Read(p []byte) (int, error) {
	if r.rest == "" {
		if r.n >= len(r.chunks) {
			return 0, io.EOF
		}
		if r.n > 0 {
			time.Sleep(r.pause)
		}
		r.rest = r.chunks[r.n]
		r.n++
	}
	k := copy(p, r.rest)
	r.rest = r.rest[k:]
	return k, nil
}

// #844: the bytes after "ESC [" get the same allowance as the byte after ESC.
// They waited only 100 ms, so PageUp whose "5~" came 150 ms late was read as
// a bare ESC, which leaves chat and other screens.
func TestCSISequenceSurvivesAStallAfterTheBracket(t *testing.T) {
	for name, chunks := range map[string][]string{
		"stall after ESC [": {"\x1b[", "5~"},
		"stall inside":      {"\x1b[5", "~"},
		"stall after ESC":   {"\x1b", "[5~"},
	} {
		t.Run(name, func(t *testing.T) {
			ih := NewInputHandler(&stallingReader{chunks: chunks, pause: 150 * time.Millisecond})
			key, err := ih.ReadKey()
			if err != nil {
				t.Fatalf("ReadKey: %v", err)
			}
			if key != KeyPageUp {
				t.Errorf("key = %d, want KeyPageUp (%d); KeyEsc is %d", key, KeyPageUp, KeyEsc)
			}
		})
	}
}

// A lone ESC is still a plain ESC once the window passes.
func TestBareEscStillTimesOut(t *testing.T) {
	ih := NewInputHandler(&stallingReader{chunks: []string{"\x1b", "x"}, pause: time.Second})
	ih.SetEscTimeout(50 * time.Millisecond)
	if key, err := ih.ReadKey(); err != nil || key != int(KeyEsc) {
		t.Errorf("ReadKey = (%d, %v), want (KeyEsc, nil)", key, err)
	}
}
