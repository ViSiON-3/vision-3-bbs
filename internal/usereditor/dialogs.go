package usereditor

import (
	"fmt"
	"strings"
)

// overlayConfirmDialog renders a confirmation dialog centered over the background.
// Recreates the UE.PAS Ask() procedure with GrowBox + centered title + Y/N buttons.
func (m Model) overlayConfirmDialog(background, title, question string) string {
	lines := strings.Split(background, "\n")

	// Dialog dimensions matching UE.PAS Ask(9,9,71,15)
	dialogW := 62
	dialogH := 7
	startRow := (m.height - dialogH) / 2
	startCol := (m.width - dialogW) / 2
	if startRow < 0 {
		startRow = 0
	}
	if startCol < 0 {
		startCol = 0
	}

	// Build dialog lines
	// UE.PAS: Ask_Colors → PColor=94 (magenta bg, yellow fg), NormColor=95 (magenta bg, white fg)
	border := dialogBorderStyle.Render("╔" + strings.Repeat("═", dialogW-2) + "╗")
	borderBot := dialogBorderStyle.Render("╚" + strings.Repeat("═", dialogW-2) + "╝")
	side := dialogBorderStyle.Render("║")

	// Title line
	titlePad := (dialogW - 2 - len(title)) / 2
	if titlePad < 0 {
		titlePad = 0
	}
	titleLine := side +
		dialogTitleStyle.Render(strings.Repeat(" ", titlePad)+title+strings.Repeat(" ", max(0, dialogW-2-titlePad-len(title)))) +
		side

	// Empty line
	emptyLine := side +
		dialogTextStyle.Render(strings.Repeat(" ", dialogW-2)) +
		side

	// Question line
	qPad := (dialogW - 2 - len(question)) / 2
	if qPad < 0 {
		qPad = 0
	}
	questionLine := side +
		dialogTextStyle.Render(strings.Repeat(" ", qPad)+question+strings.Repeat(" ", max(0, dialogW-2-qPad-len(question)))) +
		side

	// Button line: " Yes " (5) + "  " (2) + " No " (4) = 11 visible chars
	btnVisW := 11
	var yesBtn, noBtn string
	if m.confirmYes {
		yesBtn = buttonActiveStyle.Render(" Yes ")
		noBtn = buttonInactiveStyle.Render(" No ")
	} else {
		yesBtn = buttonInactiveStyle.Render(" Yes ")
		noBtn = buttonActiveStyle.Render(" No ")
	}
	btnGap := dialogTextStyle.Render("  ")
	btnContent := yesBtn + btnGap + noBtn
	btnPad := (dialogW - 2 - btnVisW) / 2
	buttonLine := side +
		dialogTextStyle.Render(strings.Repeat(" ", max(0, btnPad))) +
		btnContent +
		dialogTextStyle.Render(strings.Repeat(" ", max(0, dialogW-2-btnPad-btnVisW))) +
		side

	dialogLines := []string{border, titleLine, emptyLine, questionLine, emptyLine, buttonLine, borderBot}

	// Overlay dialog on background, preserving content on both sides
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

// overlayDeleteDialog renders the delete confirmation with explanation text.
func (m Model) overlayDeleteDialog(background, handle string) string {
	lines := strings.Split(background, "\n")

	dialogW := 62
	dialogH := 9
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

	mkLine := func(text string, style func(...string) string) string {
		pad := (dialogW - 2 - len(text)) / 2
		if pad < 0 {
			pad = 0
		}
		return side + style(strings.Repeat(" ", pad)+text+strings.Repeat(" ", max(0, dialogW-2-pad-len(text)))) + side
	}

	emptyLine := side + dialogTextStyle.Render(strings.Repeat(" ", dialogW-2)) + side

	title := "-- Automatic User Annihilator! --"
	question := fmt.Sprintf("Delete %s? ", handle)
	info := "User will lose access on next login."

	// Button line
	btnVisW := 11
	var yesBtn, noBtn string
	if m.confirmYes {
		yesBtn = buttonActiveStyle.Render(" Yes ")
		noBtn = buttonInactiveStyle.Render(" No ")
	} else {
		yesBtn = buttonInactiveStyle.Render(" Yes ")
		noBtn = buttonActiveStyle.Render(" No ")
	}
	btnPad := (dialogW - 2 - btnVisW) / 2
	buttonLine := side +
		dialogTextStyle.Render(strings.Repeat(" ", max(0, btnPad))) +
		yesBtn + dialogTextStyle.Render("  ") + noBtn +
		dialogTextStyle.Render(strings.Repeat(" ", max(0, dialogW-2-btnPad-btnVisW))) +
		side

	dialogLines := []string{
		border,
		mkLine(title, dialogTitleStyle.Render),
		emptyLine,
		mkLine(question, dialogTextStyle.Render),
		mkLine(info, dialogTextStyle.Render),
		emptyLine,
		buttonLine,
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

// overlayPurgeDialog renders the purge confirmation with explanation text.
func (m Model) overlayPurgeDialog(background, handle string) string {
	lines := strings.Split(background, "\n")

	dialogW := 62
	dialogH := 10
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

	mkLine := func(text string, style func(...string) string) string {
		pad := (dialogW - 2 - len(text)) / 2
		if pad < 0 {
			pad = 0
		}
		return side + style(strings.Repeat(" ", pad)+text+strings.Repeat(" ", max(0, dialogW-2-pad-len(text)))) + side
	}

	emptyLine := side + dialogTextStyle.Render(strings.Repeat(" ", dialogW-2)) + side

	title := "-- Purge User --"
	question := fmt.Sprintf("Purge %s now? ", handle)
	info1 := "This permanently removes the user record"
	info2 := "and deletes all associated data (infoforms, etc)."

	// Button line
	btnVisW := 11
	var yesBtn, noBtn string
	if m.confirmYes {
		yesBtn = buttonActiveStyle.Render(" Yes ")
		noBtn = buttonInactiveStyle.Render(" No ")
	} else {
		yesBtn = buttonInactiveStyle.Render(" Yes ")
		noBtn = buttonActiveStyle.Render(" No ")
	}
	btnPad := (dialogW - 2 - btnVisW) / 2
	buttonLine := side +
		dialogTextStyle.Render(strings.Repeat(" ", max(0, btnPad))) +
		yesBtn + dialogTextStyle.Render("  ") + noBtn +
		dialogTextStyle.Render(strings.Repeat(" ", max(0, dialogW-2-btnPad-btnVisW))) +
		side

	dialogLines := []string{
		border,
		mkLine(title, dialogTitleStyle.Render),
		emptyLine,
		mkLine(question, dialogTextStyle.Render),
		mkLine(info1, dialogTextStyle.Render),
		mkLine(info2, dialogTextStyle.Render),
		emptyLine,
		buttonLine,
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

// overlayInfoAlert renders an informational alert dialog (title + message + "Hit a Key").
func (m Model) overlayInfoAlert(background string) string {
	lines := strings.Split(background, "\n")

	dialogW := 62
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

	// Title line
	titlePad := (dialogW - 2 - len(m.alertTitle)) / 2
	if titlePad < 0 {
		titlePad = 0
	}
	titleLine := side +
		dialogTitleStyle.Render(strings.Repeat(" ", titlePad)+m.alertTitle+strings.Repeat(" ", max(0, dialogW-2-titlePad-len(m.alertTitle)))) +
		side

	// Empty line
	emptyLine := side +
		dialogTextStyle.Render(strings.Repeat(" ", dialogW-2)) +
		side

	// Message line
	msgPad := (dialogW - 2 - len(m.alertMessage)) / 2
	if msgPad < 0 {
		msgPad = 0
	}
	msgLine := side +
		dialogTextStyle.Render(strings.Repeat(" ", msgPad)+m.alertMessage+strings.Repeat(" ", max(0, dialogW-2-msgPad-len(m.alertMessage)))) +
		side

	// "Hit a Key" line
	hitText := "Hit a Key."
	hitPad := (dialogW - 2 - len(hitText)) / 2
	hitLine := side +
		dialogTitleStyle.Render(strings.Repeat(" ", hitPad)+hitText+strings.Repeat(" ", max(0, dialogW-2-hitPad-len(hitText)))) +
		side

	dialogLines := []string{border, titleLine, emptyLine, msgLine, emptyLine, hitLine, borderBot}

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

// overlayHelpScreen renders the help screen overlay.
// Recreates UE.PAS Help_Screen: Color(4,15) GrowBox(18,4,62,20)
func (m Model) overlayHelpScreen(background string) string {
	lines := strings.Split(background, "\n")

	dialogW := 46
	dialogH := 19 // number of lines in helpLines below
	startRow := (m.height - dialogH) / 2
	startCol := (m.width - dialogW) / 2
	if startRow < 0 {
		startRow = 0
	}

	// Help box styles: Red bg, white fg
	helpBorder := helpBoxStyle
	helpTitle := helpTitleStyle

	border := helpBorder.Render("╔" + strings.Repeat("═", dialogW-2) + "╗")
	borderBot := helpBorder.Render("╚" + strings.Repeat("═", dialogW-2) + "╝")
	side := helpBorder.Render("║")

	helpLines := []string{
		border,
		side + helpTitle.Render(centerText("V/3 User Editor Help", dialogW-2)) + side,
		side + helpBorder.Render(strings.Repeat(" ", dialogW-2)) + side,
		side + helpBorder.Render(centerText("Enter - Edit Highlighted User", dialogW-2)) + side,
		side + helpBorder.Render(centerText("Up/Down/End/Home/PgUp/PgDn - Scroll", dialogW-2)) + side,
		side + helpBorder.Render(centerText("Left/Right Arrow: Scroll User Data", dialogW-2)) + side,
		side + helpBorder.Render(centerText("F3 - Alphabetize / De-Alphabetize", dialogW-2)) + side,
		side + helpBorder.Render(centerText("F2 - Delete Highlighted User", dialogW-2)) + side,
		side + helpBorder.Render(centerText("Shift-F2 - Delete All Tagged Users", dialogW-2)) + side,
		side + helpBorder.Render(centerText("F4 - Purge Deleted User", dialogW-2)) + side,
		side + helpBorder.Render(centerText("Shift-F4 - Purge All Deleted Users", dialogW-2)) + side,
		side + helpBorder.Render(centerText("F5 - Auto Validate Highlighted User", dialogW-2)) + side,
		side + helpBorder.Render(centerText("Shift-F5 - Validate All Tagged Users", dialogW-2)) + side,
		side + helpBorder.Render(centerText("F10 - Tag All  /  Shift-F10 - Untag All", dialogW-2)) + side,
		side + helpBorder.Render(centerText("Space - Toggle Tag on User", dialogW-2)) + side,
		side + helpBorder.Render(centerText("ESC - Exit Program", dialogW-2)) + side,
		side + helpBorder.Render(strings.Repeat(" ", dialogW-2)) + side,
		side + helpTitle.Render(centerText("HIT A KEY.", dialogW-2)) + side,
		borderBot,
	}

	// Overlay dialog on background, preserving content on both sides
	endCol := startCol + dialogW
	for i, hl := range helpLines {
		row := startRow + i
		if row >= 0 && row < len(lines) {
			left := padToCol(lines[row], startCol)
			right := skipToCol(lines[row], endCol)
			lines[row] = left + hl + right
		}
	}

	return strings.Join(lines, "\n")
}

