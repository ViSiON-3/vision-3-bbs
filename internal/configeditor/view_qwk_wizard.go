package configeditor

import "fmt"

// viewQWKWizardForm renders the QWK network wizard form.
func (m Model) viewQWKWizardForm() string {
	title := "QWK Network Wizard"
	if m.qwkWizard != nil && m.qwkWizard.editing() {
		title = "QWK Network Wizard — " + m.qwkWizard.editingKey
	}
	return m.viewFieldWizardForm(m.qwkWizardFields, title, modeQWKWizardForm, modeQWKWizardField)
}

// viewQWKWizardPicker renders the wizard's opening choice: add a network or
// edit one already configured.
func (m Model) viewQWKWizardPicker() string {
	boxW := 70
	total := len(m.qwkWizardPickerKeys) + 1
	lb := m.newListBox(boxW, qwkListVisible+13)

	lb.topBorder()
	lb.title("QWK Network Wizard")
	lb.colHeader(fmt.Sprintf("  %-14s  %-8s  %-28s  %s", "Network", "Hub", "Host", "Areas"))
	lb.separator()

	lb.list(qwkListVisible, m.qwkWizardPickerScroll, m.qwkWizardPickerCursor, total, func(i int) string {
		if i == 0 {
			return "+ Add a new network..."
		}
		key := m.qwkWizardPickerKeys[i-1]
		net := m.configs.QWKNet.Networks[key]
		return fmt.Sprintf("  %-14s  %-8s  %-28s  %d", padRight(key, 14), padRight(net.HubID, 8),
			padRight(truncateToDisplayWidth(net.HostPort(), 28), 28), len(m.qwkConferenceNumbersFor(key)))
	})

	lb.separator()
	if m.qwkWizardPickerCursor == 0 {
		lb.row(editInfoValueStyle.Render(padRight("  Join a QWK network this system is not a node of yet.", boxW)))
		lb.emptyRows(3)
	} else {
		key := m.qwkWizardPickerKeys[m.qwkWizardPickerCursor-1]
		net := m.configs.QWKNet.Networks[key]
		name := net.Name
		if name == "" {
			name = key
		}
		lb.row(editInfoValueStyle.Render(padRight(fmt.Sprintf("  %s — hub %s at %s", name, net.HubID, net.HostPort()), boxW)))
		lb.row(editInfoValueStyle.Render(padRight(fmt.Sprintf("  Conferences carried: %d", len(m.qwkConferenceNumbersFor(key))), boxW)))
		enabled := "off"
		if net.Enabled {
			enabled = "on"
		}
		lb.row(editInfoValueStyle.Render(padRight(fmt.Sprintf("  Polling: %s   Login: %s", enabled, net.LoginUser(m.systemQWKID())), boxW)))
		lb.row(editInfoValueStyle.Render(padRight("  Enter re-opens the wizard to change the hub or add conferences.", boxW)))
	}

	lb.bottomBorder()
	lb.bgRows(lb.bottomPad + 1)
	return lb.finish("Enter - Select  |  N - Add New  |  ESC - Back")
}

// viewQWKNetworkBrowser renders the known-network list with an info panel.
func (m Model) viewQWKNetworkBrowser() string {
	boxW := 70
	total := len(m.qwkNetBrowserNets)
	lb := m.newListBox(boxW, qwkListVisible+13)

	lb.topBorder()
	lb.title("Known QWK Networks")
	lb.colHeader(fmt.Sprintf("  %-12s  %-8s  %s", "Network", "Hub", "Description"))
	lb.separator()

	lb.list(qwkListVisible, m.qwkNetBrowserScrl, m.qwkNetBrowserCur, total, func(i int) string {
		net := m.qwkNetBrowserNets[i]
		marker := " "
		if _, exists := m.configs.QWKNet.Networks[net.Key]; exists {
			marker = "*"
		}
		descW := boxW - 2 - 12 - 2 - 8 - 2 - 2
		desc := net.Description
		if truncateToDisplayWidth(desc, descW) != desc {
			desc = truncateToDisplayWidth(desc, descW-3) + "..."
		}
		return fmt.Sprintf("%s %-12s  %-8s  %s", marker, padRight(net.Name, 12), padRight(net.HubID, 8), desc)
	})

	lb.separator()
	if total > 0 && m.qwkNetBrowserCur < total {
		net := m.qwkNetBrowserNets[m.qwkNetBrowserCur]
		lb.row(editInfoValueStyle.Render(padRight(fmt.Sprintf("  Hub: %s at %s:%d, %d conferences", net.HubID, net.Host, net.Port, len(net.Conferences)), boxW)))
		lb.row(editInfoValueStyle.Render(padRight("  Info: "+net.InfoURL, boxW)))
		notes := wrapToWidth(net.JoinNotes, boxW-4)
		for i := 0; i < 2; i++ {
			line := ""
			if i < len(notes) {
				line = "  " + notes[i]
			}
			lb.row(editInfoValueStyle.Render(padRight(line, boxW)))
		}
	} else {
		lb.emptyRows(4)
	}

	lb.bottomBorder()
	lb.bgRows(lb.bottomPad + 1)
	return lb.finish("Enter - Select  |  C - Custom Hub  |  ESC - Back")
}

