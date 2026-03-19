package configeditor

import (
	"fmt"
	"strings"
)

// viewV3NetWizard renders the V3Net setup fork screen or active wizard step.
func (m Model) viewV3NetWizard() string {
	if m.mode == modeV3NetSetupFork {
		return m.viewV3NetSetupFork()
	}
	if m.wizard.flow == "leaf" {
		return m.viewLeafWizardStep()
	}
	return m.viewHubWizardStep()
}

func (m Model) viewV3NetSetupFork() string {
	var b strings.Builder
	b.WriteString(m.globalHeaderLine())
	b.WriteByte('\n')

	bgLine := bgFillStyle.Render(strings.Repeat("░", m.width))
	boxW := 52
	boxH := 6
	extraV := maxInt(0, m.height-boxH-3)
	topPad := extraV / 2
	bottomPad := extraV - topPad

	for i := 0; i < topPad; i++ {
		b.WriteString(bgLine)
		b.WriteByte('\n')
	}

	padL := maxInt(0, (m.width-boxW-2)/2)
	padR := maxInt(0, m.width-padL-boxW-2)
	pad := func(s string) string {
		return bgFillStyle.Render(strings.Repeat("░", padL)) + s +
			bgFillStyle.Render(strings.Repeat("░", maxInt(0, padR)))
	}

	b.WriteString(pad(menuBorderStyle.Render("┌" + strings.Repeat("─", boxW) + "┐")))
	b.WriteByte('\n')
	b.WriteString(pad(menuBorderStyle.Render("│") +
		menuHeaderStyle.Render(centerText("V3Net Setup", boxW)) +
		menuBorderStyle.Render("│")))
	b.WriteByte('\n')
	b.WriteString(pad(menuBorderStyle.Render("│") + menuHeaderStyle.Render(strings.Repeat(" ", boxW)) + menuBorderStyle.Render("│")))
	b.WriteByte('\n')

	forkItems := []string{
		"  1.  Join an existing network  (leaf node)  ",
		"  2.  Host your own network     (hub operator)",
	}
	for i, item := range forkItems {
		var styled string
		if i == m.wizard.forkCursor {
			styled = menuHighlightStyle.Render(padRight(item, boxW))
		} else {
			styled = menuItemStyle.Render(padRight(item, boxW))
		}
		b.WriteString(pad(menuBorderStyle.Render("│") + styled + menuBorderStyle.Render("│")))
		b.WriteByte('\n')
	}

	b.WriteString(pad(menuBorderStyle.Render("│") + menuHeaderStyle.Render(strings.Repeat(" ", boxW)) + menuBorderStyle.Render("│")))
	b.WriteByte('\n')
	b.WriteString(pad(menuBorderStyle.Render("└" + strings.Repeat("─", boxW) + "┘")))
	b.WriteByte('\n')

	for i := 0; i < bottomPad; i++ {
		b.WriteString(bgLine)
		b.WriteByte('\n')
	}

	if m.message != "" {
		b.WriteString(flashMessageStyle.Render(" " + padRight(m.message, m.width-1)))
	} else {
		b.WriteString(bgLine)
	}
	b.WriteByte('\n')
	b.WriteString(helpBarStyle.Render(centerText("Up/Down Move  |  J/H Select  |  Enter Confirm  |  ESC Back", m.width)))
	return b.String()
}

func (m Model) viewLeafWizardStep() string {
	titles := []string{
		"Leaf Setup — Step 1 of 5 — Hub URL",
		"Leaf Setup — Step 2 of 5 — Network",
		"Leaf Setup — Step 3 of 5 — Board Tag",
		"Leaf Setup — Step 4 of 5 — Poll Interval",
		"Leaf Setup — Step 5 of 5 — Origin",
	}
	helps := []string{
		"URL of the V3Net hub (e.g. https://hub.felonynet.org)",
		"Network name to subscribe to (e.g. felonynet)",
		"Local message area tag prefix for received messages",
		"How often to poll for new messages (e.g. 5m, 30s, 1h)",
		"Origin line identifying your BBS — leave blank to use BBS name",
	}
	title := "Leaf Setup"
	if m.wizard.step < len(titles) {
		title = titles[m.wizard.step]
	}
	help := ""
	if m.wizard.step < len(helps) {
		help = helps[m.wizard.step]
	}

	notice := ""
	if m.wizard.step == leafStepNetwork && m.wizard.fetchError != "" {
		notice = m.wizard.fetchError
	}

	return m.viewWizardInputBox(title, help, notice)
}

