package usereditor

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/mattn/go-runewidth"

	"github.com/ViSiON-3/vision-3-bbs/internal/perm"
	"github.com/ViSiON-3/vision-3-bbs/internal/uitext"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// membershipField labels the edit-screen action that opens the roles and
// groups dialog. Both lists are edited there, in one field's row, so the edit
// screen keeps a row free for the next per-user setting.
const membershipField = "Roles/Groups"

// membershipFieldDef is the edit-screen action for the user's admin roles
// (#610) and user groups (#1036).
func membershipFieldDef() fieldDef {
	return fieldDef{
		Label: membershipField, Type: ftAction, Col: rightCol, Row: 13, Width: 18,
		Get: membershipSummary,
	}
}

// membershipSummary is the field's value: how many roles and groups the user
// has, "(none)" for neither.
func membershipSummary(u *user.User) string {
	r, g := len(perm.NormalizeRoles(u.Roles)), len(perm.NormalizeRoles(u.Groups))
	if r == 0 && g == 0 {
		return "(none)"
	}
	plural := func(n int, one string) string {
		if n == 1 {
			return "1 " + one
		}
		return fmt.Sprintf("%d %ss", n, one)
	}
	return plural(r, "role") + ", " + plural(g, "group")
}

// membershipCharLimit is the longest list that can be typed into the
// dialog. A longer list already on the record is kept whole.
const membershipCharLimit = 200

// The dialog's two inputs.
const (
	memberRoles = iota
	memberGroups
)

// membershipDialog is the roles and groups dialog's state.
type membershipDialog struct {
	inputs [2]textinput.Model
	focus  int
	err    string
}

// openMembershipDialog switches to modeMembership with the user's lists.
func (m *Model) openMembershipDialog() {
	u := m.editingUser()
	d := &m.membership
	for i, v := range []string{perm.FormatRoleList(u.Roles), perm.FormatRoleList(u.Groups)} {
		ti := textinput.New()
		ti.Prompt = ""
		// Set before the value, which textinput cuts to the limit, and
		// never below the list already stored, so opening the dialog and
		// pressing Enter can't shorten it.
		ti.CharLimit = max(membershipCharLimit, len([]rune(v)))
		ti.Width = 46
		ti.SetValue(v)
		ti.CursorEnd()
		d.inputs[i] = ti
	}
	d.err = ""
	d.setFocus(memberRoles)
	m.mode = modeMembership
}

func (d *membershipDialog) setFocus(f int) {
	d.focus = f
	for i := range d.inputs {
		if i == f {
			d.inputs[i].Focus()
		} else {
			d.inputs[i].Blur()
		}
	}
}

// updateMembership handles keys in the dialog. Enter checks both lists
// against configs/roles.json and, when both are good, stores them on the
// user; a name the file doesn't define is refused with the list of the ones
// it does. Esc leaves the user as they were.
func (m Model) updateMembership(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	d := &m.membership
	switch msg.Type {
	case tea.KeyTab, tea.KeyDown, tea.KeyShiftTab, tea.KeyUp:
		d.setFocus(1 - d.focus)
		return m, textinput.Blink
	case tea.KeyEscape:
		m.mode = modeEdit
		return m, nil
	case tea.KeyEnter:
		if m.rolesErr != nil {
			d.err = fmt.Sprintf("configs/roles.json can't be read: %v", m.rolesErr)
			return m, nil
		}
		roles, err := perm.ParseRoleList(d.inputs[memberRoles].Value(), m.roles)
		if err != nil {
			d.err = "Roles: " + err.Error()
			d.setFocus(memberRoles)
			return m, nil
		}
		groups, err := perm.ParseGroupList(d.inputs[memberGroups].Value(), m.roles)
		if err != nil {
			d.err = "Groups: " + err.Error()
			d.setFocus(memberGroups)
			return m, nil
		}
		u := m.editingUser()
		if perm.FormatRoleList(roles) != perm.FormatRoleList(u.Roles) || perm.FormatRoleList(groups) != perm.FormatRoleList(u.Groups) {
			u.Roles, u.Groups = roles, groups
			m.dirty, m.editDirty = true, true
		}
		m.mode = modeEdit
		return m, nil
	}
	var cmd tea.Cmd
	d.inputs[d.focus], cmd = d.inputs[d.focus].Update(msg)
	d.err = ""
	return m, cmd
}

// overlayMembershipDialog draws the dialog over background.
func (m Model) overlayMembershipDialog(background string) string {
	const dialogW = 72
	inner := dialogW - 2
	side := dialogBorderStyle.Render("║")
	line := func(text string) string {
		return side + dialogTextStyle.Render(fitCells(" "+text, inner)) + side
	}
	centered := func(text string) string {
		text = runewidth.Truncate(text, inner, "")
		pad := (inner - runewidth.StringWidth(text)) / 2
		return side + dialogTitleStyle.Render(strings.Repeat(" ", pad)+fitCells(text, inner-pad)) + side
	}
	input := func(label string, i int) string {
		head := fmt.Sprintf(" %-8s: ", label)
		view := m.membership.inputs[i].View()
		pad := max(0, inner-runewidth.StringWidth(head)-uitext.ApproximateVisibleLen(view))
		return side + dialogTextStyle.Render(head) + view + dialogTextStyle.Render(strings.Repeat(" ", pad)) + side
	}
	defined := func(label string, names []string) string {
		if len(names) == 0 {
			return line(label + " defined: none")
		}
		return line(label + " defined: " + strings.Join(names, ", "))
	}

	rows := []string{
		dialogBorderStyle.Render("╔" + strings.Repeat("═", inner) + "╗"),
		centered("Roles & Groups for " + m.editingUser().Handle),
		line(""),
		input("Roles", memberRoles),
		input("Groups", memberGroups),
		line(""),
		defined("Roles", m.roles.Names()),
		defined("Groups", m.roles.GroupNames()),
		line(m.membership.err),
		centered("Names separated by commas  [Tab] switch  [Enter] OK  [Esc] cancel"),
		dialogBorderStyle.Render("╚" + strings.Repeat("═", inner) + "╝"),
	}

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
