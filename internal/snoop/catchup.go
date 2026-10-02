// Package snoop lets the WFC console watch a caller's session: a Tap copies
// everything written to the caller to any attached sysop, keeps the bytes
// since the last clear-screen so a new watcher sees the current screen, and
// merges sysop keystrokes into the caller's input while type-in or chat is on.
package snoop

import "bytes"

// CatchupLimit bounds the bytes kept for a newly attached watcher.
const CatchupLimit = 64 << 10

var clearScreen = []byte("\x1b[2J")

// catchup holds the output since the last clear-screen; snapshot returns at
// most limit bytes of it. The buffer may grow to twice limit before it is
// cut back, so a long screen costs one copy per limit bytes written rather
// than one per write. It is not safe for concurrent use; Tap serialises
// access.
type catchup struct {
	limit   int
	buf     []byte
	dropped bool // bytes since the last clear-screen were cut
}

func newCatchup(limit int) *catchup { return &catchup{limit: limit} }

// write appends p. A clear-screen anywhere in the buffer (including one
// split across writes) drops everything before it.
func (c *catchup) write(p []byte) {
	// Search from far enough back to catch a sequence split across writes.
	from := len(c.buf) - (len(clearScreen) - 1)
	if from < 0 {
		from = 0
	}
	c.buf = append(c.buf, p...)
	if i := bytes.LastIndex(c.buf[from:], clearScreen); i >= 0 {
		c.buf = append(c.buf[:0], c.buf[from+i:]...)
		c.dropped = false
	}
	if len(c.buf) > 2*c.limit {
		c.buf = append(c.buf[:0], c.buf[len(c.buf)-c.limit:]...)
		c.dropped = true
	}
}

// snapshot returns a copy of the last limit bytes and whether any bytes
// since the last clear-screen are missing from it.
func (c *catchup) snapshot() ([]byte, bool) {
	if len(c.buf) > c.limit {
		return bytes.Clone(c.buf[len(c.buf)-c.limit:]), true
	}
	return bytes.Clone(c.buf), c.dropped
}
