// Package snoop lets the WFC console watch a caller's session: a Tap copies
// everything written to the caller to any attached sysop, keeps the bytes
// since the last clear-screen so a new watcher sees the current screen, and
// merges sysop keystrokes into the caller's input while type-in or chat is on.
package snoop

import "bytes"

// CatchupLimit bounds the bytes kept for a newly attached watcher.
const CatchupLimit = 64 << 10

var clearScreen = []byte("\x1b[2J")

// catchup holds the output since the last clear-screen, capped at limit.
// It is not safe for concurrent use; Tap serialises access.
type catchup struct {
	limit      int
	buf        []byte
	overflowed bool
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
		c.overflowed = false
	}
	if len(c.buf) > c.limit {
		c.buf = append(c.buf[:0], c.buf[len(c.buf)-c.limit:]...)
		c.overflowed = true
	}
}

// snapshot returns a copy of the buffer and whether it lost bytes.
func (c *catchup) snapshot() ([]byte, bool) {
	return bytes.Clone(c.buf), c.overflowed
}
