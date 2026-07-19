package configeditor

import (
	_ "embed"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
)

//go:embed assets/TUICONFIG.ANS
var tuiConfigANS []byte

const (
	artWidth  = 80
	artHeight = 25
)

// artCell is one rasterized character with its DOS palette colors.
// fg is 0-15 (bright included), bg is 0-7 (blink bit dropped).
type artCell struct {
	ch rune
	fg int
	bg int
}

// rasterizeArt converts CP437 ANSI art into a fixed artHeight×artWidth grid,
// interpreting SGR color/attribute codes, CR/LF, 80-column wrap, and the CSI
// cursor-position (H/f) and erase (J/K) operations. Input bytes are converted
// CP437→UTF-8 first (escape sequences are preserved by that conversion).
func rasterizeArt(data []byte) [][]artCell {
	grid := make([][]artCell, artHeight)
	for y := range grid {
		grid[y] = make([]artCell, artWidth)
		for x := range grid[y] {
			grid[y][x] = artCell{ch: ' ', fg: 7, bg: 0}
		}
	}

	s := string(ansi.CP437BytesToUTF8(data))
	baseFg, bg, bold := 7, 0, false
	fg := func() int {
		if bold {
			return baseFg + 8
		}
		return baseFg
	}
	x, y := 0, 0

	put := func(r rune) {
		if x >= artWidth {
			x = 0
			y++
		}
		if y < 0 || y >= artHeight {
			return
		}
		grid[y][x] = artCell{ch: r, fg: fg(), bg: bg}
		x++
	}

	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == '\x1b' && i+1 < len(s) && s[i+1] == '[' {
			// Parse CSI: ESC [ params final
			j := i + 2
			for j < len(s) && (s[j] < '@' || s[j] > '~') {
				j++
			}
			if j >= len(s) {
				break
			}
			final := s[j]
			params := parseCSIParams(s[i+2 : j])
			switch final {
			case 'm':
				baseFg, bg, bold = applySGR(params, baseFg, bg, bold)
			case 'H', 'f':
				row, col := 1, 1
				if len(params) >= 1 && params[0] > 0 {
					row = params[0]
				}
				if len(params) >= 2 && params[1] > 0 {
					col = params[1]
				}
				y, x = row-1, col-1
			case 'J':
				if len(params) >= 1 && params[0] == 2 {
					for yy := range grid {
						for xx := range grid[yy] {
							grid[yy][xx] = artCell{ch: ' ', fg: fg(), bg: bg}
						}
					}
					x, y = 0, 0
				}
			case 'K':
				if y >= 0 && y < artHeight {
					for xx := x; xx < artWidth; xx++ {
						grid[y][xx] = artCell{ch: ' ', fg: fg(), bg: bg}
					}
				}
			}
			i = j + 1
			continue
		}
		switch r {
		case '\n':
			y++
			x = 0
		case '\r':
			x = 0
		default:
			if r >= ' ' {
				put(r)
			}
		}
		i += size
	}
	return grid
}

// backdrop holds the composited terminal-sized cell grid rendered behind every
// config-editor screen. When art is false (rasterization failed), segment/line
// fall back to the legacy ░ shaded fill.
type backdrop struct {
	cells  [][]artCell
	width  int
	height int
	art    bool
}

// loadBackdrop rasterizes the embedded TUICONFIG.ANS art and composites it,
// centered, onto a width×height black canvas. On failure it returns a backdrop
// in fallback mode (art=false).
func loadBackdrop(width, height int) *backdrop {
	if width < artWidth {
		width = artWidth
	}
	if height < artHeight {
		height = artHeight
	}
	grid := rasterizeArt(tuiConfigANS)
	if len(grid) != artHeight || len(grid[0]) != artWidth {
		return &backdrop{width: width, height: height, art: false}
	}

	startCol := (width - artWidth) / 2
	startRow := (height - artHeight) / 2

	cells := make([][]artCell, height)
	for r := 0; r < height; r++ {
		cells[r] = make([]artCell, width)
		for c := 0; c < width; c++ {
			ar, ac := r-startRow, c-startCol
			if ar >= 0 && ar < artHeight && ac >= 0 && ac < artWidth {
				cells[r][c] = grid[ar][ac]
			} else {
				cells[r][c] = artCell{ch: ' ', fg: 7, bg: 0} // black margin
			}
		}
	}
	return &backdrop{cells: cells, width: width, height: height, art: true}
}