func (m Model) viewHubWizardStep() string {
	switch m.wizard.step {
	case hubStepNetwork:
		title := "Hub Setup — Step 1 of 4 — Network Name"
		helpText := "Short lowercase alphanumeric identifier (e.g. felonynet)"
		if m.wizard.areaAdding {
			title = "Hub Setup — Step 1 of 4 — Description"
			helpText = "Human-readable description shown to subscribers"
		}
		return m.viewWizardInputBox(title, helpText, "")
	case hubStepPort:
		return m.viewWizardInputBox(
			"Hub Setup — Step 2 of 4 — Listen Port",
			"TCP port for the hub server (default: 8765)",
			"",
		)
	case hubStepAutoApprove:
		return m.viewHubAutoApproveStep()
	case hubStepAreas:
		return m.viewHubAreasStep()
	}
	return ""
}

// viewHubAutoApproveStep renders the auto-approve step as a canonical Y/N
// toggle field (fieldLabelStyle label + fieldEditStyle value with ░ fill),
// matching the record editor's ftYesNo field pattern.
func (m Model) viewHubAutoApproveStep() string {
	var b strings.Builder
	b.WriteString(m.globalHeaderLine())
	b.WriteByte('\n')

	bgLine := bgFillStyle.Render(strings.Repeat("░", m.width))
	boxW := 60
	boxH := 6
	// -4: header(1) + help line(1) + bgLine spacer(1) + help bar(1)
	extraV := maxInt(0, m.height-boxH-4)
	topPad := extraV / 2
	bottomPad := extraV - topPad

	for i := 0; i < topPad; i++ {
		b.WriteString(bgLine)
		b.WriteByte('\n')
	}

	padL := maxInt(0, (m.width-boxW-2)/2)
	padR := maxInt(0, m.width-padL-boxW-2)
	border := func(s string) string {
		return bgFillStyle.Render(strings.Repeat("░", padL)) + s +
			bgFillStyle.Render(strings.Repeat("░", maxInt(0, padR)))
	}

	// Field row: label (18 chars) + value (42 chars) = 60 = boxW.
	const labelW = 18
	const valueW = 42 // boxW - labelW
	const editW = 3   // canonical ftYesNo: Width(1) + 2
	labelStr := fieldLabelStyle.Render(padRight("  Auto-Approve  : ", labelW))
	fill := strings.Repeat(string(fieldFillChar), editW-1)
	editStr := fieldEditStyle.Render(boolToYN(m.wizard.autoApprove) + fill)
	rest := fieldDisplayStyle.Render(strings.Repeat(" ", maxInt(0, valueW-editW)))
	valueStr := editStr + rest

	b.WriteString(border(editBorderStyle.Render("┌" + strings.Repeat("─", boxW) + "┐")))
	b.WriteByte('\n')
	b.WriteString(border(editBorderStyle.Render("│") +
		menuHeaderStyle.Render(centerText("Hub Setup — Step 3 of 4 — Auto-Approve", boxW)) +
		editBorderStyle.Render("│")))
	b.WriteByte('\n')
	b.WriteString(border(editBorderStyle.Render("│") +
		editInfoLabelStyle.Render(strings.Repeat(" ", boxW)) +
		editBorderStyle.Render("│")))
	b.WriteByte('\n')
	b.WriteString(border(editBorderStyle.Render("│") +
		labelStr + valueStr +
		editBorderStyle.Render("│")))
	b.WriteByte('\n')
	b.WriteString(border(editBorderStyle.Render("│") +
		editInfoLabelStyle.Render(strings.Repeat(" ", boxW)) +
		editBorderStyle.Render("│")))
	b.WriteByte('\n')
	b.WriteString(border(editBorderStyle.Render("└" + strings.Repeat("─", boxW) + "┘")))
	b.WriteByte('\n')

	for i := 0; i < bottomPad; i++ {
		b.WriteString(bgLine)
		b.WriteByte('\n')
	}

	helpText := "Y = auto-join (testing only) / N = manual approval"
	if m.message != "" {
		b.WriteString(bgFillStyle.Render(strings.Repeat("░", padL)) +
			flashMessageStyle.Render(" "+padRight(m.message, boxW)) +
			bgFillStyle.Render(strings.Repeat("░", maxInt(0, padR+1))))
	} else {
		b.WriteString(bgFillStyle.Render(strings.Repeat("░", padL)) +
			editInfoLabelStyle.Render(centerText(helpText, boxW+1)) +
			bgFillStyle.Render(strings.Repeat("░", maxInt(0, padR+1))))
	}
	b.WriteByte('\n')
	b.WriteString(bgLine)
	b.WriteByte('\n')
	b.WriteString(helpBarStyle.Render(centerText("Space/Y/N Toggle  |  Enter Confirm  |  ESC Back", m.width)))
	return b.String()
}

