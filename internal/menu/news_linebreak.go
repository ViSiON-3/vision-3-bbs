package menu

import (
	"strings"
	"unicode/utf8"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/mattn/go-runewidth"
)

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
//
// Width is measured with visibleColumns, the same measure wrapAnsiString used
// to produce these lines. Counting runes here instead let a line of wide
// characters read as narrower than it renders, so an unbreakable token that
// really did overflow was judged to fit and passed straight through.
func breakOversizedLines(lines []string, width int, mode ansi.OutputMode) []string {
	if width <= 0 {
		return lines
	}

	out := make([]string, 0, len(lines))
	for _, line := range lines {
		if visibleColumns(line, mode) <= width {
			out = append(out, line)
			continue
		}
		out = append(out, hardBreak(line, width, mode)...)
	}
	return out
}

// hardBreak cuts s into chunks of at most width visible columns.
//
// It walks bytes rather than runes. Decoding to []rune first turned every byte
// of a CP437 line into U+FFFD, and writing those back out replaced the art
// with replacement characters; the bytes are now copied through exactly as
// they arrived.
func hardBreak(s string, width int, mode ansi.OutputMode) []string {
	var (
		chunks  []string
		b       strings.Builder
		visible int
	)
	asUTF8 := utf8.ValidString(s) // with escapes in, as columnWidth expects

	for i := 0; i < len(s); {
		if n := escapeLen(s, i); n > 0 {
			// Zero-width: emit with the current chunk, do not count or split.
			b.WriteString(s[i : i+n])
			i += n
			continue
		}

		n, w := 1, 1
		if asUTF8 {
			r, size := utf8.DecodeRuneInString(s[i:])
			n = size
			if mode != ansi.OutputModeCP437 {
				w = runewidth.RuneWidth(r)
			}
		}
		// A double-width glyph cannot straddle the margin, so break before it
		// rather than letting the chunk run a column over.
		if visible > 0 && visible+w > width {
			chunks = append(chunks, b.String())
			b.Reset()
			visible = 0
		}
		b.WriteString(s[i : i+n])
		visible += w
		i += n
	}
	if b.Len() > 0 {
		chunks = append(chunks, b.String())
	}
	return chunks
}
