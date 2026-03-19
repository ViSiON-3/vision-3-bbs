package configeditor

import (
	"fmt"
	"strings"
)

func (m Model) viewSeedInterstitial() string {
	var b strings.Builder
	b.WriteString(m.globalHeaderLine())
	b.WriteByte('\n')

	bgLine := bgFillStyle.Render(strings.Repeat("░", m.width))
	boxW := 60
	title := "V3Net Node Identity Created"
	var contentLines []string

	contentLines = append(contentLines,
		fmt.Sprintf("  Node ID: %s", m.seedInterstitialNodeID),
		"",
		"  Your recovery seed phrase:",
		"",
	)

	words := strings.Split(m.seedInterstitialPhrase, " ")
	if len(words) == 24 {
		for row := 0; row < 6; row++ {
			contentLines = append(contentLines, fmt.Sprintf(
				"  %2d. %-12s %2d. %-12s %2d. %-12s %2d. %-12s",
				row+1, words[row], row+7, words[row+6],
				row+13, words[row+12], row+19, words[row+18],
			))
		}
	}

	contentLines = append(contentLines,
		"",
		"  Write down these 24 words and store them safely.",
		"  This phrase can restore your node identity if your",
		"  key file is ever lost.",
	)

	helpText := "[E] Export to file   [C] Continue"

	contentRows := len(contentLines)
	extraV := maxInt(0, m.height-contentRows-10)
	topPad := extraV / 2
	bottomPad := extraV - topPad

	for i := 0; i < topPad; i++ {
		b.WriteString(bgLine)
		b.WriteByte('\n')
	}

	padL := maxInt(0, (m.width-boxW-2)/2)
	padR := maxInt(0, m.width-padL-boxW-2)

	b.WriteString(bgFillStyle.Render(strings.Repeat("░", padL)) +
		editBorderStyle.Render("┌"+strings.Repeat("─", boxW)+"┐") +
		bgFillStyle.Render(strings.Repeat("░", maxInt(0, padR))))
	b.WriteByte('\n')

	titleLine := editBorderStyle.Render("│") +
		menuHeaderStyle.Render(centerText(title, boxW)) +
		editBorderStyle.Render("│")
	b.WriteString(bgFillStyle.Render(strings.Repeat("░", padL)) + titleLine +
		bgFillStyle.Render(strings.Repeat("░", maxInt(0, padR))))
	b.WriteByte('\n')

	emptyLine := bgFillStyle.Render(strings.Repeat("░", padL)) +
		editBorderStyle.Render("│") +
		fieldDisplayStyle.Render(strings.Repeat(" ", boxW)) +
		editBorderStyle.Render("│") +
		bgFillStyle.Render(strings.Repeat("░", maxInt(0, padR)))
	b.WriteString(emptyLine)
	b.WriteByte('\n')

	for _, line := range contentLines {
		padded := line
		if len(padded) < boxW {
			padded += strings.Repeat(" ", boxW-len(padded))
		}
		row := bgFillStyle.Render(strings.Repeat("░", padL)) +
			editBorderStyle.Render("│") +
			fieldDisplayStyle.Render(padded) +
			editBorderStyle.Render("│") +
			bgFillStyle.Render(strings.Repeat("░", maxInt(0, padR)))
		b.WriteString(row)
		b.WriteByte('\n')
	}

	b.WriteString(emptyLine)
	b.WriteByte('\n')
	b.WriteString(bgFillStyle.Render(strings.Repeat("░", padL)) +
		editBorderStyle.Render("└"+strings.Repeat("─", boxW)+"┘") +
		bgFillStyle.Render(strings.Repeat("░", maxInt(0, padR))))
	b.WriteByte('\n')

	for i := 0; i < bottomPad; i++ {
		b.WriteString(bgLine)
		b.WriteByte('\n')
	}

	if m.message != "" {
		b.WriteString(bgFillStyle.Render(centerText(m.message, m.width)))
	} else {
		b.WriteString(bgLine)
	}
	b.WriteByte('\n')

	b.WriteString(bgLine)
	b.WriteByte('\n')
	b.WriteString(helpBarStyle.Render(centerText(helpText, m.width)))

	return b.String()
}

