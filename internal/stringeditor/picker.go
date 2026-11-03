package stringeditor

// The colour picker: a dialog opened with F2 while editing a value that
// inserts a foreground pipe code, followed by a background code when one is
// chosen, at the input cursor (#622). It offers exactly the codes the BBS renders,
// |00-|15 and |B0-|B7, drawn in the same palette the list preview uses, with
// a sample line rendered by that preview so what the dialog shows is what the
// caller sees.

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ViSiON-3/vision-3-bbs/internal/tuiart"
)

const (
	// pickerRowFG and pickerRowBG name the two swatch rows.
	pickerRowFG = 0
	pickerRowBG = 1
	// pickerNoBG is the background choice that inserts no background code.
	pickerNoBG = -1
	// pickerSample is the text the dialog previews in the chosen colours.
	pickerSample = "The quick brown fox"
)

// openPicker enters the picker from edit mode. The last choice is kept
// between openings, so inserting the same colour twice is two keystrokes.
func (m Model) openPicker() (tea.Model, tea.Cmd) {
	m.mode = modePicker
	m.pickRow = pickerRowFG
	return m, nil
}

// updatePicker handles keys in the picker. Left and Right move along the
// row, Up and Down (or Tab) switch rows, Enter inserts the chosen codes and
// Esc returns to the input with its value as it was.
func (m Model) updatePicker(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyLeft:
		if m.pickRow == pickerRowFG {
			m.pickFG = (m.pickFG + 15) % 16
		} else {
			m.pickBG = wrapBG(m.pickBG - 1)
		}
	case tea.KeyRight:
		if m.pickRow == pickerRowFG {
			m.pickFG = (m.pickFG + 1) % 16
		} else {
			m.pickBG = wrapBG(m.pickBG + 1)
		}
	case tea.KeyUp, tea.KeyDown, tea.KeyTab:
		m.pickRow = 1 - m.pickRow
	case tea.KeyEnter:
		m.insertAtCursor(m.pickerCode())
		m.mode = modeEdit
		m.editErr = ""
	case tea.KeyEscape:
		m.mode = modeEdit
	}
	return m, nil
}

// wrapBG keeps a background choice within pickerNoBG..7, wrapping at the ends.
func wrapBG(n int) int {
	if n < pickerNoBG {
		return 7
	}
	if n > 7 {
		return pickerNoBG
	}
	return n
}

// pickerCode is the text Enter inserts: the foreground code, then the
// background code when one is chosen. The foreground goes first because the
// BBS expands |00-|07 to sequences that reset every attribute, which would
// wipe a background set before them; a background set after survives.
func (m Model) pickerCode() string {
	code := fmt.Sprintf("|%02d", m.pickFG)
	if m.pickBG != pickerNoBG {
		code += fmt.Sprintf("|B%d", m.pickBG)
	}
	return code
}

// insertAtCursor puts s into the edit input at the cursor and leaves the
// cursor after it. Positions count runes, as the input does.
func (m *Model) insertAtCursor(s string) {
	runes := []rune(m.textInput.Value())
	pos := m.textInput.Position()
	if pos < 0 {
		pos = 0
	}
	if pos > len(runes) {
		pos = len(runes)
	}
	m.textInput.SetValue(string(runes[:pos]) + s + string(runes[pos:]))
	m.textInput.SetCursor(pos + len([]rune(s)))
}

// pickerWidth is the dialog's outer width: sixteen four-cell swatches inside
// the border, which also fits the nine background cells and the sample.
const pickerWidth = 16*4 + 2

// overlayPicker draws the picker dialog centred over the rendered screen.
func (m Model) overlayPicker(background string) string {
	inner := pickerWidth - 2
	side := dialogBorderStyle.Render("║")
	row := func(content string) string {
		if pad := inner - visualLen(content); pad > 0 {
			content += dialogTextStyle.Render(strings.Repeat(" ", pad))
		}
		return side + content + side
	}
	center := func(text string) string {
		pad := max(0, (inner-visualLen(text))/2)
		return row(dialogTextStyle.Render(strings.Repeat(" ", pad)) + text)
	}

	var fg strings.Builder
	for n := 0; n < 16; n++ {
		fg.WriteString(pickerSwatch(fmt.Sprintf("%02d", n), swatchStyleFG(n), m.pickRow == pickerRowFG && m.pickFG == n))
	}
	var bg strings.Builder
	bg.WriteString(pickerSwatch("--", dialogButtonStyle, m.pickRow == pickerRowBG && m.pickBG == pickerNoBG))
	for n := 0; n < 8; n++ {
		bg.WriteString(pickerSwatch(fmt.Sprintf("B%d", n), tuiart.Color(bgCodeToDOS[n], dosWhite), m.pickRow == pickerRowBG && m.pickBG == n))
	}
	code := m.pickerCode()
	sample := dialogTextStyle.Render(code+" ") + RenderColorString(code+pickerSample, inner-len(code)-1)

	lines := []string{
		dialogBorderStyle.Render("╔" + strings.Repeat("═", inner) + "╗"),
		center(dialogTextStyle.Render("Insert Colour")),
		row(fg.String()),
		row(bg.String()),
		row(sample),
		center(dialogTextStyle.Render("←→ colour   ↑↓ row   Enter insert   Esc cancel")),
		dialogBorderStyle.Render("╚" + strings.Repeat("═", inner) + "╝"),
	}
	return m.composeOverlay(background, lines, pickerWidth)
}

// pickerSwatch draws one four-cell swatch: the code in its colour, in
// brackets when it is the selected cell.
func pickerSwatch(code string, style interface{ Render(...string) string }, selected bool) string {
	if selected {
		return dialogButtonActiveStyle.Render("[") + style.Render(code) + dialogButtonActiveStyle.Render("]")
	}
	return dialogTextStyle.Render(" ") + style.Render(code) + dialogTextStyle.Render(" ")
}

// swatchStyleFG draws foreground colour n on black, as the list preview
// does; black itself sits on dark gray so it can be seen.
func swatchStyleFG(n int) interface{ Render(...string) string } {
	if n == dosBlack {
		return tuiart.Color(dosDarkGray, dosBlack)
	}
	return tuiart.Color(dosBlack, n)
}
