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
		{
			// Terminals clamp a CUP past the bottom row rather than scrolling
			// to it, so parking the cursor low must not report a tall image.
			name:     "cursor positioning alone does not claim a row",
			data:     []byte("hi\x1b[25;1H"),
			width:    80,
			wantRows: 1,
			wantCols: 2,
		},
		{
			name:     "cursor positioning claims a row once something prints there",
			data:     []byte("hi\x1b[25;1Hx"),
			width:    80,
			wantRows: 25,
			wantCols: 1,
		},
		{
			name:     "line feeds past the bottom do claim a row, since they scroll",
			data:     []byte("hi\n\n\n"),
			width:    80,
			wantRows: 4,
			wantCols: 2,
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

	// Art that merely parks the cursor at the bottom of a 25-row layout is
	// clamped by the terminal, not scrolled, and must not warn.
	if ArtOverflowsHeight([]byte("art\x1b[25;1H"), 80, 24) {
		t.Error("cursor positioning alone should not report an overflow")
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

func TestHardWrap(t *testing.T) {
	x80 := strings.Repeat("x", 80)
	tests := []struct {
		name      string
		in        string
		utf8Spans bool
		want      string
	}{
		{
			name: "wrap-only art gets an explicit break",
			in:   x80 + x80,
			want: x80 + "\r\n" + x80,
		},
		{
			name: "full row followed by CRLF is left alone",
			in:   x80 + "\r\n" + "y",
			want: x80 + "\r\n" + "y",
		},
		{
			name: "SGR does not cancel a pending wrap",
			in:   x80 + "\x1b[31my",
			want: x80 + "\x1b[31m\r\ny",
		},
		{
			name: "cursor positioning cancels a pending wrap",
			in:   x80 + "\x1b[5;1Hy",
			want: x80 + "\x1b[5;1Hy",
		},
		{
			name: "cursor forward is clamped at the right margin",
			in:   "abc\x1b[100Cz",
			want: "abc\x1b[76Cz",
		},
		{
			name: "cursor forward at the margin is dropped",
			in:   strings.Repeat("x", 79) + "\x1b[5Cz" + "w",
			want: strings.Repeat("x", 79) + "z\r\nw",
		},
		{
			name:      "UTF-8 spans are measured in runes",
			in:        strings.Repeat("─", 80) + "x",
			utf8Spans: true,
			want:      strings.Repeat("─", 80) + "\r\nx",
		},
		{
			name: "CP437 bytes that happen to form valid UTF-8 are one cell each",
			in:   strings.Repeat("\xdc\xb3", 40) + "x",
			want: strings.Repeat("\xdc\xb3", 40) + "\r\nx",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := string(HardWrap([]byte(tt.in), 80, tt.utf8Spans))
			if got != tt.want {
				t.Errorf("HardWrap = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFitArtToWidth_LeavesStandardWidthAlone(t *testing.T) {
	in := []byte(strings.Repeat("x", 160))
	for _, w := range []int{0, 40, 80} {
		if got := FitArtToWidth(in, w, false); !bytes.Equal(got, in) {
			t.Errorf("termWidth %d: art was modified", w)
		}
	}
	if got := FitArtToWidth(in, 132, false); bytes.Equal(got, in) {
		t.Error("termWidth 132: wrap-only art was not broken into rows")
	}
}

// renderCells draws ANSI art onto an unbounded grid width columns wide, the
// way a terminal with deferred autowrap would, and returns the character in
// each cell. It understands just the sequences shipped art uses.
func renderCells(data []byte, width int) map[[2]int]byte {
	cells := make(map[[2]int]byte)
	x, y, sx, sy := 1, 1, 1, 1
	pending, sp := false, false // xterm saves the pending wrap with the cursor
	clamp := func() {
		x = min(max(x, 1), width)
		y = max(y, 1)
	}
	for i := 0; i < len(data); i++ {
		b := data[i]
		switch {
		case b == 0x1b && i+1 < len(data) && data[i+1] == '[':
			j := i + 2
			for j < len(data) && data[j] >= 0x20 && data[j] <= 0x3f {
				j++
			}
			if j >= len(data) {
				return cells
			}
			var ps []int
			for _, f := range strings.Split(string(data[i+2:j]), ";") {
				n := 0
				for _, c := range f {
					if c >= '0' && c <= '9' {
						n = n*10 + int(c-'0')
					}
				}
				ps = append(ps, n)
			}
			p := func(k int) int {
				if k < len(ps) && ps[k] > 0 {
					return ps[k]
				}
				return 1
			}
			switch data[j] {
			case 'H', 'f':
				y, x = p(0), p(1)
			case 'A':
				y -= p(0)
			case 'B':
				y += p(0)
			case 'C':
				x += p(0)
			case 'D':
				x -= p(0)
			case 'E':
				y, x = y+p(0), 1
			case 'F':
				y, x = y-p(0), 1
			case 'G', '`':
				x = p(0)
			case 'J':
				if len(ps) > 0 && ps[0] == 2 {
					clear(cells)
				}
			}
			switch data[j] {
			case 'm', 'J', 'K':
			case 's':
				sx, sy, sp = x, y, pending
			case 'u':
				x, y, pending = sx, sy, sp
			default:
				pending = false
			}
			clamp()
			i = j
		case b == 0x1b && i+1 < len(data):
			i++
		case b == '\r':
			x, pending = 1, false
		case b == '\n':
			x, y, pending = 1, y+1, false
		case b == '\b':
			x, pending = x-1, false
			clamp()
		case b == '\t':
			x, pending = ((x-1)/8+1)*8+1, false
			clamp()
		case b < 0x20:
		default:
			if pending {
				x, y, pending = 1, y+1, false
			}
			cells[[2]int{y, x}] = b
			if x == width {
				pending = true
			} else {
				x++
			}
		}
	}
	return cells
}

// TestHardWrapShippedArt checks that every shipped ANSI file, passed through
// HardWrap, draws exactly the same cells on a 132-column terminal as the
// original does on an 80-column one.
func TestHardWrapShippedArt(t *testing.T) {
	root := filepath.Join("..", "..", "menus", "v3")
	if _, err := os.Stat(root); err != nil {
		t.Skipf("menu set not available: %v", err)
	}
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.EqualFold(filepath.Ext(path), ".ans") {
			return err
		}
		data, err := GetAnsiFileContent(path)
		if err != nil {
			return err
		}
		want := renderCells(data, 80)
		got := renderCells(HardWrap(data, 80, false), 132)
		if len(got) != len(want) {
			t.Errorf("%s: %d cells drawn at 132 columns, want %d", path, len(got), len(want))
			return nil
		}
		for pos, c := range want {
			if got[pos] != c {
				t.Errorf("%s: row %d col %d = %q at 132 columns, want %q", path, pos[0], pos[1], got[pos], c)
				return nil
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