// viewQWKConfBrowser renders the conference picker, or the progress and
// error states around fetching the list.
func (m Model) viewQWKConfBrowser() string {
	boxW := 70
	w := m.qwkWizard
	total := 0
	if w != nil {
		total = len(w.available)
	}
	lb := m.newListBox(boxW, qwkListVisible+9)

	lb.topBorder()
	name := ""
	if w != nil {
		name = w.networkName
	}
	lb.title(fmt.Sprintf("Hub Conferences — %s", name))

	if m.mode == modeQWKConfFetching {
		return lb.statusScreen(menuItemStyle.Render(centerText("Connecting to the hub for its conference list...", boxW)),
			qwkListVisible, 2, "ESC - Cancel")
	}
	if m.qwkConfBrowserErr != "" && total == 0 {
		lb.row(lb.errorRow(m.qwkConfBrowserErr))
		lb.row(menuItemStyle.Render(padRight("  ESC keeps what you entered — you can save the network now and", boxW)))
		lb.row(menuItemStyle.Render(padRight("  add conferences later under Message Areas.", boxW)))
		lb.emptyRows(qwkListVisible - 1)
		lb.bottomBorder()
		lb.bgRows(lb.bottomPad + 2)
		return lb.finish("R - Retry  |  ESC - Back")
	}

	lb.colHeader(fmt.Sprintf("   %-4s %6s  %s", " ", "Conf", "Name"))
	lb.separator()
	lb.list(qwkListVisible, m.qwkConfBrowserScrl, m.qwkConfBrowserCur, total, func(i int) string {
		c := w.available[i]
		check := "[ ]"
		if i < len(m.qwkConfBrowserSel) && m.qwkConfBrowserSel[i] {
			check = "[x]"
		}
		suffix := ""
		if w.existingConfs[c.Number] {
			suffix = "  (configured)"
		}
		nameW := boxW - 3 - 4 - 8 - len(suffix)
		n := c.Name
		if truncateToDisplayWidth(n, nameW) != n {
			n = truncateToDisplayWidth(n, nameW-3) + "..."
		}
		return fmt.Sprintf("   %s %6d  %s%s", check, c.Number, n, suffix)
	})

	lb.bottomBorder()
	lb.bgRows(lb.bottomPad)
	selected := 0
	for _, s := range m.qwkConfBrowserSel {
		if s {
			selected++
		}
	}
	source := "preset list"
	if w.confsFromHub || w.known == nil {
		source = "from the hub"
	}
	lb.line(lb.pad(editInfoValueStyle.Render(centerText(fmt.Sprintf("%d of %d conferences selected (%s)", selected, total, source), boxW+2))))
	if m.qwkConfBrowserErr != "" {
		msg := "Hub refresh failed: " + m.qwkConfBrowserErr
		if len([]rune(msg)) > boxW {
			msg = string([]rune(msg)[:boxW-3]) + "..."
		}
		lb.messageRow(msg)
	} else {
		lb.bgRows(1)
	}
	return lb.finish("Space - Toggle  |  A - All  |  N - None  |  F - Refresh from Hub  |  Enter - Confirm  |  ESC - Back")
}

// wrapToWidth breaks text into lines no wider than w on word boundaries.
func wrapToWidth(text string, w int) []string {
	var lines []string
	line := ""
	for _, word := range splitWords(text) {
		if line == "" {
			line = word
			continue
		}
		if len(line)+1+len(word) > w {
			lines = append(lines, line)
			line = word
			continue
		}
		line += " " + word
	}
	if line != "" {
		lines = append(lines, line)
	}
	return lines
}

func splitWords(s string) []string {
	var out []string
	cur := ""
	for _, r := range s {
		if r == ' ' || r == '\n' || r == '\t' {
			if cur != "" {
				out = append(out, cur)
				cur = ""
			}
			continue
		}
		cur += string(r)
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}
