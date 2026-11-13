package usereditor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/acs"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// rolesModel is a Model over one user, with roles.json holding rolesJSON
// ("" for no file).
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
	return m
}

func rolesFieldOf(t *testing.T, m Model) fieldDef {
	t.Helper()
	for _, f := range m.fields {
		if f.Label == rolesField {
			return f
		}
	}
	t.Fatal("no Roles field")
	return fieldDef{}
}

func TestRolesFieldChecksRolesJSON(t *testing.T) {
	m := rolesModel(t, `{"roles": [{"name": "validator", "permissions": ["users.validate"]}]}`, &user.User{ID: 2, Handle: "Val", AccessLevel: 20})
	f := rolesFieldOf(t, m)
	u := &user.User{Handle: "Val"}
	if err := f.Set(u, "nobody"); err == nil || !strings.Contains(err.Error(), "no role named nobody") {
		t.Fatalf("undefined role: %v", err)
	}
	if u.Roles != nil {
		t.Fatalf("a refused edit stored %v", u.Roles)
	}
	if err := f.Set(u, " Validator "); err != nil || f.Get(u) != "validator" {
		t.Fatalf("Set = %v, Get = %q", err, f.Get(u))
	}
	if err := f.Set(u, ""); err != nil || u.Roles != nil {
		t.Fatalf("clearing: %v %v", err, u.Roles)
	}

	// The tester reads the same file.
	m.openACSTester()
	tc, msg := m.acsTest.tester()
	if msg != "" {
		t.Fatal(msg)
	}
	x := acs.Render(tc.Explain("PERM:users.validate&ROLE:validator", &user.User{AccessLevel: 20, Roles: []string{"validator"}}), 0)
	if !strings.Contains(strings.Join(x, "\n"), "Result:     PASS") {
		t.Errorf("tester:\n%s", strings.Join(x, "\n"))
	}
}

func TestRolesFieldWithoutRolesJSON(t *testing.T) {
	m := rolesModel(t, "", &user.User{ID: 1, Handle: "Sysop", AccessLevel: 255})
	if err := rolesFieldOf(t, m).Set(&user.User{}, "validator"); err == nil || !strings.Contains(err.Error(), "defines no roles") {
		t.Fatalf("no roles.json: %v", err)
	}
	m = rolesModel(t, `{"roles": [`, &user.User{ID: 1, Handle: "Sysop", AccessLevel: 255})
	if err := rolesFieldOf(t, m).Set(&user.User{}, "validator"); err == nil || !strings.Contains(err.Error(), "can't be read") {
		t.Fatalf("broken roles.json: %v", err)
	}
}

// The Roles field is narrower than a list of several roles; the input takes
// the whole list and scrolls.
func TestRolesFieldTakesLongerLists(t *testing.T) {
	long := "validator, msg_ops, fsxnet_mod"
	m := rolesModel(t, `{"roles": []}`, &user.User{ID: 2, Handle: "Val", AccessLevel: 20})
	for i, f := range m.fields {
		if f.Label == rolesField {
			m.editField = i
		}
	}
	m.users[m.editIndex].Roles = []string{"validator", "msg_ops", "fsxnet_mod"}
	m, _ = m.startFieldEdit()
	if got := m.textInput.Value(); got != long {
		t.Fatalf("value = %q, want %q", got, long)
	}
	if m.textInput.CharLimit < 200 {
		t.Errorf("CharLimit = %d", m.textInput.CharLimit)
	}
}
