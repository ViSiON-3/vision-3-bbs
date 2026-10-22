package menu

// chatScrollbackLines is how many display lines of chat each session keeps
// for paging back with PgUp.
const chatScrollbackLines = 500

// chatScrollback holds the lines written to the chat region, as wrapped
// display lines in pipe-code form, so the caller can page back through them
// and so the region can be redrawn after its size changes. It keeps the last
// capacity lines. offset is how many lines the view sits above the newest
// line; 0 is the live view, where new lines are drawn as they arrive.
//
// It is not safe for concurrent use; the chat session guards it with rawMu.
type chatScrollback struct {
	capacity int
	lines    []string
	offset   int
}

// add appends lines, dropping the oldest beyond capacity. While the view is
// scrolled back it stays on the lines it shows.
func (b *chatScrollback) add(lines ...string) {
	b.lines = append(b.lines, lines...)
	if over := len(b.lines) - b.capacity; over > 0 {
		b.lines = append(b.lines[:0], b.lines[over:]...)
	}
	if b.offset > 0 {
		b.offset = min(b.offset+len(lines), len(b.lines))
	}
}

// view returns the lines a region h rows tall shows: up to h lines, ending
// offset lines above the newest. The slice shares the buffer's storage and
// is only valid until the next add.
func (b *chatScrollback) view(h int) []string {
	b.offset = min(b.offset, b.maxOffset(h))
	end := len(b.lines) - b.offset
	return b.lines[max(0, end-h):end]
}

// page moves the view a page of an h-row region up (dir < 0) or down
// (dir > 0). A page is h-1 lines, so one line of context stays in view. It
// reports whether the view moved.
func (b *chatScrollback) page(dir, h int) bool {
	step := max(1, h-1)
	old := b.offset
	if dir < 0 {
		b.offset = min(b.offset+step, b.maxOffset(h))
	} else {
		b.offset = max(0, b.offset-step)
	}
	return b.offset != old
}

// maxOffset is the offset that shows the oldest line at the top of an h-row
// region.
func (b *chatScrollback) maxOffset(h int) int { return max(0, len(b.lines)-h) }

// live reports whether the view is on the newest line.
func (b *chatScrollback) live() bool { return b.offset == 0 }

// toLive returns the view to the newest line and reports whether it moved.
func (b *chatScrollback) toLive() bool {
	moved := b.offset != 0
	b.offset = 0
	return moved
}

// len returns how many lines are held.
func (b *chatScrollback) len() int { return len(b.lines) }

// reset empties the buffer and returns to the live view.
func (b *chatScrollback) reset() {
	b.lines = nil
	b.offset = 0
}
