package testterm

import (
	"unicode/utf8"
)

// Write feeds terminal output to the screen. It never returns an error; the
// io.Writer signature is what the code under test expects.
//
// Bytes held back mid-sequence or mid-rune are carried over to the next call,
// so a sequence split across writes is still parsed as one sequence.
func (t *Term) Write(p []byte) (int, error) {
	n := len(p)
	buf := p
	if len(t.pending) > 0 {
		buf = append(t.pending, p...)
		t.pending = nil
	}

	for len(buf) > 0 {
		b := buf[0]

		if b == 0x1b {
			seq, rest, complete := scanEscape(buf)
			if !complete {
				t.pending = append([]byte(nil), buf...)
				return n, nil
			}
			t.applyEscape(seq)
			buf = rest
			continue
		}

		if b < 0x20 {
			t.applyControl(b)
			buf = buf[1:]
			continue
		}

		r, size := t.decodeRune(buf)
		if size == 0 { // incomplete UTF-8 rune at the end of the chunk
			t.pending = append([]byte(nil), buf...)
			return n, nil
		}
		t.putRune(r)
		buf = buf[size:]
	}
	return n, nil
}

// decodeRune reads one character from the head of buf. In CP437 mode a high
// byte is one character; otherwise the stream is UTF-8. A size of 0 means the
// buffer ends mid-rune.
func (t *Term) decodeRune(buf []byte) (rune, int) {
	if t.cp437 {
		return cp437Rune(buf[0]), 1
	}
	r, size := utf8.DecodeRune(buf)
	if r == utf8.RuneError && size <= 1 && !utf8.FullRune(buf) {
		return 0, 0
	}
	return r, size
}

// scanEscape splits a CSI sequence off the head of buf. complete is false when
// buf ends before the sequence's final byte.
func scanEscape(buf []byte) (seq, rest []byte, complete bool) {
	if len(buf) < 2 {
		return nil, buf, false
	}
	if buf[1] != '[' {
		// Not CSI; consume ESC plus one byte so it cannot be printed as text.
		return buf[:2], buf[2:], true
	}
	for i := 2; i < len(buf); i++ {
		c := buf[i]
		if c >= '@' && c <= '~' {
			return buf[:i+1], buf[i+1:], true
		}
	}
	return nil, buf, false
}

// applyEscape interprets one sequence. Task 1 records everything; later tasks
// replace this body with real handling.
func (t *Term) applyEscape(seq []byte) {
	t.unhandled = append(t.unhandled, string(seq))
}

// applyControl handles the C0 control characters the editor emits.
func (t *Term) applyControl(b byte) {
	switch b {
	case '\r':
		t.col = 1
	case '\n':
		t.row++
	case '\b':
		if t.col > 1 {
			t.col--
		}
	case '\t':
		next := ((t.col-1)/8+1)*8 + 1
		t.col = next
	default:
		t.unhandled = append(t.unhandled, string([]byte{b}))
	}
}

// cp437Rune is replaced with a real decode in Task 6.
func cp437Rune(b byte) rune { return rune(b) }
