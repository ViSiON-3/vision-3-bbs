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
	boxH := 8
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

	items := []string{
		"  [J]  Join an existing network  (leaf node)",
		"  [H]  Host your own network     (hub operator)",
	}
	for _, item := range items {
		b.WriteString(pad(menuBorderStyle.Render("│") +
			menuItemStyle.Render(padRight(item, boxW)) +
			menuBorderStyle.Render("│")))
		b.WriteByte('\n')
	}

	b.WriteString(pad(menuBorderStyle.Render("│") + menuHeaderStyle.Render(strings.Repeat(" ", boxW)) + menuBorderStyle.Render("│")))
	b.WriteByte('\n')
	b.WriteString(pad(menuBorderStyle.Render("│") +
		editInfoLabelStyle.Render(padRight("  ESC — Back", boxW)) +
		menuBorderStyle.Render("│")))
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
	b.WriteString(helpBarStyle.Render(centerText("J/H Select  |  ESC Back", m.width)))
	return b.String()
}

func (m Model) viewLeafWizardStep() string {
	titles := []string{
		"Step 1 of 5 — Hub URL",
		"Step 2 of 5 — Network Name",
		"Step 3 of 5 — Board Tag",
		"Step 4 of 5 — Poll Interval",
		"Step 5 of 5 — Origin Line",
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

	return m.viewWizardInputBox("Join a Network — "+title, help, notice)
}

func (m Model) viewHubWizardStep() string {
	switch m.wizard.step {
	case hubStepNetwork:
		subField := "Network Name"
		helpText := "Short lowercase alphanumeric identifier (e.g. felonynet)"
		if m.wizard.areaAdding {
			subField = "Description"
			helpText = "Human-readable description shown to subscribers"
		}
		return m.viewWizardInputBox(
			"Host a Network — Step 1 of 4 — "+subField,
			helpText,
			"",
		)
	case hubStepPort:
		return m.viewWizardInputBox(
			"Host a Network — Step 2 of 4 — Listen Port",
			"TCP port for the hub server (default: 8765)",
			"",
		)
	case hubStepAutoApprove:
		current := "N (No)"
		if m.wizard.autoApprove {
			current = "Y (Yes)"
		}
		return m.viewWizardInputBox(
			"Host a Network — Step 3 of 4 — Auto-Approve",
			fmt.Sprintf("Auto-approve new nodes? Currently: %s   Press Y or N, then Enter", current),
			"Yes = nodes join instantly (testing only)  /  No = sysop approves each node",
		)
	case hubStepAreas:
		return m.viewHubAreasStep()
	}
	return ""
}

func (m Model) viewHubAreasStep() string {
	var b strings.Builder
	b.WriteString(m.globalHeaderLine())
	b.WriteByte('\n')

	bgLine := bgFillStyle.Render(strings.Repeat("░", m.width))
	boxW := 60
	listH := maxInt(3, len(m.wizard.areas)+1)
	boxH := listH + 8
	extraV := maxInt(0, m.height-boxH-3)
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
		return border(menuBorderStyle.Render("│") +
			menuItemStyle.Render(padRight(content, boxW)) +
			menuBorderStyle.Render("│"))
	}

	b.WriteString(border(menuBorderStyle.Render("┌" + strings.Repeat("─", boxW) + "┐")))
	b.WriteByte('\n')
	b.WriteString(border(menuBorderStyle.Render("│") +
		menuHeaderStyle.Render(centerText("Host a Network — Step 4 of 4 — Initial Areas", boxW)) +
		menuBorderStyle.Render("│")))
	b.WriteByte('\n')

	if m.wizard.areaAdding {
		if m.wizard.areaEditTag == "" {
			b.WriteString(row("  Tag (e.g. net.general):"))
		} else {
			b.WriteString(row(fmt.Sprintf("  Tag: %s  Name:", m.wizard.areaEditTag)))
		}
		b.WriteByte('\n')
		b.WriteString(row("  > " + m.textInput.View()))
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

	b.WriteString(border(menuBorderStyle.Render("│") + menuHeaderStyle.Render(strings.Repeat(" ", boxW)) + menuBorderStyle.Render("│")))
	b.WriteByte('\n')
	b.WriteString(row("  A Add area  D Delete  Enter Confirm  ESC Back"))
	b.WriteByte('\n')
	b.WriteString(border(menuBorderStyle.Render("└" + strings.Repeat("─", boxW) + "┘")))
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
	b.WriteString(helpBarStyle.Render(centerText("A Add  D Delete  Enter Confirm  ESC Back", m.width)))
	return b.String()
}

// viewWizardInputBox renders a generic single-field wizard step box.
func (m Model) viewWizardInputBox(title, helpText, notice string) string {
	var b strings.Builder
	b.WriteString(m.globalHeaderLine())
	b.WriteByte('\n')

	bgLine := bgFillStyle.Render(strings.Repeat("░", m.width))
	boxW := 60
	boxH := 7
	if notice != "" {
		boxH = 9
	}
	extraV := maxInt(0, m.height-boxH-3)
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
		editInfoLabelStyle.Render(padRight("  "+helpText, boxW)) +
		editBorderStyle.Render("│")))
	b.WriteByte('\n')
	b.WriteString(border(editBorderStyle.Render("│") +
		fieldDisplayStyle.Render(padRight("  > "+m.textInput.View(), boxW)) +
		editBorderStyle.Render("│")))
	b.WriteByte('\n')

	if notice != "" {
		b.WriteString(border(editBorderStyle.Render("│") +
			editInfoLabelStyle.Render(padRight("  "+notice, boxW)) +
			editBorderStyle.Render("│")))
		b.WriteByte('\n')
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

	if m.message != "" {
		b.WriteString(flashMessageStyle.Render(" " + padRight(m.message, m.width-1)))
	} else {
		b.WriteString(bgLine)
	}
	b.WriteByte('\n')
	b.WriteString(helpBarStyle.Render(centerText("Enter Confirm  |  ESC Back", m.width)))
	return b.String()
}
