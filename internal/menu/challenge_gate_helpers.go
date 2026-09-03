package menu

import (
	"bytes"
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

// substituteGateTokens replaces the literal tokens "{KEY}", "{PRESSES}", and
// "{TIMES}" in prompt with keyDisplay, the decimal presses count, and the
// correct pluralized noun ("time" for 1 press, "times" for other counts),
// respectively. This allows custom gate art (and the built-in fallback) to
// render the configured challenge key and required press count without
// drifting from config. Any "##" countdown field is left untouched.
//
// Substitution is done line by line so a line whose width changed can be
// re-padded against a trailing box border (see realignBorder), keeping framed
// art square no matter how wide the configured key and press count render.
func substituteGateTokens(prompt []byte, keyDisplay string, presses int) []byte {
	timesWord := "times"
	if presses == 1 {
		timesWord = "time"
	}
	lines := bytes.SplitAfter(prompt, []byte("\n"))
	for i, line := range lines {
		out := bytes.ReplaceAll(line, []byte("{KEY}"), []byte(keyDisplay))
		out = bytes.ReplaceAll(out, []byte("{PRESSES}"), []byte(strconv.Itoa(presses)))
		out = bytes.ReplaceAll(out, []byte("{TIMES}"), []byte(timesWord))
		if len(out) != len(line) {
			out = realignBorder(out, len(line)-len(out))
		}
		lines[i] = out
	}
	return bytes.Join(lines, nil)
}

// realignBorder restores a substituted line to its original width by adding
// (delta > 0) or absorbing (delta < 0) spaces in the gap in front of the
// line's trailing border decoration, so art like
//
//	| Press {KEY} {PRESSES} {TIMES} if you're not a bot. |
//
// keeps its right-hand border in the same column after the tokens shrink. The
// trailing run is only treated as a border when it is made purely of frame
// characters (no letters or digits) and a space separates it from the text, so
// a plain sentence line — the built-in fallback prompt, for instance — is
// returned untouched rather than gaining a gap mid-sentence. Color codes are
// not text: escape sequences trailing the border (a closing reset, say) or
// colouring it are skipped when looking for the border.
func realignBorder(line []byte, delta int) []byte {
	body := line
	for len(body) > 0 && (body[len(body)-1] == '\n' || body[len(body)-1] == '\r') {
		body = body[:len(body)-1]
	}
	tail := trimGateTail(body) // trailing escape sequences and spaces, kept as-is
	suffix := line[len(body)-len(tail):]
	body = body[:len(body)-len(tail)]

	border := len(body)
	for border > 0 && !isGateSpace(body[border-1]) {
		border--
	}
	if border == len(body) || border == 0 || !isGateBorder(body[border:]) {
		return line // no trailing border to align against
	}
	gap := border
	for gap > 0 && isGateSpace(body[gap-1]) {
		gap--
	}

	pad := delta
	if pad < 0 {
		if avail := border - gap; -pad > avail {
			pad = -avail // line outgrew its gap; close it up as far as it goes
		}
	}

	out := make([]byte, 0, len(line)+pad)
	out = append(out, body[:border]...)
	if pad > 0 {
		out = append(out, bytes.Repeat([]byte{' '}, pad)...)
	} else {
		out = out[:len(out)+pad]
	}
	out = append(out, body[border:]...)
	out = append(out, suffix...)
	return out
}

func isGateSpace(b byte) bool { return b == ' ' || b == '\t' }

// trimGateTail returns the run of escape sequences and spaces at the end of
// body — everything past the last visible character — so border detection can
// look at the border itself rather than at a trailing reset code.
func trimGateTail(body []byte) []byte {
	end := len(body)
	for end > 0 {
		if isGateSpace(body[end-1]) {
			end--
			continue
		}
		esc := bytes.LastIndexByte(body[:end], 0x1B)
		if esc < 0 || esc+escapeSeqLen(body[esc:end]) != end {
			break
		}
		end = esc
	}
	return body[end:]
}

// isGateBorder reports whether run is a frame decoration rather than words:
// no ASCII letters or digits, and no sentence punctuation, which keeps text
// ending in "." or "!" from being mistaken for a border. Escape sequences
// within the run (a color set on the border) are ignored.
func isGateBorder(run []byte) bool {
	visible := 0
	for i := 0; i < len(run); i++ {
		b := run[i]
		if b == 0x1B {
			i += escapeSeqLen(run[i:]) - 1 // -1: the loop's i++ passes the last byte
			continue
		}
		switch {
		case b >= 'a' && b <= 'z', b >= 'A' && b <= 'Z', b >= '0' && b <= '9':
			return false
		case b == '.' || b == ',' || b == '!' || b == '?' || b == ';' || b == '\'' || b == '"':
			return false
		}
		visible++
	}
	return visible > 0
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