func (m Model) viewHubAreasStep() string {
	var b strings.Builder
	b.WriteString(m.globalHeaderLine())
	b.WriteByte('\n')

	bgLine := bgFillStyle.Render(strings.Repeat("░", m.width))
	boxW := 60

	// Content rows inside the box (between title and bottom border).
	var contentRows int
	if m.wizard.areaAdding {
		contentRows = 2 // prompt + input
	} else if len(m.wizard.areas) == 0 {
		contentRows = 1 // "(no areas yet …)"
	} else {
		contentRows = len(m.wizard.areas)
	}
	// Box: top border(1) + title(1) + blank(1) + content + blank(1) + bottom border(1)
	boxH := contentRows + 5
	// -4: header(1) + help line(1) + bgLine spacer(1) + help bar(1)
	extraV := maxInt(0, m.height-boxH-4)
	topPad := extraV / 2
	bottomPad := extraV - topPad

	for i := 0; i < topPad; i++ {
		b.WriteString(bgLine)
		b.WriteByte('\n')
	}

	padL := maxInt(0, (m.width-boxW-2)/2)
	padR := maxInt(0, m.width-padL-boxW-2)
	border := func(s string) string {
		return bgFillStyle.Render(strings.Repeat("░", padL)) + s +
			bgFillStyle.Render(strings.Repeat("░", maxInt(0, padR)))
	}
	row := func(content string) string {
		return border(editBorderStyle.Render("│") +
			fieldDisplayStyle.Width(boxW).Render(content) +
			editBorderStyle.Render("│"))
	}

	b.WriteString(border(editBorderStyle.Render("┌" + strings.Repeat("─", boxW) + "┐")))
	b.WriteByte('\n')
	b.WriteString(border(editBorderStyle.Render("│") +
		menuHeaderStyle.Render(centerText("Hub Setup — Step 4 of 4 — Initial Areas", boxW)) +
		editBorderStyle.Render("│")))
	b.WriteByte('\n')
	b.WriteString(border(editBorderStyle.Render("│") +
		editInfoLabelStyle.Render(strings.Repeat(" ", boxW)) +
		editBorderStyle.Render("│")))
	b.WriteByte('\n')

	if m.wizard.areaAdding {
		if m.wizard.areaEditTag == "" {
			b.WriteString(row("  Tag (e.g. net.general):"))
		} else {
			b.WriteString(row(fmt.Sprintf("  Tag: %s  Name:", m.wizard.areaEditTag)))
		}
		b.WriteByte('\n')
		b.WriteString(border(editBorderStyle.Render("│") +
			fieldDisplayStyle.Width(boxW).Render("  > "+m.textInput.View()) +
			editBorderStyle.Render("│")))
		b.WriteByte('\n')
	} else {
		if len(m.wizard.areas) == 0 {
			b.WriteString(row("  (no areas yet — press A to add)"))
			b.WriteByte('\n')
		}
		for i, a := range m.wizard.areas {
			cursor := "  "
			if i == m.wizard.areaCursor {
				cursor = "> "
			}
			b.WriteString(row(fmt.Sprintf("  %s%-20s %s", cursor, a.Tag, a.Name)))
			b.WriteByte('\n')
		}
	}

	b.WriteString(border(editBorderStyle.Render("│") +
		editInfoLabelStyle.Render(strings.Repeat(" ", boxW)) +
		editBorderStyle.Render("│")))
	b.WriteByte('\n')
	b.WriteString(border(editBorderStyle.Render("└" + strings.Repeat("─", boxW) + "┘")))
	b.WriteByte('\n')

	for i := 0; i < bottomPad; i++ {
		b.WriteString(bgLine)
		b.WriteByte('\n')
	}

	helpText := "A Add area  |  D Delete  |  Enter Confirm  |  ESC Back"
	if m.wizard.areaAdding {
		helpText = "Enter Confirm  |  ESC Cancel"
	}
	if m.message != "" {
		b.WriteString(bgFillStyle.Render(strings.Repeat("░", padL)) +
			flashMessageStyle.Render(" "+padRight(m.message, boxW)) +
			bgFillStyle.Render(strings.Repeat("░", maxInt(0, padR+1))))
	} else {
		b.WriteString(bgFillStyle.Render(strings.Repeat("░", padL)) +
			editInfoLabelStyle.Render(centerText(helpText, boxW+1)) +
			bgFillStyle.Render(strings.Repeat("░", maxInt(0, padR+1))))
	}
	b.WriteByte('\n')
	b.WriteString(bgLine)
	b.WriteByte('\n')
	b.WriteString(helpBarStyle.Render(centerText("A Add  D Delete  Enter Confirm  ESC Back", m.width)))
	return b.String()
}

