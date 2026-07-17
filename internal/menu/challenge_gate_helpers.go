package menu

import (
	"errors"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/editor"
)

// parseChallengeKey maps a config key string to the key code the challenge
// loop compares against. "ESC" (any case) or empty -> editor.KeyEsc; otherwise
// the first rune of the string.
func parseChallengeKey(s string) int {
	if s == "" || strings.EqualFold(s, "ESC") {
		return editor.KeyEsc
	}
	for _, r := range s { // first rune
		return int(r)
	}
	return editor.KeyEsc
}

// findCountdownField scans prompt bytes as a terminal would, tracking the
// visual cursor position, and returns the 1-based (row, col) and width of the
// first run of '#' characters. ANSI CSI (ESC[...) and SS3 (ESC O x) escape
// sequences are skipped without advancing the column, so color codes preceding
// the field do not offset it. \r resets the column, \n advances the row.
// found is false if there is no '#' run.
//
// Limitation: cursor-movement escape sequences before the field are treated as
// zero-width (like SGR); prompt art for the gate is expected to use plain text
// plus color codes, matching botgate's own assumptions.
func findCountdownField(prompt []byte) (row, col, width int, found bool) {
	row, col = 1, 1
	for i := 0; i < len(prompt); i++ {
		b := prompt[i]
		switch b {
		case 0x1B: // ESC — skip an escape sequence
			i += escapeSeqLen(prompt[i:]) - 1 // -1: loop's i++ advances past the last byte
		case '\r':
			col = 1
		case '\n':
			row++
		case '#':
			w := 0
			for i+w < len(prompt) && prompt[i+w] == '#' {
				w++
			}
			return row, col, w, true
		default:
			col++
		}
	}
	return 0, 0, 0, false
}

// escapeSeqLen returns the byte length of the escape sequence at the start of b
// (which begins with ESC). Handles CSI (ESC [ ... final 0x40-0x7E), SS3
// (ESC O x), and a lone/unknown ESC (length 1).
func escapeSeqLen(b []byte) int {
	if len(b) < 2 {
		return 1
	}
	switch b[1] {
	case '[': // CSI: params/intermediates until a final byte 0x40-0x7E
		n := 2
		for n < len(b) {
			c := b[n]
			n++
			if c >= 0x40 && c <= 0x7E {
				break
			}
		}
		return n
	case 'O': // SS3: ESC O <one byte>
		if len(b) >= 3 {
			return 3
		}
		return 2
	default:
		return 1
	}
}

// formatCountdownValue renders seconds right-aligned to width. If the number is
// wider than width it is returned unpadded.
func formatCountdownValue(seconds, width int) string {
	s := strconv.Itoa(seconds)
	if len(s) >= width {
		return s
	}
	return strings.Repeat(" ", width-len(s)) + s
}

// substituteCountdown replaces the first run of '#' in prompt with the
// right-aligned seconds value. Used for the initial (and static-mode) draw.
func substituteCountdown(prompt []byte, seconds, width int) []byte {
	start := -1
	for i := 0; i < len(prompt); i++ {
		if prompt[i] == '#' {
			start = i
			break
		}
	}
	if start < 0 {
		return prompt
	}
	end := start
	for end < len(prompt) && prompt[end] == '#' {
		end++
	}
	out := make([]byte, 0, len(prompt))
	out = append(out, prompt[:start]...)
	out = append(out, []byte(formatCountdownValue(seconds, end-start))...)
	out = append(out, prompt[end:]...)
	return out
}

// challengeInput is the subset of editor.InputHandler the loop needs.
type challengeInput interface {
	ReadKeyWithTimeout(d time.Duration) (int, error)
}

// runChallengeLoop drives the gate decision. It returns (true, nil) once
// matchKey has been read `required` times, (false, nil) on deadline or on a
// flood of `strayLimit` non-matching keys, and (false, io.EOF) if the caller
// disconnects. `now` and `tick` are injected for testability; `onTick` runs on
// each idle second (used to redraw the live countdown).
func runChallengeLoop(in challengeInput, now func() time.Time, deadline time.Time,
	matchKey, required, strayLimit int, tick time.Duration, onTick func()) (bool, error) {
	matches, stray := 0, 0
	for {
		cur := now()
		if !cur.Before(deadline) {
			return false, nil // timed out
		}
		remaining := deadline.Sub(cur)
		wait := tick
		if remaining < wait {
			wait = remaining
		}
		k, err := in.ReadKeyWithTimeout(wait)
		if err != nil {
			if errors.Is(err, editor.ErrIdleTimeout) {
				onTick()
				continue
			}
			if errors.Is(err, io.EOF) {
				return false, io.EOF
			}
			return false, err
		}
		if k == matchKey {
			matches++
			if matches >= required {
				return true, nil
			}
			continue
		}
		stray++
		if stray >= strayLimit {
			return false, nil // scripted-payload flood
		}
	}
}
