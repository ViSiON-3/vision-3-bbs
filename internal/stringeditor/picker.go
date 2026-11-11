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

	"github.com/ViSiON-3/vision-3-bbs/internal/tuiart/colorpick"
)

// pickerNoBG is the background choice that inserts no background code.
const pickerNoBG = colorpick.NoBG

// pickerSample is the text the dialog previews in the chosen colours.
const pickerSample = "The quick brown fox"

// openPicker enters the picker from edit mode. The last choice is kept
// between openings, so inserting the same colour twice is two keystrokes.
func (m Model) openPicker() (tea.Model, tea.Cmd) {
	m.mode = modePicker
	m.pick.Row = colorpick.RowFG
	return m, nil
}

// updatePicker handles keys in the picker. Left and Right move along the
// row, Up and Down (or Tab) switch rows, Enter inserts the chosen codes and
// Esc returns to the input with its value as it was.
func (m Model) updatePicker(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.pick.Key(msg) {
		return m, nil
	}
	switch msg.Type {
	case tea.KeyEnter:
		code, ok := m.pickerCodeBefore(m.runeAfterCursor())
		if !ok {
			// Only a dark foreground with a background before a digit 0-5
			// has no safe form; say so and leave the input as it was.
			m.mode = modeEdit
			m.editErr = fmt.Sprintf("%s before the digit %c would read as |B%d%c; move the cursor or type a space first",
				code, m.runeAfterCursor(), m.pick.BG, m.runeAfterCursor())
			return m, nil
		}
		m.insertAtCursor(code)
		m.mode = modeEdit
		m.editErr = ""
	case tea.KeyEscape:
		m.mode = modeEdit
	}
	return m, nil
}

// pickerCode is the text Enter inserts: the foreground code, then the
// background code when one is chosen. The foreground goes first because the
// BBS expands |00-|07 to sequences that reset every attribute, which would
// wipe a background set before them; a background set after survives.
func (m Model) pickerCode() string {
	code, _ := m.pickerCodeBefore(0)
	return code
}

// pickerCodeBefore is pickerCode for an insertion followed by next, the rune
// after the cursor (0 at the end of the input). A background code |Bn
// followed by a digit 0-5 would be read by the BBS as |Bn0-|Bn5, a bright
// background, swallowing the digit. Before such a digit the background goes
// first and the foreground, a bright one that does not reset it, last. A dark
// foreground resets the background whichever side it is on, so that pair has
// no safe form before such a digit: ok is false and the code is for the
// message only.
func (m Model) pickerCodeBefore(next rune) (code string, ok bool) {
	fg := fmt.Sprintf("|%02d", m.pick.FG)
	if m.pick.BG == pickerNoBG {
		return fg, true
	}
	bg := fmt.Sprintf("|B%d", m.pick.BG)
	if next < '0' || next > '5' {
		return fg + bg, true
	}
	if m.pick.FG >= 8 {
		return bg + fg, true
	}
	return fg + bg, false
}

// runeAfterCursor returns the rune just after the edit cursor, or 0 at the
// end of the input.
func (m Model) runeAfterCursor() rune {
	runes := []rune(m.textInput.Value())
	pos := m.textInput.Position()
	if pos < 0 || pos >= len(runes) {
		return 0
	}
	return runes[pos]
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
const pickerWidth = colorpick.Width

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

	st := pickerStyles()
	fg, bg := m.pick.FGRow(st), m.pick.BGRow(st)
	code := m.pickerCode()
	sample := dialogTextStyle.Render(code+" ") + RenderColorString(code+pickerSample, inner-len(code)-1)

	lines := []string{
		dialogBorderStyle.Render("╔" + strings.Repeat("═", inner) + "╗"),
		center(dialogTextStyle.Render("Insert Colour")),
		row(fg),
		row(bg),
		row(sample),
		center(dialogTextStyle.Render("←→ colour   ↑↓ row   Enter insert   Esc cancel")),
		dialogBorderStyle.Render("╚" + strings.Repeat("═", inner) + "╝"),
	}
	return m.composeOverlay(background, lines, pickerWidth)
}

// pickerStyles are this editor's dialog colours, for the shared picker.
func pickerStyles() colorpick.Styles {
	return colorpick.Styles{Text: dialogTextStyle, Button: dialogButtonStyle, Active: dialogButtonActiveStyle}
}
