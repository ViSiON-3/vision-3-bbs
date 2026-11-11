// Package colorpick is the colour picker the TUI editors share: a row of the
// sixteen foreground colours and, optionally, a row of the eight backgrounds,
// each drawn as a swatch in the BBS palette, so what is picked is what a
// caller sees. It holds the choice and the keys that move it; each editor
// decides what Enter and Esc do, and draws the dialog around the rows in its
// own colours.
package colorpick

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/ViSiON-3/vision-3-bbs/internal/tuiart"
)

// NoBG is the background choice that picks no background.
const NoBG = -1

// The rows.
const (
	RowFG = 0
	RowBG = 1
)

// SwatchWidth is the cells one swatch takes, and Width the dialog's outer
// width: sixteen swatches inside a one-cell border each side.
const (
	SwatchWidth = 4
	Width       = 16*SwatchWidth + 2
)

// bgToDOS maps a |Bn background code to its DOS colour.
var bgToDOS = [8]int{0, 4, 2, 6, 1, 5, 3, 7}

// DOS colours the swatches use.
const (
	dosBlack    = 0
	dosDarkGray = 8
	dosWhite    = 15
)

// Styles are the dialog colours of the editor the picker sits in: its plain
// text, an unselected button, and the selection brackets.
type Styles struct {
	Text, Button, Active lipgloss.Style
}

// Picker is a colour choice: FG 0-15, BG 0-7 or NoBG, and the row the cursor
// is on. With FGOnly there is no background row.
type Picker struct {
	FG, BG, Row int
	FGOnly      bool
}

// New returns a picker on foreground fg with no background.
func New(fg int, fgOnly bool) Picker {
	return Picker{FG: fg, BG: NoBG, FGOnly: fgOnly}
}

// Key moves the choice for Left, Right (along the row), Up, Down and Tab
// (between rows), and reports whether msg was one of them.
func (p *Picker) Key(msg tea.KeyMsg) bool {
	switch msg.Type {
	case tea.KeyLeft:
		if p.Row == RowFG {
			p.FG = (p.FG + 15) % 16
		} else {
			p.BG = wrapBG(p.BG - 1)
		}
	case tea.KeyRight:
		if p.Row == RowFG {
			p.FG = (p.FG + 1) % 16
		} else {
			p.BG = wrapBG(p.BG + 1)
		}
	case tea.KeyUp, tea.KeyDown, tea.KeyTab:
		if !p.FGOnly {
			p.Row = 1 - p.Row
		}
	default:
		return false
	}
	return true
}

// wrapBG keeps a background choice within NoBG..7, wrapping at the ends.
func wrapBG(n int) int {
	if n < NoBG {
		return 7
	}
	if n > 7 {
		return NoBG
	}
	return n
}

// FGRow draws the sixteen foreground swatches, the chosen one bracketed
// while the cursor is on the row.
func (p Picker) FGRow(st Styles) string {
	var b strings.Builder
	for n := 0; n < 16; n++ {
		b.WriteString(Swatch(fmt.Sprintf("%02d", n), FGStyle(n), p.Row == RowFG && p.FG == n, st))
	}
	return b.String()
}

// BGRow draws "no background" and the eight background swatches.
func (p Picker) BGRow(st Styles) string {
	var b strings.Builder
	b.WriteString(Swatch("--", st.Button, p.Row == RowBG && p.BG == NoBG, st))
	for n := 0; n < 8; n++ {
		b.WriteString(Swatch(fmt.Sprintf("B%d", n), BGStyle(n), p.Row == RowBG && p.BG == n, st))
	}
	return b.String()
}

// Swatch draws one SwatchWidth-cell swatch: code in style, in brackets when
// selected.
func Swatch(code string, style interface{ Render(...string) string }, selected bool, st Styles) string {
	if selected {
		return st.Active.Render("[") + style.Render(code) + st.Active.Render("]")
	}
	return st.Text.Render(" ") + style.Render(code) + st.Text.Render(" ")
}

// FGStyle draws foreground colour n on black, as previews do; black itself
// sits on dark gray so it can be seen.
func FGStyle(n int) lipgloss.Style {
	if n == dosBlack {
		return tuiart.Color(dosDarkGray, dosBlack)
	}
	return tuiart.Color(dosBlack, n)
}

// BGStyle draws background code |Bn behind white.
func BGStyle(n int) lipgloss.Style {
	return tuiart.Color(bgToDOS[n], dosWhite)
}
