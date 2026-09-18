package menu

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/mattn/go-runewidth"
)

// The news body is wrapped by wrapAnsiString and then re-checked against the
// same budget by breakOversizedLines. When the second pass counted runes, a
// line of wide characters read as narrower than it renders, so an unbreakable
// token that really did overflow was judged to fit and passed straight through
// to the terminal.
func TestNewsHardBreakMeasuresWideRunes(t *testing.T) {
	const width = 20

	// 15 CJK runes: 15 by rune count, so the old measure saw a line that fit
	// comfortably; 30 display columns, so it overflowed by half again.
	token := strings.Repeat("\u4f60", 15)
	if n := utf8.RuneCountInString(token); n > width {
		t.Fatalf("fixture is %d runes; it must look like it fits to a rune count", n)
	}
	if w := runewidth.StringWidth(token); w <= width {
		t.Fatalf("fixture is %d display columns; it must actually overflow %d", w, width)
	}

	got := breakOversizedLines([]string{token}, width, ansi.OutputModeUTF8)
	if len(got) < 2 {
		t.Fatalf("overflowing token was judged to fit and passed through: %d chunk(s)", len(got))
	}
	for i, chunk := range got {
		if w := runewidth.StringWidth(chunk); w > width {
			t.Errorf("chunk %d is %d display columns, over the %d budget: %q", i, w, width, chunk)
		}
	}
	if strings.Join(got, "") != token {
		t.Errorf("breaking altered the text:\n got  %q\n want %q", strings.Join(got, ""), token)
	}
}

// A double-width glyph cannot straddle the margin, so a chunk must break
// before it rather than running a column over.
func TestNewsHardBreakDoesNotStraddleTheMargin(t *testing.T) {
	const width = 5 // odd, so pairs of 2-column glyphs cannot fill it exactly
	for _, chunk := range breakOversizedLines([]string{strings.Repeat("你", 8)}, width, ansi.OutputModeUTF8) {
		if w := runewidth.StringWidth(chunk); w > width {
			t.Errorf("chunk is %d columns, over %d: %q", w, width, chunk)
		}
	}
}

// hardBreak used to decode to []rune first, which turned every byte of a CP437
// line into U+FFFD and wrote replacement characters back out in place of the
// art. The bytes must survive exactly.
func TestNewsHardBreakPreservesCP437Bytes(t *testing.T) {
	const width = 10
	line := strings.Repeat("\xc4\xdc", 12) // 24 columns of CP437 art, unbreakable

	if utf8.ValidString(line) {
		t.Fatalf("fixture must be invalid UTF-8 to exercise the CP437 path")
	}

	got := breakOversizedLines([]string{line}, width, ansi.OutputModeCP437)
	if len(got) < 2 {
		t.Fatalf("oversized CP437 line was not broken: %d chunk(s)", len(got))
	}
	if joined := strings.Join(got, ""); joined != line {
		t.Errorf("CP437 bytes were corrupted:\n got  %q\n want %q", joined, line)
	}
	if strings.Contains(strings.Join(got, ""), "�") {
		t.Error("replacement characters written in place of CP437 bytes")
	}
	for i, chunk := range got {
		if len(chunk) > width {
			t.Errorf("chunk %d is %d columns, over %d", i, len(chunk), width)
		}
	}
}

// Escapes still ride along without counting toward the width.
func TestNewsHardBreakKeepsEscapesOutOfTheCount(t *testing.T) {
	const width = 10
	line := "\x1b[31m" + strings.Repeat("a", 25) + "\x1b[0m"

	for i, chunk := range breakOversizedLines([]string{line}, width, ansi.OutputModeUTF8) {
		if w := visibleColumns(chunk, ansi.OutputModeUTF8); w > width {
			t.Errorf("chunk %d is %d visible columns, over %d: %q", i, w, width, chunk)
		}
	}
}
