package menu

import "strings"

// breakOversizedLines splits any line wider than width into chunks that fit.
//
// wrapAnsiString breaks on spaces, so a single token with no break opportunity
// — a long URL, a path, a run of dashes — is emitted unchanged and overflows.
// Left alone the client terminal wraps it at its own margin, which is the very
// mid-token splitting the wrapping exists to prevent, and which lands at the
// terminal's width rather than ours (re-introducing the trailing-column
// auto-wrap this code otherwise avoids).
//
// An over-long token has to break somewhere; doing it here makes the break
// deterministic and keeps every emitted line inside the budget.
//
// ANSI escape sequences are copied through without counting toward the width
// and are never split across a chunk boundary.
func breakOversizedLines(lines []string, width int) []string {
	if width <= 0 {
		return lines
	}

	out := make([]string, 0, len(lines))
	for _, line := range lines {
		if visibleWidth(line) <= width {
			out = append(out, line)
			continue
		}
		out = append(out, hardBreak(line, width)...)
	}
	return out
}

// hardBreak cuts s into chunks of at most width visible columns.
func hardBreak(s string, width int) []string {
	var (
		chunks  []string
		b       strings.Builder
		visible int
	)
	runes := []rune(s)

	for i := 0; i < len(runes); {
		if n := ansiEscapeLen(runes[i:]); n > 0 {
			// Zero-width: emit with the current chunk, do not count or split.
			b.WriteString(string(runes[i : i+n]))
			i += n
			continue
		}
		if visible == width {
			chunks = append(chunks, b.String())
			b.Reset()
			visible = 0
		}
		b.WriteRune(runes[i])
		visible++
		i++
	}
	if b.Len() > 0 {
		chunks = append(chunks, b.String())
	}
	return chunks
}

// ansiEscapeLen returns the rune length of the CSI escape sequence at the start
// of r, or 0 if r does not begin with one. Handles ESC [ params... final-byte,
// which is the form ReplacePipeCodes emits.
func ansiEscapeLen(r []rune) int {
	if len(r) < 2 || r[0] != 0x1b || r[1] != '[' {
		return 0
	}
	for i := 2; i < len(r); i++ {
		c := r[i]
		if (c >= '0' && c <= '9') || c == ';' || c == '?' {
			continue
		}
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') {
			return i + 1 // include the final byte
		}
		return 0 // malformed; treat as literal text
	}
	return 0 // unterminated
}

// visibleWidth is the on-screen column count of s, ignoring ANSI escapes.
func visibleWidth(s string) int {
	runes := []rune(s)
	n := 0
	for i := 0; i < len(runes); {
		if l := ansiEscapeLen(runes[i:]); l > 0 {
			i += l
			continue
		}
		n++
		i++
	}
	return n
}
