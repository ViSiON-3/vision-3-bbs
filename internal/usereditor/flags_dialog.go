package usereditor

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/mattn/go-runewidth"

	"github.com/ViSiON-3/vision-3-bbs/internal/perm"
	"github.com/ViSiON-3/vision-3-bbs/internal/uitext"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// namedFlagsField labels the edit-screen action that opens the named flags
// dialog (#1074), on the right-column row #1043 left free.
const namedFlagsField = "Named Flags"

// namedFlagsFieldDef is the edit-screen action for the user's named flags.
func namedFlagsFieldDef() fieldDef {
	return fieldDef{
		Label: namedFlagsField, Type: ftAction, Col: rightCol, Row: 14, Width: 18,
		Get: func(u *user.User) string { return namedFlagsSummary(u, time.Now()) },
	}
}

// namedFlagsSummary is the field's value: how many named flags the user
// has and how many of them have expired, "(none)" for none.
func namedFlagsSummary(u *user.User, now time.Time) string {
	grants := perm.FlagGrants(u)
	if len(grants) == 0 {
		return "(none)"
	}
	expired := 0
	for _, g := range grants {
		if !g.Live(now) {
			expired++
		}
	}
	s := fmt.Sprintf("%d flags", len(grants))
	if len(grants) == 1 {
		s = "1 flag"
	}
	if expired > 0 {
		s += fmt.Sprintf(", %d expired", expired)
	}
	return s
}

// flagsDialogShown is how many of the user's grants the dialog lists; the
// rest are counted.
const flagsDialogShown = 5

// flagsDialog is the named flags dialog's state.
type flagsDialog struct {
	input textinput.Model
	err   string
	// now is when the dialog opened: the grants it lists as live or
	// expired, and the moment a new expiry must come after.
	now time.Time
}

// openFlagsDialog switches to modeNamedFlags with the user's flags.
func (m *Model) openFlagsDialog() {
	d := &m.flags
	d.now = time.Now()
	v := perm.FormatFlagList(m.editingUser(), m.boardLocation)
	ti := textinput.New()
	ti.Prompt = ""
	// As in the roles and groups dialog: never below what is stored.
	ti.CharLimit = max(membershipCharLimit, len([]rune(v)))
	ti.Width = 58
	ti.SetValue(v)
	ti.CursorEnd()
	ti.Focus()
	d.input = ti
	d.err = ""
	m.mode = modeNamedFlags
}

// updateFlags handles keys in the dialog. Enter checks the list against
// configs/roles.json and the expiries against the clock and, when they are
// good, stores the flags on the user. Leaving a flag out revokes it. Esc
// leaves the user as they were.
func (m Model) updateFlags(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	d := &m.flags
	switch msg.Type {
	case tea.KeyEscape:
		m.mode = modeEdit
		return m, nil
	case tea.KeyEnter:
		if m.rolesErr != nil {
			d.err = fmt.Sprintf("configs/roles.json can't be read: %v", m.rolesErr)
			return m, nil
		}
		u := m.editingUser()
		flags, err := perm.ParseFlagList(d.input.Value(), m.roles, m.boardLocation, time.Now(), u)
		if err != nil {
			d.err = err.Error()
			return m, nil
		}
		if !perm.SameFlags(flags, u.NamedFlags) {
			u.NamedFlags = flags
			m.dirty, m.editDirty = true, true
		}
		m.mode = modeEdit
		return m, nil
	}
	var cmd tea.Cmd
	d.input, cmd = d.input.Update(msg)
	d.err = ""
	return m, cmd
}

// overlayFlagsDialog draws the dialog over background.
func (m Model) overlayFlagsDialog(background string) string {
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
	head := " Flags   : "
	view := m.flags.input.View()
	pad := max(0, inner-runewidth.StringWidth(head)-uitext.ApproximateVisibleLen(view))
	input := side + dialogTextStyle.Render(head) + view + dialogTextStyle.Render(strings.Repeat(" ", pad)) + side

	u := m.editingUser()
	zone := "the server's zone"
	if m.boardLocation != nil && m.boardLocation != time.Local {
		zone = m.boardLocation.String()
	}
	rows := []string{
		dialogBorderStyle.Render("╔" + strings.Repeat("═", inner) + "╗"),
		centered("Named Flags for " + u.Handle),
		line(""),
	}
	grants := perm.FlagGrants(u)
	if len(grants) == 0 {
		rows = append(rows, line("Has: none"))
	}
	for i, g := range grants {
		if i == flagsDialogShown {
			rows = append(rows, line(fmt.Sprintf("     and %d more", len(grants)-i)))
			break
		}
		label := "     "
		if i == 0 {
			label = "Has: "
		}
		rows = append(rows, line(label+perm.DescribeGrant(g, m.boardLocation, m.flags.now)))
	}
	declared := "none"
	if names := m.roles.FlagNames(); len(names) > 0 {
		declared = strings.Join(names, ", ")
	}
	rows = append(rows,
		line(""),
		input,
		line("Each: name, or name=YYYY-MM-DD or name=YYYY-MM-DD HH:MM to expire"),
		line("Times: "+zone+"; a date alone is midnight at its start"),
		line("Declared: "+declared),
		line(m.flags.err),
		centered("Leave a flag out to revoke it  [Enter] OK  [Esc] cancel"),
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
