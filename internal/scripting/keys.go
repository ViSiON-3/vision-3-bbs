package scripting

import (
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/editor"
)

// csiKeyName names the key a complete CSI sequence (ESC [ ... final) stands
// for, from the editor's key table, or returns "" for one that is not a key.
func csiKeyName(seq []byte) string {
	if len(seq) < 3 {
		return ""
	}
	return editor.KeyName(editor.CSIKey(seq[2:len(seq)-1], seq[len(seq)-1]))
}

// ss3KeyName names the key for ESC O <b>, or returns "" for one that is not
// a key.
func ss3KeyName(b byte) string {
	return editor.KeyName(editor.SS3Key(b))
}

// readNamedKey reads one key like readKey, but reports special keys by name
// instead of discarding them: "UP", "DOWN", "LEFT", "RIGHT", "HOME", "END",
// "PGUP", "PGDN", "INS", "DEL", "F1"-"F12", "BACKTAB" and "ESC". Any other
// key comes back as the character, as from readKey: "\r" for Enter, "\x08"
// or "\x7f" for Backspace. An escape sequence that is not a key is skipped.
// It returns "" on timeout (timeout 0 waits forever).
//
// The keys and their names come from the editor's table (editor.CSIKey,
// editor.KeyName), so readkey and the editor read every key the same way. A
// lone ESC is held for editor.DefaultEscTimeout, the editor's window, in case
// the rest of a sequence is on its way; if nothing follows, it is the Esc key.
func (eng *Engine) readNamedKey(timeout time.Duration) (string, error) {
	eng.holdEscape = true
	defer func() { eng.holdEscape = false }()
	var deadline time.Time
	if timeout > 0 {
		deadline = time.Now().Add(timeout)
	}
	for {
		left := time.Duration(0)
		if !deadline.IsZero() {
			if left = time.Until(deadline); left <= 0 {
				return "", nil
			}
		}
		eng.keyName = ""
		key, _, err := eng.readKeyEcho(left)
		if err != nil {
			return "", err
		}
		if eng.keyName != "" {
			return eng.keyName, nil
		}
		if key == "\x1b" {
			seq, rest, err := eng.completeEscape()
			if err != nil {
				return "", err
			}
			eng.inputBuf = append(rest, eng.inputBuf...)
			if len(seq) == 1 || escapePending(seq) {
				// A lone ESC, or a sequence cut short or abandoned:
				// the Esc key, as the editor reads it. The bytes of an
				// unfinished sequence are dropped.
				return "ESC", nil
			}
			eng.inputBuf = append(seq, eng.inputBuf...)
			continue
		}
		if key != "" {
			return key, nil
		}
		// A sequence that names no key, or the timeout: readKeyEcho
		// returns "" for both. Only the deadline tells them apart.
		if !deadline.IsZero() && time.Until(deadline) <= 0 {
			return "", nil
		}
	}
}

// completeEscape gathers the bytes after a lone ESC until they make a whole
// escape sequence, waiting editor.DefaultEscTimeout for each further piece. It
// returns the sequence with the ESC in front, and any bytes that arrived after
// it. The sequence is unfinished (escapePending still true) when nothing more
// came in time, when a byte that can't be part of it arrived (that byte and
// what follows are returned as rest), or when it reached
// editor.MaxCSILength.
func (eng *Engine) completeEscape() (seq, rest []byte, err error) {
	seq = []byte{0x1b}
	for escapePending(seq) {
		more, err := eng.awaitInput(editor.DefaultEscTimeout)
		if err != nil {
			return nil, nil, err
		}
		if len(more) == 0 {
			return seq, nil, nil
		}
		for i, c := range more {
			if len(seq) > 2 && seq[1] == '[' && (c < 0x20 || c > 0x7e) {
				// Enter or a control key typed after a cut-off sequence
				// is the next key, not part of this one.
				return seq, more[i:], nil
			}
			seq = append(seq, c)
			if !escapePending(seq) || len(seq)-2 >= editor.MaxCSILength {
				return seq, more[i+1:], nil
			}
		}
	}
	return seq, nil, nil
}

// escapePending reports whether b, starting with ESC, is the start of an
// escape sequence whose remaining bytes have not arrived: ESC alone, ESC [
// or ESC O, or a CSI still in its parameter bytes.
func escapePending(b []byte) bool {
	switch {
	case len(b) == 1:
		return true
	case b[1] == 'O':
		return len(b) == 2
	case b[1] != '[':
		return false
	}
	for _, c := range b[2:] {
		if c >= 0x40 && c <= 0x7e {
			return false
		}
	}
	return true
}

// awaitInput returns the next raw input bytes, waiting up to d for them, or
// nil if none come. Buffered leftovers are returned at once. A disconnect is
// returned as ErrDisconnect, as from readKeyEcho.
func (eng *Engine) awaitInput(d time.Duration) ([]byte, error) {
	if len(eng.inputBuf) > 0 {
		data := eng.inputBuf
		eng.inputBuf = nil
		return data, nil
	}
	eng.startReader()
	if eng.readRequests != nil && !eng.readPending {
		eng.readRequests <- struct{}{}
		eng.readPending = true
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case result := <-eng.rawInputCh:
		eng.readPending = false
		if result.err != nil {
			eng.cancel()
			return nil, ErrDisconnect
		}
		return result.data, nil
	case <-timer.C:
		return nil, nil
	case <-eng.ctx.Done():
		return nil, ErrTerminated
	}
}
