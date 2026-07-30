package menu

import (
	"strings"
	"testing"
)

// buildAutoWidths feeds ansi.ApplyWidthConstraintAligned, which pads and
// truncates by visible runes. A multi-byte subject, from/to, or Z
// (conference/area) value must produce a width matching its rune count, not
// its byte length, or the caller over-pads the column.
func TestBuildAutoWidthsMeasuresRunesNotBytes(t *testing.T) {
	// 5 CJK runes: 15 bytes in UTF-8.
	subj := strings.Repeat("日", 5)
	subs := map[byte]string{
		'S': subj,
		'Z': subj,
	}

	widths := buildAutoWidths(subs, 42, 80)

	if got, want := widths['S'], 5; got != want {
		t.Errorf("widths['S'] = %d, want %d (rune count of %q)", got, want, subj)
	}
	if got, want := widths['Z'], 5; got != want {
		t.Errorf("widths['Z'] = %d, want %d (rune count of %q)", got, want, subj)
	}
	// X = Z + " " + "[42/42]"
	wantX := 5 + 1 + len("[42/42]")
	if got := widths['X']; got != wantX {
		t.Errorf("widths['X'] = %d, want %d", got, wantX)
	}
}
