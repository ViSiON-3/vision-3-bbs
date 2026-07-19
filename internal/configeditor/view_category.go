package configeditor

import (
	"fmt"
	"strings"
)

// viewCategoryMenu renders a generic category sub-menu.
func (m Model) viewCategoryMenu() string {
	var b strings.Builder

	row := 0

	// Global header
	b.WriteString(m.globalHeaderLine())
	b.WriteByte('\n')
	row++

	boxW := 38
	// Box: border + header + empty + items + "Q. Return" + empty + border
	boxH := len(m.catMenuItems) + 6

	// Vertical centering: -3 for global header, message line, help bar
	extraV := maxInt(0, m.height-boxH-3)
	topPad := extraV / 2
	bottomPad := extraV - topPad

	for i := 0; i < topPad; i++ {
		b.WriteString(m.backdrop.line(row))
		b.WriteByte('\n')
		row++
	}

	padL := maxInt(0, (m.width-boxW-2)/2)
	padR := maxInt(0, m.width-padL-boxW-2)

	// Top border
	b.WriteString(m.backdrop.segment(row, 0, padL) +
		menuBorderStyle.Render("┌"+strings.Repeat("─", boxW)+"┐") +
		m.backdrop.segment(row, m.width-maxInt(0, padR), maxInt(0, padR)))
	b.WriteByte('\n')
	row++

	// Header
	headerLine := menuBorderStyle.Render("│") +
		menuHeaderStyle.Render(centerText(m.catMenuTitle, boxW)) +
		menuBorderStyle.Render("│")
	b.WriteString(m.backdrop.segment(row, 0, padL) + headerLine +
		m.backdrop.segment(row, m.width-maxInt(0, padR), maxInt(0, padR)))
	b.WriteByte('\n')
	row++

	// Empty line
	emptyLine := m.backdrop.segment(row, 0, padL) +
		menuBorderStyle.Render("│") +
		menuItemStyle.Render(strings.Repeat(" ", boxW)) +
		menuBorderStyle.Render("│") +
		m.backdrop.segment(row, m.width-maxInt(0, padR), maxInt(0, padR))
	b.WriteString(emptyLine)
	b.WriteByte('\n')
	row++

	// Menu items
	for i, item := range m.catMenuItems {
		content := fmt.Sprintf("  %d. %s", i+1, item.Label)
		content = padRight(content, boxW)

		var styled string
		if i == m.catMenuCursor {
			styled = menuHighlightStyle.Render(content)
		} else {
			styled = menuItemStyle.Render(content)
		}

		line := m.backdrop.segment(row, 0, padL) +
			menuBorderStyle.Render("│") +
			styled +
			menuBorderStyle.Render("│") +
			m.backdrop.segment(row, m.width-maxInt(0, padR), maxInt(0, padR))
		b.WriteString(line)
		b.WriteByte('\n')
		row++
	}

	// Return item
	{
		content := padRight("  Q. Return", boxW)
		styled := menuItemStyle.Render(content)
		line := m.backdrop.segment(row, 0, padL) +
			menuBorderStyle.Render("│") +
			styled +
			menuBorderStyle.Render("│") +
			m.backdrop.segment(row, m.width-maxInt(0, padR), maxInt(0, padR))
		b.WriteString(line)
		b.WriteByte('\n')
		row++
	}

	// Empty line
	emptyLine = m.backdrop.segment(row, 0, padL) +
		menuBorderStyle.Render("│") +
		menuItemStyle.Render(strings.Repeat(" ", boxW)) +
		menuBorderStyle.Render("│") +
		m.backdrop.segment(row, m.width-maxInt(0, padR), maxInt(0, padR))
	b.WriteString(emptyLine)
	b.WriteByte('\n')
	row++

	// Bottom border
	b.WriteString(m.backdrop.segment(row, 0, padL) +
		menuBorderStyle.Render("└"+strings.Repeat("─", boxW)+"┘") +
		m.backdrop.segment(row, m.width-maxInt(0, padR), maxInt(0, padR)))
	b.WriteByte('\n')
	row++

	// Message/fill
	if m.message != "" {
		msgLine := m.backdrop.segment(row, 0, padL) +
			flashMessageStyle.Render(" "+padRight(m.message, boxW)) +
			m.backdrop.segment(row, m.width-(padR+1), padR+1)
		b.WriteString(msgLine)
	} else {
		b.WriteString(m.backdrop.line(row))
	}
	b.WriteByte('\n')
	row++

	for i := 0; i < bottomPad; i++ {
		b.WriteString(m.backdrop.line(row))
		b.WriteByte('\n')
		row++
	}

	helpText := centerText("Enter - Select  |  ESC/Q - Return", m.width)
	b.WriteString(helpBarStyle.Render(helpText))

	return b.String()
}
