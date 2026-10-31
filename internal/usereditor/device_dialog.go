package usereditor

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// qwkDevicesField labels the edit-screen action that opens the device dialog.
const qwkDevicesField = "QWK Devices"

// qwkDevicesFieldDef is the field for a user's paired QWK API devices. It is
// added only on a board using the API, since the API is optional.
func qwkDevicesFieldDef() fieldDef {
	return fieldDef{
		Label: qwkDevicesField, Type: ftAction, Col: 3, Row: 16, Width: 22,
		Get: func(u *user.User) string { return fmt.Sprintf("(%d)", len(u.ValidAPIDevices())) },
	}
}

// anyAPIDevices reports whether any user still has valid paired devices, which
// keeps them reachable after the API has been turned off.
func anyAPIDevices(users []*user.User) bool {
	for _, u := range users {
		if len(u.ValidAPIDevices()) > 0 {
			return true
		}
	}
	return false
}

// openDeviceDialog switches to modeDeviceList. It shares the key dialog's
// selection state, since only one of the two is open at a time.
func (m *Model) openDeviceDialog() {
	m.mode = modeDeviceList
	m.keySelected = 0
}

// updateDeviceList handles keys in the device dialog. An unpairing is an edit
// like any other: it reaches users.json when the user is saved, and F10 undoes it.
func (m Model) updateDeviceList(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	devices := m.editingUser().ValidAPIDevices()
	switch msg.Type {
	case tea.KeyUp:
		if m.keySelected > 0 {
			m.keySelected--
		}
	case tea.KeyDown:
		if m.keySelected < len(devices)-1 {
			m.keySelected++
		}
	case tea.KeyEscape:
		m.mode = modeEdit
	default:
		if s := msg.String(); (s == "d" || s == "D") && m.keySelected < len(devices) {
			m.editingUser().DropAPIDevice(devices[m.keySelected].ID)
			m.dirty = true
			m.editDirty = true
			if m.keySelected >= len(devices)-1 && m.keySelected > 0 {
				m.keySelected--
			}
		}
	}
	return m, nil
}

// overlayDeviceDialog draws the device dialog over background.
func (m Model) overlayDeviceDialog(background string) string {
	u := m.editingUser()
	devices := u.ValidAPIDevices()
	const dialogW = 72
	inner := dialogW - 2
	side := dialogBorderStyle.Render("║")
	centered := func(text string) string {
		text = truncateRunes(text, inner)
		pad := (inner - len([]rune(text))) / 2
		return strings.Repeat(" ", pad) + padRight(text, inner-pad)
	}

	rows := []string{
		dialogBorderStyle.Render("╔" + strings.Repeat("═", inner) + "╗"),
		side + dialogTitleStyle.Render(centered(fmt.Sprintf("QWK Devices for %s  (%d paired)", u.Handle, len(devices)))) + side,
		side + dialogTextStyle.Render(strings.Repeat(" ", inner)) + side,
	}
	if len(devices) == 0 {
		rows = append(rows, side+dialogTextStyle.Render(centered("(no devices paired)"))+side)
	}
	nameW := inner - 26
	for i, d := range devices {
		last := "never"
		if !d.LastUsed.IsZero() {
			last = d.LastUsed.Local().Format("2006-01-02")
		}
		row := padRight(truncateRunes(d.Name, nameW), nameW) + "  " + d.CreatedAt.Local().Format("2006-01-02") + "  " + padRight(last, 12)
		style := dialogTextStyle
		if i == m.keySelected {
			style = buttonActiveStyle
		}
		rows = append(rows, side+style.Render(row)+side)
	}
	rows = append(rows,
		side+dialogTextStyle.Render(strings.Repeat(" ", inner))+side,
		side+dialogTextStyle.Render(centered("[D] Unpair selected  [Esc] back"))+side,
		dialogBorderStyle.Render("╚"+strings.Repeat("═", inner)+"╝"),
	)

	lines := strings.Split(background, "\n")
	startRow := max((m.height-len(rows))/2, 0)
	startCol := max((m.width-dialogW)/2, 0)
	for i, dl := range rows {
		if row := startRow + i; row < len(lines) {
			lines[row] = padToCol(lines[row], startCol) + dl + skipToCol(lines[row], startCol+dialogW)
		}
	}
	return strings.Join(lines, "\n")
}
