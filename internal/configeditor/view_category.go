package configeditor

import (
	"fmt"
	"strings"
)

// viewCategoryMenu renders a generic category sub-menu.
func (m Model) viewCategoryMenu() string {
	var b strings.Builder

	// Global header
	b.WriteString(m.globalHeaderLine())
	b.WriteByte('\n')

	bgLine := bgFillStyle.Render(strings.Repeat("░", m.width))

	boxW := 38
	// Box: border + header + empty + items + "Q. Return" + empty + border
	boxH := len(m.catMenuItems) + 6

	// Vertical centering: -3 for global header, message line, help bar
	extraV := maxInt(0, m.height-boxH-3)
	topPad := extraV / 2
	bottomPad := extraV - topPad

	for i := 0; i < topPad; i++ {
		b.WriteString(bgLine)
		b.WriteByte('\n')
	}

	padL := maxInt(0, (m.width-boxW-2)/2)
	padR := maxInt(0, m.width-padL-boxW-2)

	// Top border
	b.WriteString(bgFillStyle.Render(strings.Repeat("░", padL)) +
		menuBorderStyle.Render("┌"+strings.Repeat("─", boxW)+"┐") +
		bgFillStyle.Render(strings.Repeat("░", maxInt(0, padR))))
	b.WriteByte('\n')

	// Header
	headerLine := menuBorderStyle.Render("│") +
		menuHeaderStyle.Render(centerText(m.catMenuTitle, boxW)) +
		menuBorderStyle.Render("│")
	b.WriteString(bgFillStyle.Render(strings.Repeat("░", padL)) + headerLine +
		bgFillStyle.Render(strings.Repeat("░", maxInt(0, padR))))
	b.WriteByte('\n')

	// Empty line
	emptyLine := bgFillStyle.Render(strings.Repeat("░", padL)) +
		menuBorderStyle.Render("│") +
		menuItemStyle.Render(strings.Repeat(" ", boxW)) +
		menuBorderStyle.Render("│") +
		bgFillStyle.Render(strings.Repeat("░", maxInt(0, padR)))
	b.WriteString(emptyLine)
	b.WriteByte('\n')

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

		line := bgFillStyle.Render(strings.Repeat("░", padL)) +
			menuBorderStyle.Render("│") +
			styled +
			menuBorderStyle.Render("│") +
			bgFillStyle.Render(strings.Repeat("░", maxInt(0, padR)))
		b.WriteString(line)
		b.WriteByte('\n')
	}

	// Return item
	{
		content := padRight("  Q. Return", boxW)
		styled := menuItemStyle.Render(content)
		line := bgFillStyle.Render(strings.Repeat("░", padL)) +
			menuBorderStyle.Render("│") +
			styled +
			menuBorderStyle.Render("│") +
			bgFillStyle.Render(strings.Repeat("░", maxInt(0, padR)))
		b.WriteString(line)
		b.WriteByte('\n')
	}

	// Empty line
	b.WriteString(emptyLine)
	b.WriteByte('\n')

	// Bottom border
	b.WriteString(bgFillStyle.Render(strings.Repeat("░", padL)) +
		menuBorderStyle.Render("└"+strings.Repeat("─", boxW)+"┘") +
		bgFillStyle.Render(strings.Repeat("░", maxInt(0, padR))))
	b.WriteByte('\n')

	// Message/fill
	if m.message != "" {
		msgLine := bgFillStyle.Render(strings.Repeat("░", padL)) +
			flashMessageStyle.Render(" "+padRight(m.message, boxW)) +
			bgFillStyle.Render(strings.Repeat("░", maxInt(0, padR+1)))
		b.WriteString(msgLine)
	} else {
		b.WriteString(bgLine)
	}
	b.WriteByte('\n')

	for i := 0; i < bottomPad; i++ {
		b.WriteString(bgLine)
		b.WriteByte('\n')
	}

	helpText := centerText("Enter - Select  |  ESC/Q - Return", m.width)
	b.WriteString(helpBarStyle.Render(helpText))

	return b.String()
}
