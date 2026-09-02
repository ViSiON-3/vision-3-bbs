package configeditor

import (
	"fmt"
	"strings"
)

// viewFTNWizardPicker renders the wizard's opening choice: add another network,
// or edit one that is already configured. It exists because the wizard used to
// open on a blank form no matter what was already set up, which left sysops
// unable to see what they had configured (issue #176).
func (m Model) viewFTNWizardPicker() string {
	boxW := 70
	total := len(m.ftnWizardPickerKeys) + 1

	lb := m.newListBox(boxW, ftnWizardPickerVisible+13)

	lb.topBorder()
	lb.title("FTN Setup Wizard")
	lb.colHeader(fmt.Sprintf("  %-14s  %-16s  %s", "Network", "Your Address", "Hub"))
	lb.separator()

	lb.list(ftnWizardPickerVisible, m.ftnWizardPickerScroll, m.ftnWizardPickerCursor, total,
		func(i int) string {
			if i == 0 {
				return "+ Add a new network..."
			}
			key := m.ftnWizardPickerKeys[i-1]
			net := m.configs.FTN.Networks[key]

			hub := "(no link configured)"
			if len(net.Links) > 0 {
				hub = net.Links[0].Hostname
				if net.Links[0].Port > 0 {
					hub = fmt.Sprintf("%s:%d", hub, net.Links[0].Port)
				}
			}

			hubW := boxW - 2 - 14 - 2 - 16 - 2
			if truncateToDisplayWidth(hub, hubW) != hub {
				hub = truncateToDisplayWidth(hub, hubW-3) + "..."
			}
			return fmt.Sprintf("  %-14s  %-16s  %s", padRight(key, 14), padRight(net.OwnAddress, 16), hub)
		})

	lb.separator()

	// Detail panel for the highlighted entry.
	if m.ftnWizardPickerCursor == 0 {
		lb.row(editInfoValueStyle.Render(padRight("  Set up a network this system does not carry yet.", boxW)))
		lb.emptyRows(3)
	} else {
		key := m.ftnWizardPickerKeys[m.ftnWizardPickerCursor-1]
		net := m.configs.FTN.Networks[key]

		lb.row(editInfoValueStyle.Render(padRight(fmt.Sprintf("  %s — address %s", key, net.OwnAddress), boxW)))

		if len(net.Links) > 0 {
			link := net.Links[0]
			lb.row(editInfoValueStyle.Render(padRight(
				fmt.Sprintf("  Hub: %s at %s:%d", link.Address, link.Hostname, link.Port), boxW)))
		} else {
			lb.row(editInfoValueStyle.Render(padRight("  Hub: none configured", boxW)))
		}

		echos, netmail := m.ftnAreaCounts(key)
		areas := fmt.Sprintf("  Areas: %d echo", echos)
		if echos != 1 {
			areas += "s"
		}
		if netmail > 0 {
			areas += " + netmail"
		}
		lb.row(editInfoValueStyle.Render(padRight(areas, boxW)))

		tosser := "off"
		if net.InternalTosserEnabled {
			tosser = "on"
		}
		lb.row(editInfoValueStyle.Render(padRight(
			fmt.Sprintf("  Tosser: %s   Poll: %ds", tosser, net.PollSeconds), boxW)))
	}

	lb.bottomBorder()
	lb.bgRows(lb.bottomPad + 1)

	return lb.finish("Enter - Select  |  N - Add New  |  ESC - Back")
}

// ftnAreaCounts returns how many echomail and netmail areas are configured for
// a network, for the picker's detail panel.
func (m Model) ftnAreaCounts(netKey string) (echos, netmail int) {
	for _, area := range m.configs.MsgAreas {
		if !strings.EqualFold(area.Network, netKey) {
			continue
		}
		switch strings.ToLower(area.AreaType) {
		case "netmail":
			netmail++
		default:
			echos++
		}
	}
	return echos, netmail
}
