package configeditor

import (
	"fmt"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/ViSiON-3/vision-3-bbs/internal/tuiart"
	"github.com/ViSiON-3/vision-3-bbs/internal/tuiart/colorpick"
)

// The colour attribute picker: a dialog over the edit screen for an
// ftColorAttr field, a DOS text attribute (background*16 + foreground) such
// as the theme's Yes/No colours. It uses the colour picker the string
// editor uses (internal/tuiart/colorpick), with a foreground row and a
// background row, and previews the choice as the prompt would draw it.
// Backgrounds 8-15 are the blink bit, which the picker keeps as it was.

// attrEditor is the dialog's state: the field, the value it opened on, and
// the choice.
type attrEditor struct {
	field fieldDef
	was   int
	pick  colorpick.Picker
}

// attrValue reads an ftColorAttr field's value, 0 when it does not parse.
func attrValue(f fieldDef) int {
	n, err := strconv.Atoi(strings.TrimSpace(f.Get()))
	if err != nil || n < 0 || n > 255 {
		return 0
	}
	return n
}

// startAttrEdit opens the picker for f, the current field.
func (m Model) startAttrEdit(f fieldDef) (Model, tea.Cmd) {
	was := attrValue(f)
	m.attr = attrEditor{field: f, was: was, pick: colorpick.FromAttr(was)}
	m.mode = modeColorAttr
	return m, nil
}

// updateColorAttr handles keys in the attribute picker: arrows choose,
// Enter sets the field, T types the number instead, Esc leaves it.
func (m Model) updateColorAttr(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.attr.pick.Key(msg) {
		return m, nil
	}
	switch msg.Type {
	case tea.KeyEnter:
		val := strconv.Itoa(m.attr.pick.Attr(m.attr.was))
		// Compare with what the field held, not with the value the picker
		// opened on: a stored value it could not read (300, say) opened
		// as 0, and setting 0 is still a change to save.
		before := m.attr.field.Get()
		if err := m.attr.field.Set(val); err != nil {
			m.message = err.Error()
		} else if val != before {
			m.dirty = true
			m.message = ""
		}
		m.mode = modeSysConfigEdit
	case tea.KeyEscape:
		m.mode = modeSysConfigEdit
	case tea.KeyRunes:
		if strings.EqualFold(string(msg.Runes), "t") {
			m.mode = modeSysConfigEdit
			return m.startSysFieldEdit()
		}
	}
	return m, nil
}

// attrSample draws attr as a Yes/No prompt would, with its number.
func attrSample(attr int) string {
	return tuiart.Style(byte(attr)).Render(" Yes ") + " " + tuiart.Style(byte(attr)).Render(" No ") +
		dialogTextStyle.Render(fmt.Sprintf("   = %d", attr))
}

// viewColorAttr draws the attribute picker over the edit screen.
func (m Model) viewColorAttr() string {
	lines := strings.Split(m.viewSysConfigEdit(), "\n")
	a := m.attr
	st := rampStyles()
	w := colorpick.Width
	inner := w - 2
	side := dialogBorderStyle.Render("│")
	row := func(content string) string {
		if pad := inner - lipgloss.Width(content); pad > 0 {
			content += dialogTextStyle.Render(strings.Repeat(" ", pad))
		}
		return side + content + side
	}
	text := func(s string) string { return row(dialogTextStyle.Render(s)) }
	dialog := []string{
		dialogBorderStyle.Render("┌" + strings.Repeat("─", inner) + "┐"),
		side + dialogTitleStyle.Render(centerText(a.field.Label, inner)) + side,
		text(""),
		text(rowLabel(a.pick.Row == colorpick.RowFG, "Foreground")),
		row(a.pick.FGRow(st)),
		text(rowLabel(a.pick.Row == colorpick.RowBG, "Background")),
		row(a.pick.BGRow(st)),
		text(""),
		row(dialogTextStyle.Render(" Preview:  ") + attrSample(a.pick.Attr(a.was))),
		text(""),
		text(" ←→ colour   ↑↓ row   Enter set   T type a number   Esc cancel"),
		dialogBorderStyle.Render("└" + strings.Repeat("─", inner) + "┘"),
	}
	startRow := max(0, (m.height-len(dialog))/2)
	startCol := max(0, (m.width-w)/2)
	for i, dl := range dialog {
		at := startRow + i
		if at < len(lines) {
			lines[at] = padToCol(lines[at], startCol) + dl + skipToCol(lines[at], startCol+w)
		}
	}
	return strings.Join(lines, "\n")
}

// rowLabel names a picker row, marked when the arrows move it.
func rowLabel(active bool, name string) string {
	if active {
		return " ► " + name + ":"
	}
	return "   " + name + ":"
}
