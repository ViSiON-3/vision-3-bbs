package usereditor

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ViSiON-3/vision-3-bbs/internal/acs"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// rolesModel is a Model over u, with roles.json holding rolesJSON ("" for no
// file), on u's edit screen with the roles and groups dialog open.
func rolesModel(t *testing.T, rolesJSON string, u *user.User) Model {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "users.json")
	if _, err := SaveUsers(path, []*user.User{u}); err != nil {
		t.Fatal(err)
	}
	cfg := filepath.Join(dir, "configs")
	if err := os.Mkdir(cfg, 0o755); err != nil {
		t.Fatal(err)
	}
	if rolesJSON != "" {
		if err := os.WriteFile(filepath.Join(cfg, "roles.json"), []byte(rolesJSON), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	m, err := NewWithConfig(path, cfg)
	if err != nil {
		t.Fatal(err)
	}
	m = press(t, m, tea.WindowSizeMsg{Width: 80, Height: 25})
	return enterMode(t, m, modeMembership)
}

// typeInto replaces the focused input's text with s.
func typeInto(t *testing.T, m Model, s string) Model {
	t.Helper()
	d := &m.membership
	d.inputs[d.focus].SetValue("")
	return press(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)})
}

const membershipRoles = `{"roles": [{"name": "validator", "permissions": ["users.validate"]}],
	"groups": [{"name": "fsxnet-ops"}, {"name": "beta"}]}`

func TestMembershipDialogSetsRolesAndGroups(t *testing.T) {
	m := rolesModel(t, membershipRoles, &user.User{ID: 2, Handle: "Val", AccessLevel: 20})
	view := m.View()
	for _, want := range []string{"Roles & Groups for Val", "Roles defined: validator", "Groups defined: fsxnet-ops, beta"} {
		if !strings.Contains(view, want) {
			t.Errorf("dialog lacks %q", want)
		}
	}
	m = typeInto(t, m, " Validator ")
	m = press(t, m, key(tea.KeyTab))
	m = typeInto(t, m, "BETA, fsxnet-ops")
	m = press(t, m, key(tea.KeyEnter))
	if m.mode != modeEdit {
		t.Fatalf("mode = %v, err %q", m.mode, m.membership.err)
	}
	u := m.editingUser()
	if strings.Join(u.Roles, ",") != "validator" || strings.Join(u.Groups, ",") != "beta,fsxnet-ops" || !m.dirty {
		t.Fatalf("user = %v %v, dirty %v", u.Roles, u.Groups, m.dirty)
	}
	if got := membershipSummary(u); got != "1 role, 2 groups" {
		t.Errorf("summary = %q", got)
	}

	// The tester reads the same file.
	m.openACSTester()
	tc, _ := m.acsTest.tester()
	if x := acs.Render(tc.Explain("PERM:users.validate&ROLE:validator&GROUP:beta", u), 0); !strings.Contains(strings.Join(x, "\n"), "Result:     PASS") {
		t.Errorf("tester:\n%s", strings.Join(x, "\n"))
	}
}

func TestMembershipDialogRefusesUndefinedNames(t *testing.T) {
	m := rolesModel(t, membershipRoles, &user.User{ID: 2, Handle: "Val", AccessLevel: 20, Roles: []string{"validator"}})
	m = press(t, m, key(tea.KeyTab))
	m = typeInto(t, m, "ops")
	m = press(t, m, key(tea.KeyEnter))
	if m.mode != modeMembership || !strings.Contains(m.membership.err, "Groups: no group named ops") {
		t.Fatalf("mode %v, err %q", m.mode, m.membership.err)
	}
	if !strings.Contains(m.View(), "Groups: no group named ops") {
		t.Error("the error isn't shown")
	}
	// Esc leaves the user as they were.
	m = press(t, m, key(tea.KeyEscape))
	if u := m.editingUser(); m.mode != modeEdit || len(u.Groups) != 0 || m.dirty {
		t.Fatalf("after Esc: mode %v groups %v dirty %v", m.mode, u.Groups, m.dirty)
	}
}

func TestMembershipDialogWithoutRolesJSON(t *testing.T) {
	m := rolesModel(t, "", &user.User{ID: 1, Handle: "Sysop", AccessLevel: 255})
	m = typeInto(t, m, "validator")
	m = press(t, m, key(tea.KeyEnter))
	if !strings.Contains(m.membership.err, "defines no roles") {
		t.Fatalf("no roles.json: %q", m.membership.err)
	}
	m = rolesModel(t, `{"roles": [`, &user.User{ID: 1, Handle: "Sysop", AccessLevel: 255})
	m = press(t, m, key(tea.KeyEnter))
	if !strings.Contains(m.membership.err, "can't be read") {
		t.Fatalf("broken roles.json: %q", m.membership.err)
	}
}

// The edit screen keeps a row free for the next setting.
func TestMembershipFreesARow(t *testing.T) {
	for _, f := range editFields() {
		if f.Col == rightCol && f.Row == 14 {
			t.Errorf("right column row 14 is taken by %q", f.Label)
		}
	}
	if got := membershipSummary(&user.User{}); got != "(none)" {
		t.Errorf("summary = %q", got)
	}
}

// A stored list longer than the dialog's typing limit opens whole, and Enter
// keeps it whole.
func TestMembershipDialogKeepsLongLists(t *testing.T) {
	var names []string
	var groups []string
	for i := 0; i < 12; i++ {
		names = append(names, fmt.Sprintf(`{"name": "group_number_%02d"}`, i))
		groups = append(groups, fmt.Sprintf("group_number_%02d", i))
	}
	m := rolesModel(t, `{"roles": [], "groups": [`+strings.Join(names, ",")+`]}`,
		&user.User{ID: 2, Handle: "Val", AccessLevel: 20, Groups: groups})
	want := strings.Join(groups, ", ")
	if len(want) <= membershipCharLimit {
		t.Fatalf("list of %d characters doesn't test the limit", len(want))
	}
	if got := m.membership.inputs[memberGroups].Value(); got != want {
		t.Fatalf("dialog opened with %q", got)
	}
	m = press(t, m, key(tea.KeyEnter))
	if got := strings.Join(m.editingUser().Groups, ", "); got != want || m.dirty {
		t.Errorf("after Enter: groups %q, dirty %v", got, m.dirty)
	}
}
