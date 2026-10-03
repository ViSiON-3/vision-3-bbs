package editor

import (
	"bytes"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
)

// ReadCursorReport waits up to timeout for the terminal's reply to a cursor
// position request (ESC[6n). Anything else that arrives first, such as keys
// the caller typed, is pushed back for the next read.
func (ih *InputHandler) ReadCursorReport(timeout time.Duration) (row, col int, ok bool) {
	deadline := time.Now().Add(timeout)
	var got []byte
	for {
		wait := time.Until(deadline)
		if wait <= 0 {
			break
		}
		b, err := ih.readByteWithTimeout(wait)
		if err != nil {
			break
		}
		got = append(got, b)
		if b != 'R' {
			continue
		}
		start := bytes.LastIndexByte(got, 0x1b)
		if start < 0 {
			continue
		}
		if row, col, ok = ansi.ParseCPR(got[start:]); ok {
			got = got[:start]
			break
		}
	}
	ih.unreadBuf = append(got, ih.unreadBuf...)
	return row, col, ok
}