// viewV3NetIdentity renders the Node Identity screen and all sub-states.
func (m Model) viewV3NetIdentity() string {
	var b strings.Builder
	b.WriteString(m.globalHeaderLine())
	b.WriteByte('\n')

	bgLine := bgFillStyle.Render(strings.Repeat("░", m.width))
	boxW := 60

	// Build content lines based on sub-state.
	var title string
	var contentLines []string
	var helpText string

	switch m.identitySubState {
	case identityShowPhrase:
		title = "Recovery Seed Phrase"
		words := strings.Split(m.identityPhrase, " ")
		if len(words) == 24 {
			for row := 0; row < 6; row++ {
				contentLines = append(contentLines, fmt.Sprintf(
					"  %2d. %-12s %2d. %-12s %2d. %-12s %2d. %-12s",
					row+1, words[row], row+7, words[row+6],
					row+13, words[row+12], row+19, words[row+18],
				))
			}
		}
		contentLines = append(contentLines, "")
		contentLines = append(contentLines, "  Press any key to return")
		helpText = "Any key - Return"

	case identityExportPrompt:
		title = "Export Recovery Phrase"
		contentLines = []string{
			"  Export to file: " + m.textInput.View(),
		}
		helpText = "Enter - Save  |  ESC - Cancel"

	case identityRecoverInput:
		title = "Recover Identity"
		contentLines = []string{
			"  Enter your 24-word recovery phrase:",
			"",
			"  " + m.textInput.View(),
		}
		helpText = "Enter - Submit  |  ESC - Cancel"

	case identityRecoverConfirm:
		title = "Confirm Recovery"
		contentLines = []string{
			fmt.Sprintf("  Node ID will become: %s", m.identityRecoverNodeID),
			"",
			"  This will replace your current key file.",
			"  Continue? [Y/N]",
		}
		helpText = "Y - Confirm  |  N - Cancel"

	default: // identityMain
		title = "V3Net Node Identity"
		ks, err := m.loadIdentityKeystore()
		if err != nil {
			contentLines = []string{
				fmt.Sprintf("  Error: %v", err),
			}
		} else if ks == nil {
			contentLines = []string{
				"  No V3Net identity configured.",
				"  Set up a leaf subscription or hub network to generate one.",
			}
		} else {
			path := m.configs.V3Net.KeystorePath
			if path == "" {
				path = "data/v3net.key"
			}
			contentLines = []string{
				fmt.Sprintf("  Node ID:    %s", ks.NodeID()),
				fmt.Sprintf("  Public Key: %s", ks.PubKeyBase64()),
				fmt.Sprintf("  Key File:   %s", path),
				"",
				"  [S] Show recovery seed phrase",
				"  [E] Export recovery seed phrase to file",
				"  [R] Recover identity from seed phrase",
			}
		}
		helpText = "S - Show  |  E - Export  |  R - Recover  |  Q - Return"
	}

	// Render the box.
	contentRows := len(contentLines)
	extraV := maxInt(0, m.height-contentRows-10)
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
		editBorderStyle.Render("┌"+strings.Repeat("─", boxW)+"┐") +
		bgFillStyle.Render(strings.Repeat("░", maxInt(0, padR))))
	b.WriteByte('\n')

	// Title
	titleLine := editBorderStyle.Render("│") +
		menuHeaderStyle.Render(centerText(title, boxW)) +
		editBorderStyle.Render("│")
	b.WriteString(bgFillStyle.Render(strings.Repeat("░", padL)) + titleLine +
		bgFillStyle.Render(strings.Repeat("░", maxInt(0, padR))))
	b.WriteByte('\n')

	// Empty line
	emptyLine := bgFillStyle.Render(strings.Repeat("░", padL)) +
		editBorderStyle.Render("│") +
		fieldDisplayStyle.Render(strings.Repeat(" ", boxW)) +
		editBorderStyle.Render("│") +
		bgFillStyle.Render(strings.Repeat("░", maxInt(0, padR)))
	b.WriteString(emptyLine)
	b.WriteByte('\n')

	// Content lines
	for _, line := range contentLines {
		padded := line
		if len(padded) < boxW {
			padded += strings.Repeat(" ", boxW-len(padded))
		}
		row := bgFillStyle.Render(strings.Repeat("░", padL)) +
			editBorderStyle.Render("│") +
			fieldDisplayStyle.Render(padded) +
			editBorderStyle.Render("│") +
			bgFillStyle.Render(strings.Repeat("░", maxInt(0, padR)))
		b.WriteString(row)
		b.WriteByte('\n')
	}

	// Empty line + bottom border
	b.WriteString(emptyLine)
	b.WriteByte('\n')
	b.WriteString(bgFillStyle.Render(strings.Repeat("░", padL)) +
		editBorderStyle.Render("└"+strings.Repeat("─", boxW)+"┘") +
		bgFillStyle.Render(strings.Repeat("░", maxInt(0, padR))))
	b.WriteByte('\n')

	for i := 0; i < bottomPad; i++ {
		b.WriteString(bgLine)
		b.WriteByte('\n')
	}

	// Message line
	if m.message != "" {
		b.WriteString(bgFillStyle.Render(centerText(m.message, m.width)))
	} else {
		b.WriteString(bgLine)
	}
	b.WriteByte('\n')

	b.WriteString(bgLine)
	b.WriteByte('\n')
	b.WriteString(helpBarStyle.Render(centerText(helpText, m.width)))

	return b.String()
}
