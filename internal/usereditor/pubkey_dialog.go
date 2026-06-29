package usereditor

import (
	"fmt"
	"strings"

	"github.com/ViSiON-3/vision-3-bbs/internal/uitext"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// editingUser returns a pointer to the user currently being edited.
// All key dialog operations must go through this helper.
func (m *Model) editingUser() *user.User {
	return m.users[m.editIndex]
}

// openKeyDialog switches to modeKeyList and resets selection state.
func (m *Model) openKeyDialog() {
	m.mode = modeKeyList
	m.keySelected = 0
	m.keyDialogErr = ""
}

// keyDialogAdd validates and adds a public key line to the editing user.
// On error it stores the message in m.keyDialogErr (caller may inspect it).
func (m *Model) keyDialogAdd(line string) error {
	_, err := m.editingUser().AddPublicKey(line)
	if err != nil {
		m.keyDialogErr = err.Error()
		return err
	}
	m.keyDialogErr = ""
	m.dirty = true
	m.editDirty = true
	return nil
}

// keyDialogDelete removes the key identified by ref (1-based index or fingerprint).
func (m *Model) keyDialogDelete(ref string) error {
	_, err := m.editingUser().RemovePublicKey(ref)
	if err != nil {
		m.keyDialogErr = err.Error()
		return err
	}
	m.keyDialogErr = ""
	m.dirty = true
	m.editDirty = true
	// Keep keySelected in bounds
	keys, _ := m.editingUser().ListPublicKeys()
	if m.keySelected >= len(keys) && m.keySelected > 0 {
		m.keySelected = len(keys) - 1
	}
	return nil
}

// overlayKeyListDialog renders the WFC Keys list overlay over background.
func (m Model) overlayKeyListDialog(background string) string {
	u := m.editingUser()
	keys, unparseable := u.ListPublicKeys()

	lines := strings.Split(background, "\n")

	dialogW := 72
	listH := 8 // max keys shown
	if n := len(keys); n < listH {
		listH = n
	}
	// Header (title + empty + warn?) + list rows + footer
	headerRows := 2
	if unparseable > 0 {
		headerRows = 3
	}
	footerRows := 2                                    // empty + footer instructions
	dialogH := 1 + headerRows + listH + footerRows + 1 // border top + content + border bot

	startRow := (m.height - dialogH) / 2
	startCol := (m.width - dialogW) / 2
	if startRow < 0 {
		startRow = 0
	}
	if startCol < 0 {
		startCol = 0
	}

	border := dialogBorderStyle.Render("╔" + strings.Repeat("═", dialogW-2) + "╗")
	borderBot := dialogBorderStyle.Render("╚" + strings.Repeat("═", dialogW-2) + "╝")
	side := dialogBorderStyle.Render("║")

	mkLine := func(text string) string {
		pad := (dialogW - 2 - len(text)) / 2
		if pad < 0 {
			pad = 0
		}
		return side + dialogTextStyle.Render(strings.Repeat(" ", pad)+text+strings.Repeat(" ", max(0, dialogW-2-pad-len(text)))) + side
	}
	mkTitleLine := func(text string) string {
		pad := (dialogW - 2 - len(text)) / 2
		if pad < 0 {
			pad = 0
		}
		return side + dialogTitleStyle.Render(strings.Repeat(" ", pad)+text+strings.Repeat(" ", max(0, dialogW-2-pad-len(text)))) + side
	}
	emptyLine := side + dialogTextStyle.Render(strings.Repeat(" ", dialogW-2)) + side

	var dialogLines []string
	dialogLines = append(dialogLines, border)

	title := fmt.Sprintf("WFC Keys for %s  (%d registered)", u.Handle, len(u.PublicKeys))
	dialogLines = append(dialogLines, mkTitleLine(title))

	if unparseable > 0 {
		warn := fmt.Sprintf("WARNING: %d corrupt/unparseable key(s) stored", unparseable)
		dialogLines = append(dialogLines, mkLine(warn))
	}
	dialogLines = append(dialogLines, emptyLine)

	if len(keys) == 0 {
		dialogLines = append(dialogLines, mkLine("(no keys registered)"))
	} else {
		// Column widths: type (13) + fingerprint (47) + comment (rest)
		inner := dialogW - 2
		for i, k := range keys {
			fp := padRight(k.Fingerprint, 47)
			typ := padRight(k.Type, 13)
			cmt := k.Comment
			// Available space after type + fp + 2 spaces separator each
			avail := inner - 13 - 1 - 47 - 1
			if avail < 0 {
				avail = 0
			}
			if len(cmt) > avail {
				cmt = cmt[:avail]
			}
			row := typ + " " + fp + " " + padRight(cmt, avail)

			var rendered string
			if i == m.keySelected {
				rendered = side + buttonActiveStyle.Render(row) + side
			} else {
				rendered = side + dialogTextStyle.Render(row) + side
			}
			dialogLines = append(dialogLines, rendered)
		}
	}

	dialogLines = append(dialogLines, emptyLine)
	footerText := "[A]dd  [D]elete selected  [Esc] back"
	dialogLines = append(dialogLines, mkLine(footerText))
	dialogLines = append(dialogLines, borderBot)

	endCol := startCol + dialogW
	for i, dl := range dialogLines {
		row := startRow + i
		if row >= 0 && row < len(lines) {
			left := padToCol(lines[row], startCol)
			right := skipToCol(lines[row], endCol)
			lines[row] = left + dl + right
		}
	}
	return strings.Join(lines, "\n")
}

// overlayKeyAddDialog renders the key-add input overlay.
func (m Model) overlayKeyAddDialog(background string) string {
	lines := strings.Split(background, "\n")
	dialogW := 72
	dialogH := 7
	startRow := (m.height - dialogH) / 2
	startCol := (m.width - dialogW) / 2
	if startRow < 0 {
		startRow = 0
	}
	if startCol < 0 {
		startCol = 0
	}

	border := dialogBorderStyle.Render("╔" + strings.Repeat("═", dialogW-2) + "╗")
	borderBot := dialogBorderStyle.Render("╚" + strings.Repeat("═", dialogW-2) + "╝")
	side := dialogBorderStyle.Render("║")

	mkTitleLine := func(text string) string {
		pad := (dialogW - 2 - len(text)) / 2
		if pad < 0 {
			pad = 0
		}
		return side + dialogTitleStyle.Render(strings.Repeat(" ", pad)+text+strings.Repeat(" ", max(0, dialogW-2-pad-len(text)))) + side
	}
	emptyLine := side + dialogTextStyle.Render(strings.Repeat(" ", dialogW-2)) + side

	// Input line
	tiView := m.textInput.View()
	inputVisLen := 1 + uitext.ApproximateVisibleLen(tiView)
	innerW := dialogW - 2
	rightPad := max(0, innerW-inputVisLen)
	inputLine := side +
		dialogTextStyle.Render(" ") +
		tiView +
		dialogTextStyle.Render(strings.Repeat(" ", rightPad)) +
		side

	// Error line (blank if no error)
	var errLine string
	if m.keyDialogErr != "" {
		errText := m.keyDialogErr
		if len(errText) > innerW {
			errText = errText[:innerW]
		}
		pad := 0
		errLine = side + dialogTextStyle.Render(strings.Repeat(" ", pad)+errText+strings.Repeat(" ", max(0, innerW-pad-len(errText)))) + side
	} else {
		errLine = emptyLine
	}

	dialogLines := []string{
		border,
		mkTitleLine("Paste OpenSSH Public Key (authorized_keys format)"),
		emptyLine,
		inputLine,
		errLine,
		mkTitleLine("[Enter] add  [Esc] cancel"),
		borderBot,
	}

	endCol := startCol + dialogW
	for i, dl := range dialogLines {
		row := startRow + i
		if row >= 0 && row < len(lines) {
			left := padToCol(lines[row], startCol)
			right := skipToCol(lines[row], endCol)
			lines[row] = left + dl + right
		}
	}
	return strings.Join(lines, "\n")
}
