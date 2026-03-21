package configeditor

import (
	"fmt"
	"strings"
)

// View implements tea.Model.
func (m Model) View() string {
	switch m.mode {
	case modeTopMenu:
		return m.viewTopMenu()
	case modeCategoryMenu:
		return m.viewCategoryMenu()
	case modeSysConfigMenu:
		return m.viewSysConfigMenu()
	case modeSysConfigEdit, modeSysConfigField:
		return m.viewSysConfigEdit()
	case modeRecordList, modeRecordReorder:
		return m.viewRecordList()
	case modeRecordEdit, modeRecordField:
		return m.viewRecordEdit()
	case modeLookupPicker:
		return m.viewLookupPicker()
	case modeExitConfirm, modeSaveConfirm:
		result := m.viewTopMenu()
		return m.overlayConfirmDialog(result, "-- Unsaved Changes --",
			"Save changes before exit? ")
	case modeHelp:
		result := m.viewTopMenu()
		return m.overlayHelpScreen(result)
	case modeDeleteConfirm:
		result := m.viewRecordList()
		return m.overlayConfirmDialog(result, "-- Delete Record --",
			"Delete this record? ")
	case modeWizardForm, modeWizardField:
		return m.viewWizardForm()
	case modeV3NetWizardStep:
		return m.viewV3NetWizard()
	case modeV3NetIdentity:
		return m.viewV3NetIdentity()
	case modeNavSaveConfirm:
		return m.viewNavSaveConfirm()
	case modeWizardExitConfirm:
		bg := m.viewWizardForm()
		return m.overlayConfirmDialog(bg, "-- Unsaved Wizard --",
			"Save before leaving?")
	case modeV3NetHubAreas:
		return m.viewV3NetHubAreas()
	case modeV3NetAreaDeleteConfirm:
		bg := m.viewV3NetHubAreas()
		return m.overlayConfirmDialog(bg, "-- Remove Area --",
			"Remove this area from message area config?")
	case modeV3NetAreaDeleteJAM:
		bg := m.viewV3NetHubAreas()
		return m.overlayConfirmDialog(bg, "-- Delete JAM Files --",
			"Delete JAM message base files? (DESTRUCTIVE)")
	case modeV3NetAreaInsert:
		return m.viewV3NetAreaInsert()
	case modeV3NetAreaRename:
		return m.viewV3NetAreaRename()
	case modeV3NetAreaRenameJAM:
		bg := m.viewV3NetAreaRename()
		return m.overlayConfirmDialog(bg, "-- Rename JAM Files --",
			"Rename JAM base path on disk?")
	case modeV3NetAreaBrowser:
		return m.viewV3NetAreaBrowser()
	case modeRegistryBrowser:
		return m.viewRegistryBrowser()
	}
	return m.viewTopMenu()
}

// globalHeaderLine returns the persistent global header shown on every screen.
func (m Model) globalHeaderLine() string {
	title := centerText("-- ViSiON/3 Configuration Editor v1.0 --", m.width)
	return globalHeaderBarStyle.Render(title)
}

// viewTopMenu renders the top-level menu.
func (m Model) viewTopMenu() string {
	var b strings.Builder

	// Global header
	b.WriteString(m.globalHeaderLine())
	b.WriteByte('\n')

	bgLine := bgFillStyle.Render(strings.Repeat("░", m.width))

	// Menu box dimensions
	boxW := 42
	// Box: top border + header + empty + items + empty + bottom border = items + 5
	// Fixed rows: global header(1) + box + message line(1) + help bar(1) = box + 3
	boxH := len(m.topItems) + 5

	// Vertical centering
	extraV := maxInt(0, m.height-boxH-3)
	topPad := extraV / 2
	bottomPad := extraV - topPad

	for i := 0; i < topPad; i++ {
		b.WriteString(bgLine)
		b.WriteByte('\n')
	}

	// Horizontal centering
	padL := maxInt(0, (m.width-boxW-2)/2)
	padR := maxInt(0, m.width-padL-boxW-2)

	// Top border
	b.WriteString(bgFillStyle.Render(strings.Repeat("░", padL)) +
		menuBorderStyle.Render("┌"+strings.Repeat("─", boxW)+"┐") +
		bgFillStyle.Render(strings.Repeat("░", maxInt(0, padR))))
	b.WriteByte('\n')

	// Header
	headerText := "ViSiON/3 Configuration"
	headerLine := menuBorderStyle.Render("│") +
		menuHeaderStyle.Render(centerText(headerText, boxW)) +
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
	for i, item := range m.topItems {
		content := fmt.Sprintf("  %s. %s", item.Key, item.Label)
		content = padRight(content, boxW)

		var styled string
		if i == m.topCursor {
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

	// Empty line
	b.WriteString(emptyLine)
	b.WriteByte('\n')

	// Bottom border
	b.WriteString(bgFillStyle.Render(strings.Repeat("░", padL)) +
		menuBorderStyle.Render("└"+strings.Repeat("─", boxW)+"┘") +
		bgFillStyle.Render(strings.Repeat("░", maxInt(0, padR))))
	b.WriteByte('\n')

	// Message line
	if m.message != "" {
		msgLine := bgFillStyle.Render(strings.Repeat("░", padL)) +
			flashMessageStyle.Render(" "+padRight(m.message, boxW)) +
			bgFillStyle.Render(strings.Repeat("░", maxInt(0, padR+1)))
		b.WriteString(msgLine)
	} else {
		b.WriteString(bgLine)
	}
	b.WriteByte('\n')

	// Bottom fill
	for i := 0; i < bottomPad; i++ {
		b.WriteString(bgLine)
		b.WriteByte('\n')
	}

	// Help bar
	helpText := centerText("Alt-H Help  |  ESC/Q Quit", m.width)
	b.WriteString(helpBarStyle.Render(helpText))

	return b.String()
}

// viewSysConfigMenu renders the system configuration inner menu.
func (m Model) viewSysConfigMenu() string {
	var b strings.Builder

	// Global header
	b.WriteString(m.globalHeaderLine())
	b.WriteByte('\n')

	bgLine := bgFillStyle.Render(strings.Repeat("░", m.width))

	boxW := 38
	// Box: border + header + empty + sysMenuItems + "Q. Return" + empty + border
	boxH := len(m.sysMenuItems) + 6

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
		menuHeaderStyle.Render(centerText("System Configuration", boxW)) +
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
	for i, item := range m.sysMenuItems {
		content := fmt.Sprintf("  %d. %s", i+1, item.Label)
		content = padRight(content, boxW)

		var styled string
		if i == m.sysMenuCursor {
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

// viewNavSaveConfirm renders the save-and-continue dialog over the appropriate
// background screen.
func (m Model) viewNavSaveConfirm() string {
	// Render the background based on where we came from.
	var bg string
	switch m.navSaveSourceMode {
	case modeV3NetHubAreas:
		bg = m.viewV3NetHubAreas()
	case modeRecordEdit, modeRecordField:
		bg = m.viewRecordEdit()
	case modeRecordList:
		bg = m.viewRecordList()
	case modeCategoryMenu:
		bg = m.viewCategoryMenu()
	default:
		bg = m.viewTopMenu()
	}
	return m.overlayConfirmDialog(bg, "-- Unsaved Changes --",
		"Save changes before leaving?")
}
