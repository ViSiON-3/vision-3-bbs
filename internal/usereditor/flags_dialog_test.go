package usereditor

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

const flagsRoles = `{"roles": [], "flags": [{"name": "donor"}, {"name": "beta"}]}`

// flagsModel is rolesModel with the named flags dialog open instead.
func flagsModel(t *testing.T, rolesJSON string, u *user.User) Model {
	t.Helper()
	m := rolesModel(t, rolesJSON, u)
	m = press(t, m, key(tea.KeyEscape))
	m.openFlagsDialog()
	return m
}

func typeFlags(t *testing.T, m Model, s string) Model {
	t.Helper()
	m.flags.input.SetValue("")
	return press(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)})
}

func TestFlagsDialogGrantsExpiresAndRevokes(t *testing.T) {
	past := time.Now().Add(-48 * time.Hour).UTC().Format(time.RFC3339)
	u := &user.User{ID: 2, Handle: "Val", AccessLevel: 20, NamedFlags: map[string]string{"beta": past}}
	m := flagsModel(t, flagsRoles, u)
	view := m.View()
	for _, want := range []string{"Named Flags for Val", "Has: beta expired ", "Declared: donor, beta"} {
		if !strings.Contains(view, want) {
			t.Errorf("dialog lacks %q:\n%s", want, view)
		}
	}
	// Opening and pressing Enter keeps the expired grant as stored.
	m = press(t, m, key(tea.KeyEnter))
	if m.mode != modeEdit || m.dirty || m.editingUser().NamedFlags["beta"] != past {
		t.Fatalf("unchanged Enter: mode %v, dirty %v, flags %v", m.mode, m.dirty, m.editingUser().NamedFlags)
	}

	m.openFlagsDialog()
	m = typeFlags(t, m, "nope")
	m = press(t, m, key(tea.KeyEnter))
	if m.mode != modeNamedFlags || !strings.Contains(m.flags.err, "no flag named nope") {
		t.Fatalf("undeclared: mode %v, err %q", m.mode, m.flags.err)
	}
	m = typeFlags(t, m, "donor=2001-01-01")
	m = press(t, m, key(tea.KeyEnter))
	if !strings.Contains(m.flags.err, "already passed") {
		t.Fatalf("past expiry: %q", m.flags.err)
	}

	// Grant donor with an expiry; leaving beta out revokes it.
	next := time.Now().AddDate(1, 0, 0).Format("2006-01-02")
	m = typeFlags(t, m, "donor="+next)
	m = press(t, m, key(tea.KeyEnter))
	got := m.editingUser().NamedFlags
	if m.mode != modeEdit || !m.dirty || len(got) != 1 || got["donor"] == "" {
		t.Fatalf("grant: mode %v, dirty %v, flags %v, err %q", m.mode, m.dirty, got, m.flags.err)
	}
	if _, err := time.Parse(time.RFC3339, got["donor"]); err != nil {
		t.Errorf("stored expiry %q isn't RFC 3339", got["donor"])
	}
	if s := namedFlagsSummary(m.editingUser(), time.Now()); s != "1 flag" {
		t.Errorf("summary = %q", s)
	}
	if s := namedFlagsSummary(u, time.Now()); s != "1 flag, 1 expired" {
		t.Errorf("expired summary = %q", s)
	}

	// Esc leaves the user as they were.
	m.openFlagsDialog()
	m = typeFlags(t, m, "")
	m = press(t, m, key(tea.KeyEscape))
	if len(m.editingUser().NamedFlags) != 1 {
		t.Errorf("Esc changed the flags: %v", m.editingUser().NamedFlags)
	}
}

func TestFlagsDialogWithoutRolesJSON(t *testing.T) {
	m := flagsModel(t, `{"roles": [`, &user.User{ID: 1, Handle: "Sysop", AccessLevel: 255})
	m = press(t, m, key(tea.KeyEnter))
	if !strings.Contains(m.flags.err, "can't be read") {
		t.Fatalf("broken roles.json: %q", m.flags.err)
	}
}
