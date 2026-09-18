package menu

import (
	"strings"
	"testing"
)

// Art of this era is written for an 80-column screen and leans on the
// terminal's auto-wrap to begin each new row; twelve of the art messages in
// the fsxNet Ads + ANSI Art echo turn it on explicitly with ESC[?7h, and none
// turn it off. Clipping instead crushed everything past the margin onto the
// last column, which is what compressed the block-letter logos.
func TestANSIRendererAutoWraps(t *testing.T) {
	const w = 10

	t.Run("overflow continues on the next row", func(t *testing.T) {
		lines := RenderANSIArtToLines(strings.Repeat("a", w)+"bc", w, 5)
		if len(lines) < 2 {
			t.Fatalf("expected the overflow to wrap, got %d row(s): %q", len(lines), lines)
		}
		if got := plainRow(lines, 0); got != strings.Repeat("a", w) {
			t.Errorf("row 0 = %q, want %d a's", got, w)
		}
		if got := plainRow(lines, 1); got != "bc" {
			t.Errorf("row 1 = %q, want \"bc\"", got)
		}
	})

	// The wrap is deferred: filling the last column arms it, and only the next
	// printable character starts a new row. Wrapping eagerly would put a blank
	// row after every full-width line of art.
	t.Run("a full row followed by a newline does not gain a blank row", func(t *testing.T) {
		lines := RenderANSIArtToLines(strings.Repeat("a", w)+"\n"+strings.Repeat("b", w), w, 5)
		if len(lines) != 2 {
			t.Fatalf("expected exactly 2 rows, got %d: %q", len(lines), lines)
		}
		if plainRow(lines, 0) != strings.Repeat("a", w) || plainRow(lines, 1) != strings.Repeat("b", w) {
			t.Errorf("rows = %q", lines)
		}
	})

	t.Run("cursor movement cancels a pending wrap", func(t *testing.T) {
		// Fill the row, step back, then write: this must land on row 0, not row 1.
		lines := RenderANSIArtToLines(strings.Repeat("a", w)+"\x1b[1DZ", w, 5)
		if len(lines) != 1 {
			t.Fatalf("expected 1 row, got %d: %q", len(lines), lines)
		}
		// ESC[1D steps back to the penultimate column, so Z lands there and
		// the last character written stays put.
		if got, want := plainRow(lines, 0), strings.Repeat("a", w-2)+"Za"; got != want {
			t.Errorf("row = %q, want %q", got, want)
		}
	})

	t.Run("a carriage return cancels a pending wrap", func(t *testing.T) {
		lines := RenderANSIArtToLines(strings.Repeat("a", w)+"\rZ", w, 5)
		if len(lines) != 1 {
			t.Fatalf("expected 1 row, got %d: %q", len(lines), lines)
		}
		if got := plainRow(lines, 0); !strings.HasPrefix(got, "Z") {
			t.Errorf("row = %q, want Z written at column 0 of row 0", got)
		}
	})
}

// ESC[?7l turns wrapping off, and then a full row stays on its row with
// further writes overwriting the last column, as a terminal does.
func TestANSIRendererHonoursWrapMode(t *testing.T) {
	const w = 10

	off := RenderANSIArtToLines("\x1b[?7l"+strings.Repeat("a", w)+"bc", w, 5)
	if len(off) != 1 {
		t.Fatalf("wrapping was disabled but output took %d rows: %q", len(off), off)
	}
	if got := plainRow(off, 0); !strings.HasSuffix(got, "c") || len(got) != w {
		t.Errorf("row = %q, want %d columns ending in the last character written", got, w)
	}

	on := RenderANSIArtToLines("\x1b[?7l\x1b[?7h"+strings.Repeat("a", w)+"bc", w, 5)
	if len(on) < 2 {
		t.Fatalf("wrapping was re-enabled but output took %d row(s): %q", len(on), on)
	}
}
