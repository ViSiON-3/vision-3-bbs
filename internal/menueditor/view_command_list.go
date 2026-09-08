package menueditor

import (
	"fmt"
	"strings"

	"github.com/ViSiON-3/vision-3-bbs/internal/tuiart"
)

// viewCommandListScreen renders the command list browser for the selected menu.
// Faithfully recreates MENUEDIT.PAS Select_Command.
func (m Model) viewCommandListScreen() string {
	menuTitle := "" // friendly title for display headings
	if m.cmdsMenuIdx >= 0 && m.cmdsMenuIdx < len(m.menus) {
		e := m.menus[m.cmdsMenuIdx]
		menuTitle = e.Data.Title
		if menuTitle == "" {
			menuTitle = e.Name
		}
	}

	// Fixed rows: title(1) + box(border, section title, column header, empty,
	// listVisible, border = listVisible+5) + message(1) + help(1). The previous
	// count was 24 against an actual 23, which left the bottom row of any
	// terminal taller than 25 unpainted.
	topPad, bottomPad := tuiart.Split(m.height, listVisible+8)

	sc := tuiart.NewScreen(m.width, m.backdrop)

	// === Row 1: Title bar ===
	sc.Line(titleBarStyle.Render(centerText("-- ViSiON/3 Menu Editor v1.0 --", m.width)))
	sc.BgRows(topPad)

	// Box dimensions: MENUEDIT.PAS GrowBox(4,4,76,22) → 70 cols wide
	boxW := 70
	padL := max(0, (m.width-boxW-2)/2)
	padR := max(0, m.width-padL-boxW-2)

	box := func(content string) {
		sc.Line(sc.Pad(padL, padR,
			listBorderStyle.Render("│")+content+listBorderStyle.Render("│")))
	}

	// === Top border ===
	// MENUEDIT.PAS: Color(4,12) GrowBox → red bg, light red fg
	sc.Line(sc.Pad(padL, padR, listBorderStyle.Render("┌"+strings.Repeat("─", boxW)+"┐")))

	// === Section title inside box ===
	// MENUEDIT.PAS: Color(4,14) Center_Write('Editing Menu Commands for: ...')
	box(listHeaderStyle.Render(centerText(
		fmt.Sprintf("Editing Menu Commands for: %s", menuTitle), boxW)))

	// === Column header ===
	// MENUEDIT.PAS: 'Command Description   Keystroke(s)    Command(s)'
	box(listColTitleStyle.Render(padRight("   Node Activity          Keys       Command", boxW)))

	// === Empty separator ===
	box(listItemStyle.Render(strings.Repeat(" ", boxW)))

	// === Command list (listVisible rows) ===
	total := len(m.cmds)
	for row := 0; row < listVisible; row++ {
		idx := m.cmdScroll + row
		if idx < 0 || idx >= total {
			box(listItemStyle.Render(strings.Repeat(" ", boxW)))
			continue
		}
		box(m.renderCmdRow(idx, boxW))
	}

	// === Bottom border ===
	sc.Line(sc.Pad(padL, padR, listBorderStyle.Render("└"+strings.Repeat("─", boxW)+"┘")))

	// === Message or fill ===
	if m.message != "" {
		sc.Line(sc.Pad(padL, padR+1, flashMessageStyle.Render(" "+padRight(m.message, boxW))))
	} else {
		sc.BgLine()
	}

	sc.BgRows(bottomPad)

	// === Help bar ===
	// MENUEDIT.PAS: 'F2 Delete Command  F5 Add New Command  ALT-H Help  ESC Exits'
	sc.Last(helpBarStyle.Render(centerText(
		"(Enter) Edit  F2 Delete  F5 Add New Command  ESC Back", m.width)))

	// Overlay dialogs
	result := sc.String()
	if m.mode == modeDeleteCmdConfirm {
		desc := ""
		if m.cmdCursor >= 0 && m.cmdCursor < len(m.cmds) {
			desc = m.cmds[m.cmdCursor].NodeActivity
			if desc == "" {
				desc = m.cmds[m.cmdCursor].Keys
			}
		}
		result = m.overlayConfirmDialog(result,
			"-- Delete Command --",
			fmt.Sprintf("Delete command '%s'? ", desc))
	}

	return result
}

// renderCmdRow renders a single command entry row in the list.
func (m Model) renderCmdRow(idx int, boxW int) string {
	cmd := m.cmds[idx]
	isHighlight := idx == m.cmdCursor

	// Columns: node activity (22), keys (10), command (rest)
	activity := padRight(cmd.NodeActivity, 22)
	keys := padRight(cmd.Keys, 10)
	command := padRight(cmd.Command, boxW-36)
	content := "   " + activity + keys + command
	if len(content) < boxW {
		content += strings.Repeat(" ", boxW-len(content))
	} else if len(content) > boxW {
		content = content[:boxW]
	}

	if isHighlight {
		return listHighlightStyle.Render(content)
	}
	return listItemStyle.Render(content)
}