// viewWizardInputBox renders a generic single-field wizard step box.
// Help text and notices appear below the box using the same pattern as
// renderFieldHelpLine: bgFill(padL) + editInfoLabelStyle centered to boxW+1
// + bgFill(padR+1), followed by a bgLine spacer, then the help bar.
func (m Model) viewWizardInputBox(title, helpText, notice string) string {
	var b strings.Builder
	b.WriteString(m.globalHeaderLine())
	b.WriteByte('\n')

	bgLine := bgFillStyle.Render(strings.Repeat("░", m.width))
	boxW := 60
	boxH := 6 // top border + title + blank + input + blank + bottom border
	// -4: header(1) + help line(1) + bgLine spacer(1) + help bar(1)
	extraV := maxInt(0, m.height-boxH-4)
	topPad := extraV / 2
	bottomPad := extraV - topPad

	for i := 0; i < topPad; i++ {
		b.WriteString(bgLine)
		b.WriteByte('\n')
	}

	padL := maxInt(0, (m.width-boxW-2)/2)
	padR := maxInt(0, m.width-padL-boxW-2)
	border := func(s string) string {
		return bgFillStyle.Render(strings.Repeat("░", padL)) + s +
			bgFillStyle.Render(strings.Repeat("░", maxInt(0, padR)))
	}

	b.WriteString(border(editBorderStyle.Render("┌" + strings.Repeat("─", boxW) + "┐")))
	b.WriteByte('\n')
	b.WriteString(border(editBorderStyle.Render("│") +
		menuHeaderStyle.Render(centerText(title, boxW)) +
		editBorderStyle.Render("│")))
	b.WriteByte('\n')
	b.WriteString(border(editBorderStyle.Render("│") +
		editInfoLabelStyle.Render(strings.Repeat(" ", boxW)) +
		editBorderStyle.Render("│")))
	b.WriteByte('\n')
	b.WriteString(border(editBorderStyle.Render("│") +
		fieldDisplayStyle.Width(boxW).Render("  > "+m.textInput.View()) +
		editBorderStyle.Render("│")))
	b.WriteByte('\n')
	b.WriteString(border(editBorderStyle.Render("│") +
		editInfoLabelStyle.Render(strings.Repeat(" ", boxW)) +
		editBorderStyle.Render("│")))
	b.WriteByte('\n')
	b.WriteString(border(editBorderStyle.Render("└" + strings.Repeat("─", boxW) + "┘")))
	b.WriteByte('\n')

	for i := 0; i < bottomPad; i++ {
		b.WriteString(bgLine)
		b.WriteByte('\n')
	}

	// Help line — mirrors renderFieldHelpLine: centered to boxW+1, bgFill on sides.
	// Priority: flash message > notice > help text > blank fill.
	infoText := helpText
	if notice != "" {
		infoText = notice
	}
	if m.message != "" {
		b.WriteString(bgFillStyle.Render(strings.Repeat("░", padL)) +
			flashMessageStyle.Render(" "+padRight(m.message, boxW)) +
			bgFillStyle.Render(strings.Repeat("░", maxInt(0, padR+1))))
	} else if infoText != "" {
		b.WriteString(bgFillStyle.Render(strings.Repeat("░", padL)) +
			editInfoLabelStyle.Render(centerText(infoText, boxW+1)) +
			bgFillStyle.Render(strings.Repeat("░", maxInt(0, padR+1))))
	} else {
		b.WriteString(bgLine)
	}
	b.WriteByte('\n')
	b.WriteString(bgLine)
	b.WriteByte('\n')
	b.WriteString(helpBarStyle.Render(centerText("Enter Confirm  |  ESC Back", m.width)))
	return b.String()
}
