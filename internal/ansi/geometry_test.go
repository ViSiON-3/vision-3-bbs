package ansi

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestArtGeometry(t *testing.T) {
	tests := []struct {
		name     string
		data     []byte
		width    int
		wantRows int
		wantCols int
	}{
		{
			name:     "wrap-only art stops short of the last column",
			data:     bytes.Repeat([]byte("x"), 80*23+79),
			width:    80,
			wantRows: 24,
			wantCols: 79,
		},
		{
			name:     "wrap-only art filling the last column wraps a row further",
			data:     bytes.Repeat([]byte("x"), 80*24),
			width:    80,
			wantRows: 25,
			wantCols: 80,
		},
		{
			name:     "CRLF art counts its lines",
			data:     []byte("one\r\ntwo\r\nthree"),
			width:    80,
			wantRows: 3,
			wantCols: 5,
		},
		{
			name:     "trailing newline on the last line advances a row",
			data:     []byte("one\r\ntwo\r\n"),
			width:    80,
			wantRows: 3,
			wantCols: 3,
		},
		{
			name:     "cursor positioning is honoured",
			data:     []byte("\x1b[2J\x1b[H\x1b[20;5Hhi"),
			width:    80,
			wantRows: 20,
			wantCols: 6,
		},
		{
			name:     "relative cursor movement is honoured",
			data:     []byte("a\x1b[3B\x1b[10Cb"),
			width:    80,
			wantRows: 4,
			wantCols: 12,
		},
		{
			name:     "SAUCE past the EOF marker is ignored",
			data:     append([]byte("hi\x1a"), bytes.Repeat([]byte("S"), 128)...),
			width:    80,
			wantRows: 1,
			wantCols: 2,
		},
		{
			name:     "SGR sequences do not advance the cursor",
			data:     []byte("\x1b[1;37;40mab\x1b[0m"),
			width:    80,
			wantRows: 1,
			wantCols: 2,
		},
		{
			name:     "cursor never moves above the top row",
			data:     []byte("\x1b[10Ax"),
			width:    80,
			wantRows: 1,
			wantCols: 1,
		},
		{
			name:     "horizontal tab advances to the next tab stop",
			data:     []byte("a\tb"), // a at col 1, tab to col 9, b at col 9
			width:    80,
			wantRows: 1,
			wantCols: 9,
		},
		{
			name:     "tabs push the row over the margin like printable cells do",
			data:     bytes.Repeat([]byte("\t"), 10), // 10 tab stops = col 81 > 80
			width:    16,
			wantRows: 1,
			wantCols: 0, // nothing printable was written
		},
		{
			name:     "tabs stop at the tab stop they land on",
			data:     append(bytes.Repeat([]byte("\t"), 9), 'x'), // stops at 9,17..73
			width:    80,
			wantRows: 1,
			wantCols: 73,
		},
		{
			name:     "a tab past the margin clamps, and the cell it fills still wraps",
			data:     append(bytes.Repeat([]byte("\t"), 10), 'x'), // 10th stop is 81, clamped to 80
			width:    80,
			wantRows: 2,
			wantCols: 80,
		},
		{
			name:     "cursor forward is clamped at the right margin",
			data:     []byte("\x1b[200C\x1b[1Dx"), // clamped to 80, back one, so no wrap
			width:    80,
			wantRows: 1,
			wantCols: 79,
		},
		{
			name:     "absolute positioning past the margin is clamped",
			data:     []byte("\x1b[1;500H\x1b[1Dx"),
			width:    80,
			wantRows: 1,
			wantCols: 79,
		},
		{
			name:     "a character in the last column wraps even when reached by positioning",
			data:     []byte("\x1b[1;500Hx"),
			width:    80,
			wantRows: 2,
			wantCols: 80,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rows, cols := ArtGeometry(tc.data, tc.width)
			if rows != tc.wantRows || cols != tc.wantCols {
				t.Errorf("ArtGeometry() = (%d, %d), want (%d, %d)", rows, cols, tc.wantRows, tc.wantCols)
			}
		})
	}
}

func TestArtOverflowsHeight(t *testing.T) {
	full24 := bytes.Repeat([]byte("x"), 80*24) // fills row 24 col 80, so it wraps to row 25

	if !ArtOverflowsHeight(full24, 80, 24) {
		t.Error("80x24 full-bleed art should overflow a 24-row terminal")
	}
	if ArtOverflowsHeight(full24, 80, 25) {
		t.Error("80x24 full-bleed art should fit a 25-row terminal")
	}
	if ArtOverflowsHeight(full24, 80, 0) {
		t.Error("unknown height should never report an overflow")
	}
}

// TestMenuArtFitsStandardHeights guards the shipped menu set against the class of
// bug that made FASTLOGN.ANS scroll on 24-row terminals: art that fills its last
// row all the way to column 80 wraps one row further and shifts the whole image
// up, desynchronising it from absolutely-positioned BAR lightbar overlays.
func TestMenuArtFitsStandardHeights(t *testing.T) {
	ansiDir := filepath.Join("..", "..", "menus", "v3", "ansi")
	entries, err := os.ReadDir(ansiDir)
	if err != nil {
		t.Skipf("menu set not available: %v", err)
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".ans") {
			continue
		}
		name := entry.Name()

		// Only art with a matching BAR file positions overlays by absolute
		// coordinates, so only that art has to fit without scrolling.
		barPath := filepath.Join("..", "..", "menus", "v3", "bar", strings.TrimSuffix(name, filepath.Ext(name))+".BAR")
		if _, err := os.Stat(barPath); err != nil {
			continue
		}

		data, err := GetAnsiFileContent(filepath.Join(ansiDir, name))
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if rows, cols := ArtGeometry(data, 80); rows > 24 {
			t.Errorf("%s renders %d rows (last row occupies %d columns) and will scroll a 24-row terminal; "+
				"trim the last row so it ends at column 79 or shorter", name, rows, cols)
		}
	}
}