// renderCells renders a run of cells, grouping consecutive cells that share the
// same fg/bg into a single dosColor-styled span.
func renderCells(cells []artCell) string {
	if len(cells) == 0 {
		return ""
	}
	var b strings.Builder
	runFg, runBg := cells[0].fg, cells[0].bg
	var run strings.Builder
	flush := func() {
		if run.Len() > 0 {
			b.WriteString(dosColor(runBg, runFg).Render(run.String()))
			run.Reset()
		}
	}
	for _, c := range cells {
		if c.fg != runFg || c.bg != runBg {
			flush()
			runFg, runBg = c.fg, c.bg
		}
		run.WriteRune(c.ch)
	}
	flush()
	return b.String()
}

// segment returns the styled backdrop slice for row starting at col, width
// columns wide. Out-of-range requests are padded with black spaces so callers
// never need bounds checks. In fallback mode it returns the ░ shaded fill.
func (b *backdrop) segment(row, col, width int) string {
	if width <= 0 {
		return ""
	}
	if b == nil || !b.art {
		return bgFillStyle.Render(strings.Repeat("░", width))
	}
	if row < 0 || row >= len(b.cells) {
		out := make([]artCell, width)
		for i := range out {
			out[i] = artCell{ch: ' ', fg: 7, bg: 0}
		}
		return renderCells(out)
	}
	src := b.cells[row]
	out := make([]artCell, width)
	for i := 0; i < width; i++ {
		c := col + i
		if c >= 0 && c < len(src) {
			out[i] = src[c]
		} else {
			out[i] = artCell{ch: ' ', fg: 7, bg: 0}
		}
	}
	return renderCells(out)
}

// line returns the full-width styled backdrop row.
func (b *backdrop) line(row int) string {
	w := artWidth
	if b != nil {
		w = b.width
	}
	return b.segment(row, 0, w)
}

// parseCSIParams splits a CSI parameter string like "1;33;44" into ints,
// treating empty fields as 0 and ignoring a leading '?'.
func parseCSIParams(s string) []int {
	if s == "" {
		return nil
	}
	parts := strings.Split(strings.TrimPrefix(s, "?"), ";")
	out := make([]int, 0, len(parts))
	for _, p := range parts {
		if p == "" {
			out = append(out, 0)
			continue
		}
		n, err := strconv.Atoi(p)
		if err != nil {
			out = append(out, 0)
			continue
		}
		out = append(out, n)
	}
	return out
}

// ansiToCGA remaps an ANSI SGR color index (0-7, in ANSI's black/red/green/
// yellow/blue/magenta/cyan/white order) to the CGA/DOS palette index used by
// dosColors in colors.go (black/blue/green/cyan/red/magenta/brown/lightgray).
// This is the classic ANSI.SYS/DOS terminal-emulation swap of the red and
// blue bits (bit0 <-> bit2); without it, ANSI blue/red and yellow/cyan would
// render as their opposite CGA color.
func ansiToCGA(v int) int {
	return ((v & 1) << 2) | (v & 2) | ((v & 4) >> 2)
}

// applySGR updates DOS color state from SGR parameters. Returns the new
// (baseFg, bg, bold). Foreground 30-37 and background 40-47 are remapped
// from ANSI to CGA color order via ansiToCGA; bold (1) adds brightness to
// the foreground at render time.
func applySGR(params []int, baseFg, bg int, bold bool) (int, int, bool) {
	if len(params) == 0 {
		return 7, 0, false
	}
	for _, p := range params {
		switch {
		case p == 0:
			baseFg, bg, bold = 7, 0, false
		case p == 1:
			bold = true
		case p == 22:
			bold = false
		case p == 39:
			baseFg = 7
		case p == 49:
			bg = 0
		case p >= 30 && p <= 37:
			baseFg = ansiToCGA(p - 30)
		case p >= 40 && p <= 47:
			bg = ansiToCGA(p - 40)
		}
	}
	return baseFg, bg, bold
}
