package menueditor

import (
	"fmt"
	"strings"

	"github.com/ViSiON-3/vision-3-bbs/internal/tuiart"
)

// viewMenuListScreen renders the main menu browser.
// Faithfully recreates MENUEDIT.PAS Open_Screen + Select_Menu.
func (m Model) viewMenuListScreen() string {
	// Fixed rows: title(1) + box(border, column header, empty, listVisible,
	// border = listVisible+4) + message(1) + help(1). This screen has no
	// section-title row, so it is one shorter than the command list.
	topPad, bottomPad := tuiart.Split(m.height, listVisible+7)

	sc := tuiart.NewScreen(m.width, m.backdrop)

	// === Row 1: Title bar ===
	// MENUEDIT.PAS: Color(8,15) Center_Write('ViSiON/2 MENU EDITOR v1.0')
	sc.Line(titleBarStyle.Render(centerText("-- ViSiON/3 Menu Editor v1.0 --", m.width)))
	sc.BgRows(topPad)

	// Box dimensions: MENUEDIT.PAS GrowBox(15,5,65,22) → 50 cols wide
	boxW := 50
	padL := max(0, (m.width-boxW-2)/2)
	padR := max(0, m.width-padL-boxW-2)

	box := func(content string) {
		sc.Line(sc.Pad(padL, padR,
			listBorderStyle.Render("│")+content+listBorderStyle.Render("│")))
	}

	// === Top border ===
	// MENUEDIT.PAS: Color(3,11) GrowBox → cyan bg, light cyan fg
	sc.Line(sc.Pad(padL, padR, listBorderStyle.Render("┌"+strings.Repeat("─", boxW)+"┐")))

	// === Column header ===
	// MENUEDIT.PAS: Color(11,3) ' Menu Title           File Names'
	// nameColW=14: 3+14+1=18 chars before file column, leaving 32 for filenames
	box(listColTitleStyle.Render(padRight("   Menu Title     File Names", boxW)))

	// === Empty separator ===
	box(listItemStyle.Render(strings.Repeat(" ", boxW)))

	// === Menu list (listVisible rows) ===
	total := len(m.menus)
	for row := 0; row < listVisible; row++ {
		idx := m.menuScroll + row
		if idx < 0 || idx >= total {
			box(listItemStyle.Render(strings.Repeat(" ", boxW)))
			continue
		}
		box(m.renderMenuRow(idx, boxW))
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
	// MENUEDIT.PAS: '(CR) Edits Menu  F10 Edits Menu Commands  F2 Delete  F5 Add Menu  ESC Exits'
	sc.Last(helpBarStyle.Render(centerText(
		"(Enter) Edit Menu  F10 Commands  F2 Delete  F5 Add Menu  ESC Exit", m.width)))

	// Overlay dialogs
	result := sc.String()
	switch m.mode {
	case modeDeleteMenuConfirm:
		name := ""
		if m.menuCursor >= 0 && m.menuCursor < len(m.menus) {
			name = m.menus[m.menuCursor].Name
		}
		result = m.overlayConfirmDialog(result,
			"-- Delete Menu --",
			fmt.Sprintf("Delete %s.MNU and all commands? ", name))
	case modeExitConfirm:
		result = m.overlayConfirmDialog(result,
			"-- Unsaved Changes --",
			"Save all changes before exit? ")
	case modeAddMenu:
		result = m.overlayInputDialog(result,
			"-- Create New Menu --",
			"New menu filename (8 chars): ",
			m.textInput.View())
	case modeHelp:
		result = m.overlayHelpScreen(result)
	}

	return result
}

// renderMenuRow renders a single menu entry row in the list.
func (m Model) renderMenuRow(idx int, boxW int) string {
	entry := m.menus[idx]
	isHighlight := idx == m.menuCursor

	// Build: "   {title:14} {name}.MNU / {name}.CFG"
	// nameColW=14: 3+14+1=18 chars before file column, leaving 32 for filenames
	const nameColW = 14
	displayTitle := entry.Data.Title
	if displayTitle == "" {
		displayTitle = entry.Name
	}
	name := padRight(displayTitle, nameColW)
	// An asterisk marks a file that comes from the overlay (menus.d).
	files := fmt.Sprintf("%s.MNU%s / %s.CFG%s", entry.Name, overlayMark(entry.MnuOverlay), entry.Name, overlayMark(entry.CfgOverlay))
	files = padRight(files, boxW-nameColW-4) // 4 = 3 prefix + 1 separator
	content := "   " + name + " " + files
	if len(content) < boxW {
		content += strings.Repeat(" ", boxW-len(content))
	} else if len(content) > boxW {
		content = content[:boxW]
	}

	if isHighlight {
		// MENUEDIT.PAS: Color(1,15) for highlighted row
		return listHighlightStyle.Render(content)
	}
	return listItemStyle.Render(content)
}

func overlayMark(fromOverlay bool) string {
	if fromOverlay {
		return "*"
	}
	return ""
}
