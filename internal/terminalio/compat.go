package terminalio

import (
	"io"
	"strconv"
	"sync"
	"sync/atomic"
)

// CompatWriter adapts output written for DOS/BBS terminals to modern
// (xterm-family) terminals. It sits between the session and its
// term.Terminal, so every write the BBS makes passes through it, and it keeps
// state across writes because a colour or saved cursor set in one write
// applies to the next. Each translation is off until enabled; with both off,
// output passes through unchanged.
//
// Bold as bright (SetBoldBright): BBS art and pipe codes select the eight
// bright colours as bold plus a normal colour (|08 is ESC[1;30m, dark grey).
// DOS and retro BBS terminals render that bright; many modern terminals
// render a heavier font in the normal colour, so dark grey comes out black and
// bright green dim green. Every SGR sequence that leaves bold on with a
// standard foreground gets the matching aixterm bright code (90-97) appended,
// and one that turns bold off while keeping the colour gets the normal code
// back.
//
// DEC cursor save (SetDECCursor): ANSI.SYS saved and restored the cursor with
// ESC[s / ESC[u, and art uses them freely (ESC[s LF ESC[u is a common idiom
// for reaching the next row without losing the column). Many modern terminals
// ignore them, piling the art up at column 1. The bare forms are rewritten to
// DECSC / DECRC (ESC 7 / ESC 8), which every xterm-family terminal supports.
type CompatWriter struct {
	io.ReadWriter
	boldBright atomic.Bool
	decCursor  atomic.Bool

	mu        sync.Mutex
	bold      bool
	fg        int // 0-7: standard colour; -1: default; -2: anything else (256/truecolour, explicit bright)
	savedBold bool
	savedFg   int
}

// NewCompatWriter wraps rw with every translation disabled.
func NewCompatWriter(rw io.ReadWriter) *CompatWriter {
	return &CompatWriter{ReadWriter: rw, fg: -1, savedFg: -1}
}

// SetBoldBright turns bold-as-bright translation on or off. Colour state is
// tracked either way, so enabling it mid-session starts from what the
// terminal shows.
func (w *CompatWriter) SetBoldBright(on bool) { w.boldBright.Store(on) }

// SetDECCursor turns ESC[s / ESC[u to ESC 7 / ESC 8 translation on or off.
func (w *CompatWriter) SetDECCursor(on bool) { w.decCursor.Store(on) }

func (w *CompatWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	boldBright, decCursor := w.boldBright.Load(), w.decCursor.Load()
	var out []byte // allocated only once a sequence needs rewriting
	last := 0
	// replace swaps p[i:j] for repl in the output.
	replace := func(i, j int, repl []byte) {
		if out == nil {
			out = make([]byte, 0, len(p)+16)
		}
		out = append(out, p[last:i]...)
		out = append(out, repl...)
		last = j
	}

scan:
	for i := 0; i < len(p); i++ {
		if p[i] != 0x1b || i+1 >= len(p) {
			continue
		}
		switch p[i+1] {
		case '7': // DECSC saves colours along with the cursor
			w.savedBold, w.savedFg = w.bold, w.fg
			i++
			continue
		case '8': // DECRC restores them
			w.bold, w.fg = w.savedBold, w.savedFg
			i++
			continue
		case '[':
		default:
			continue
		}
		j := i + 2
		for j < len(p) && p[j] >= 0x30 && p[j] <= 0x3f {
			j++
		}
		if j >= len(p) {
			break scan // incomplete sequence at the end of the write: leave it
		}
		params := p[i+2 : j]
		switch p[j] {
		case 'm':
			if extra := w.applySGR(params); boldBright && extra != 0 {
				seq := append([]byte{}, p[i:j]...)
				if len(params) > 0 {
					seq = append(seq, ';')
				}
				seq = strconv.AppendInt(seq, int64(extra), 10)
				replace(i, j+1, append(seq, 'm'))
			}
		case 's':
			// Only the bare form: with parameters, CSI s is DECSLRM.
			if decCursor && len(params) == 0 {
				w.savedBold, w.savedFg = w.bold, w.fg
				replace(i, j+1, []byte("\x1b7"))
			}
		case 'u':
			// Only the bare form: with parameters, CSI u belongs to keyboard
			// protocols, not cursor restore.
			if decCursor && len(params) == 0 {
				w.bold, w.fg = w.savedBold, w.savedFg
				replace(i, j+1, []byte("\x1b8"))
			}
		}
		i = j
	}
	if out == nil {
		return w.ReadWriter.Write(p)
	}
	out = append(out, p[last:]...)
	if _, err := w.ReadWriter.Write(out); err != nil {
		return 0, err
	}
	return len(p), nil
}

// applySGR updates the tracked state from one SGR parameter list and returns
// the foreground code to append so the terminal shows the DOS colour, or 0 if
// the sequence already produces it.
func (w *CompatWriter) applySGR(params []byte) int {
	codes := splitParams(params)
	wasBold := w.bold
	setFg := false // the sequence itself chose a foreground
	for k := 0; k < len(codes); k++ {
		switch c := codes[k]; {
		case c == 0:
			w.bold, w.fg, setFg = false, -1, true
		case c == 1:
			w.bold = true
		case c == 22:
			w.bold = false
		case c >= 30 && c <= 37:
			w.fg, setFg = c-30, true
		case c == 39:
			w.fg, setFg = -1, true
		case c >= 90 && c <= 97:
			w.fg, setFg = -2, true
		case c == 38 || c == 48: // extended colour: skip its arguments
			if k+1 < len(codes) && codes[k+1] == 5 {
				k += 2
			} else if k+1 < len(codes) && codes[k+1] == 2 {
				k += 4
			}
			if c == 38 {
				w.fg, setFg = -2, true
			}
		}
	}

	switch {
	case w.bold && w.fg >= 0:
		return 90 + w.fg
	case w.bold && w.fg == -1 && (setFg || !wasBold):
		return 97 // DOS default foreground is light grey; bold makes it white
	case !w.bold && wasBold && !setFg && w.fg >= 0:
		return 30 + w.fg // bold dropped by 22: back to the normal colour
	case !w.bold && wasBold && !setFg && w.fg == -1:
		return 39
	}
	return 0
}

// splitParams parses an SGR parameter list; an empty field is 0.
func splitParams(params []byte) []int {
	if len(params) == 0 {
		return []int{0}
	}
	codes := make([]int, 0, 4)
	v := 0
	for _, b := range params {
		switch {
		case b == ';':
			codes = append(codes, v)
			v = 0
		case b >= '0' && b <= '9':
			v = v*10 + int(b-'0')
		}
	}
	return append(codes, v)
}
