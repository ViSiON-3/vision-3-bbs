package configeditor

import "testing"

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
