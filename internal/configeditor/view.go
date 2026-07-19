package configeditor

import (
	"fmt"
	"strings"
)

// View implements tea.Model.
func (m Model) View() string {
	if m.splashActive {
		return m.viewSplash()
	}
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
		if m.ftnWizard != nil && m.ftnWizard.hasData() {
			bg = m.viewFTNWizardForm()
		}
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
	case modeFTNWizardForm, modeFTNWizardField:
		return m.viewFTNWizardForm()
	case modeFTNNetworkBrowser:
		return m.viewFTNNetworkBrowser()
	case modeFTNAreaBrowser, modeFTNAreaDownloading:
		return m.viewFTNAreaBrowser()
	}
	return m.viewTopMenu()
}

// globalHeaderLine returns the persistent global header shown on every screen.
func (m Model) globalHeaderLine() string {
	title := centerText("-- ViSiON/3 Configuration Editor v1.0 --", m.width)
	return globalHeaderBarStyle.Render(title)
}

// viewSplash renders the full backdrop art alone (no menu box), shown briefly
// at startup before the top menu appears.
func (m Model) viewSplash() string {
	rows := make([]string, m.height)
	for r := 0; r < m.height; r++ {
		rows[r] = m.backdrop.line(r)
	}
	return strings.Join(rows, "\n")
}

// viewTopMenu renders the top-level menu.
func (m Model) viewTopMenu() string {
	var b strings.Builder

	row := 0

	// Global header
	b.WriteString(m.globalHeaderLine())
	b.WriteByte('\n')
	row++

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
		b.WriteString(m.backdrop.line(row))
		b.WriteByte('\n')
		row++
	}

	// Horizontal centering
	padL := maxInt(0, (m.width-boxW-2)/2)
	padR := maxInt(0, m.width-padL-boxW-2)

	// Top border
	b.WriteString(m.backdrop.segment(row, 0, padL) +
		menuBorderStyle.Render("┌"+strings.Repeat("─", boxW)+"┐") +
		m.backdrop.segment(row, m.width-maxInt(0, padR), maxInt(0, padR)))
	b.WriteByte('\n')
	row++

	// Header
	headerText := "ViSiON/3 Configuration"
	headerLine := menuBorderStyle.Render("│") +
		menuHeaderStyle.Render(centerText(headerText, boxW)) +
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
	for i, item := range m.topItems {
		content := fmt.Sprintf("  %s. %s", item.Key, item.Label)
		content = padRight(content, boxW)

		var styled string
		if i == m.topCursor {
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

	// Message line
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

	// Bottom fill
	for i := 0; i < bottomPad; i++ {
		b.WriteString(m.backdrop.line(row))
		b.WriteByte('\n')
		row++
	}

	// Help bar
	helpText := centerText("Alt-H Help  |  ESC/Q Quit", m.width)
	b.WriteString(helpBarStyle.Render(helpText))

	return b.String()
}

// viewSysConfigMenu renders the system configuration inner menu.
func (m Model) viewSysConfigMenu() string {
	var b strings.Builder

	row := 0

	// Global header
	b.WriteString(m.globalHeaderLine())
	b.WriteByte('\n')
	row++

	boxW := 38
	// Box: border + header + empty + sysMenuItems + "Q. Return" + empty + border
	boxH := len(m.sysMenuItems) + 6

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
		menuHeaderStyle.Render(centerText("System Configuration", boxW)) +
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
	for i, item := range m.sysMenuItems {
		// Hotkey display matches input mapping: items 1-9 use their digit,
		// item 10 uses '0'.
		content := fmt.Sprintf("  %d. %s", (i+1)%10, item.Label)
		content = padRight(content, boxW)

		var styled string
		if i == m.sysMenuCursor {
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
