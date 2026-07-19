package configeditor

import (
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
)

func TestRasterizeArt_Dimensions(t *testing.T) {
	grid := rasterizeArt([]byte("hello"))
	if len(grid) != artHeight {
		t.Fatalf("rows = %d, want %d", len(grid), artHeight)
	}
	for i, row := range grid {
		if len(row) != artWidth {
			t.Fatalf("row %d width = %d, want %d", i, len(row), artWidth)
		}
	}
}

func TestRasterizeArt_PlainText(t *testing.T) {
	grid := rasterizeArt([]byte("AB"))
	if grid[0][0].ch != 'A' || grid[0][1].ch != 'B' {
		t.Fatalf("got %q%q, want AB", grid[0][0].ch, grid[0][1].ch)
	}
	if grid[0][2].ch != ' ' {
		t.Fatalf("cell[0][2] = %q, want space", grid[0][2].ch)
	}
}

func TestRasterizeArt_SGRColors(t *testing.T) {
	// ESC[1;33;44m X -> ANSI SGR fg 33 (yellow) and bg 44 (blue).
	// DOS/ANSI.SYS terminals remap the ANSI 0-7 color index to the CGA
	// palette order by swapping bit0 and bit2 (red<->blue channels), which
	// is what the fg/bg fields here store, matching dosColors in colors.go
	// (index 1 = Blue, index 4 = Red, index 6 = Brown, index 14 = Yellow).
	// ANSI yellow (3) -> CGA brown (6); bold sets the bright bit (+8) = 14.
	// ANSI blue (4) -> CGA blue (1).
	grid := rasterizeArt([]byte("\x1b[1;33;44mX"))
	c := grid[0][0]
	if c.ch != 'X' {
		t.Fatalf("ch = %q, want X", c.ch)
	}
	if c.fg != 14 { // ANSI yellow(3)->CGA brown(6), bold(+8) = 14 (Yellow)
		t.Fatalf("fg = %d, want 14", c.fg)
	}
	if c.bg != 1 { // ANSI blue(4)->CGA blue(1)
		t.Fatalf("bg = %d, want 1", c.bg)
	}
}

func TestRasterizeArt_Wrap(t *testing.T) {
	// 81 'X' chars: 80 fill row 0, the 81st wraps to row 1 col 0.
	data := make([]byte, 81)
	for i := range data {
		data[i] = 'X'
	}
	grid := rasterizeArt(data)
	if grid[0][79].ch != 'X' {
		t.Fatalf("row0 col79 = %q, want X", grid[0][79].ch)
	}
	if grid[1][0].ch != 'X' {
		t.Fatalf("row1 col0 = %q, want X (wrap)", grid[1][0].ch)
	}
}

func TestRasterizeArt_CursorPosition(t *testing.T) {
	// ESC[3;5H places cursor at row 3 col 5 (1-based) -> grid[2][4].
	grid := rasterizeArt([]byte("\x1b[3;5HZ"))
	if grid[2][4].ch != 'Z' {
		t.Fatalf("grid[2][4] = %q, want Z", grid[2][4].ch)
	}
}

func TestLoadBackdrop_CentersArt(t *testing.T) {
	b := loadBackdrop(100, 30)
	if !b.art {
		t.Fatal("expected art mode from embedded asset")
	}
	if b.width != 100 || b.height != 30 {
		t.Fatalf("size = %dx%d, want 100x30", b.width, b.height)
	}
	if len(b.cells) != 30 || len(b.cells[0]) != 100 {
		t.Fatalf("grid = %dx%d, want 100x30", len(b.cells), len(b.cells[0]))
	}
	// Art (80x25) centered in 100x30: startCol=10, startRow=2.
	// Margin cells are black spaces.
	if c := b.cells[0][0]; c.ch != ' ' || c.bg != 0 {
		t.Fatalf("top-left margin = %+v, want black space", c)
	}
}

func TestLoadBackdrop_ExactSize(t *testing.T) {
	b := loadBackdrop(80, 25)
	if len(b.cells) != 25 || len(b.cells[0]) != 80 {
		t.Fatalf("grid = %dx%d, want 80x25", len(b.cells), len(b.cells[0]))
	}
}

func TestBackdrop_SegmentVisibleWidth(t *testing.T) {
	b := loadBackdrop(80, 25)
	seg := b.segment(5, 0, 10)
	if got := ansiVisibleLen(seg); got != 10 {
		t.Fatalf("segment visible width = %d, want 10", got)
	}
}

func TestBackdrop_LineVisibleWidth(t *testing.T) {
	b := loadBackdrop(90, 25)
	if got := ansiVisibleLen(b.line(3)); got != 90 {
		t.Fatalf("line visible width = %d, want 90", got)
	}
}

func TestBackdrop_FallbackUsesShade(t *testing.T) {
	b := &backdrop{width: 80, height: 25, art: false}
	seg := b.segment(0, 0, 4)
	if !strings.Contains(seg, "░") {
		t.Fatalf("fallback segment should contain ░, got %q", seg)
	}
}

func TestBackdrop_SegmentOutOfBounds(t *testing.T) {
	b := loadBackdrop(80, 25)
	// row beyond height must not panic and must pad to requested width.
	seg := b.segment(999, 0, 5)
	if got := ansiVisibleLen(seg); got != 5 {
		t.Fatalf("oob segment width = %d, want 5", got)
	}
}

// ansiVisibleLen counts runes outside ANSI SGR escape sequences.
func ansiVisibleLen(s string) int {
	return ansi.VisibleLength(s)
}

func TestBackdrop_SegmentRowOOBPadsBlack(t *testing.T) {
	b := loadBackdrop(80, 25)
	seg := b.segment(999, 0, 6)
	if strings.Contains(seg, "░") {
		t.Fatalf("row-OOB in art mode should pad black, not ░: %q", seg)
	}
	if got := ansi.VisibleLength(seg); got != 6 {
		t.Fatalf("row-OOB segment width = %d, want 6", got)
	}
}

func TestBackdrop_SegmentColOOBPadsWidth(t *testing.T) {
	b := loadBackdrop(80, 25)
	seg := b.segment(5, 78, 6) // starts at col 78, runs 6 cols → 4 beyond width 80
	if got := ansi.VisibleLength(seg); got != 6 {
		t.Fatalf("col-OOB segment width = %d, want 6", got)
	}
}

func TestLoadBackdrop_OddMargin(t *testing.T) {
	b := loadBackdrop(81, 25) // (81-80)/2 = 0 → art starts at col 0
	// Row 0 col 0 should be an art cell region (not guaranteed non-space),
	// and the extra column of slack is on the right (col 80) as a black margin.
	if len(b.cells[0]) != 81 {
		t.Fatalf("width = %d, want 81", len(b.cells[0]))
	}
	if c := b.cells[0][80]; c.bg != 0 {
		t.Fatalf("right slack col 80 bg = %d, want 0 (black)", c.bg)
	}
}
