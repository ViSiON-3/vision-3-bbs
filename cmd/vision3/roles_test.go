package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/perm"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

func writeBoardFile(t *testing.T, root, rel, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

type doctorLine struct {
	name    string
	status  doctorSeverity
	message string
}

func collectChecks(root string, check func(string, func(string, doctorSeverity, string, string))) []doctorLine {
	var out []doctorLine
	check(root, func(name string, status doctorSeverity, message, _ string) {
		out = append(out, doctorLine{name, status, message})
	})
	return out
}

func TestScanACSUndefinedRoles(t *testing.T) {
	root := t.TempDir()
	writeBoardFile(t, root, "configs/roles.json", `{"roles": [{"name": "validator", "permissions": ["users.validate"]}]}`)
	writeBoardFile(t, root, "menus/v3/mnu/MAIN.MNU", `{"ACS": ""}`)
	writeBoardFile(t, root, "menus/v3/cfg/MAIN.CFG", `[
		{"KEYS": "V", "CMD": "RUN:VALIDATEUSER", "ACS": "PERM:users.validate"},
		{"KEYS": "R", "CMD": "GOTO:ADMIN", "ACS": "ROLE:validator|!ROLE:msgops"},
		{"KEYS": "P", "CMD": "GOTO:ADMIN", "ACS": "PERM:users.vaildate"}
	]`)
	findings, _ := scanACS(root)
	if len(findings) != 2 {
		t.Fatalf("findings = %v", findings)
	}
	if f := findings[0]; !strings.Contains(f.Detail(), `"ROLE:validator|!ROLE:msgops": names ROLE:msgops, which configs/roles.json doesn't define`) || !f.failing() {
		t.Errorf("undefined role finding = %s", f.Detail())
	}
	if f := findings[1]; !strings.Contains(f.Detail(), `"users.vaildate" is not a permission`) {
		t.Errorf("unknown permission finding = %s", f.Detail())
	}
	for _, c := range collectChecks(root, checkACSStrings) {
		if c.status != doctorFail {
			t.Errorf("check %+v is not a failure", c)
		}
	}

	// Defining the role clears the finding.
	writeBoardFile(t, root, "configs/roles.json", `{"roles": [{"name": "validator", "permissions": ["users.validate"]}, {"name": "msgops", "permissions": ["users.validate"]}]}`)
	if findings, _ := scanACS(root); len(findings) != 1 {
		t.Errorf("findings after defining msgops = %v", findings)
	}
}

func TestCheckRoles(t *testing.T) {
	root := t.TempDir()
	if got := collectChecks(root, checkRoles); len(got) != 1 || got[0].status != doctorInfo || doctorSection(got[0].name) != "ACCESS CONTROL" {
		t.Fatalf("no roles.json: %+v", got)
	}

	writeBoardFile(t, root, "configs/roles.json", `{"roles": [`)
	if got := collectChecks(root, checkRoles); len(got) != 1 || got[0].status != doctorFail {
		t.Fatalf("bad JSON: %+v", got)
	}

	writeBoardFile(t, root, "configs/roles.json", `{"roles": [{"name": "validator", "permissions": ["users.validate"]}, {"name": "typo", "permissions": ["users.nope"]}]}`)
	writeBoardFile(t, root, "data/users/users.json", `[
		{"id": 1, "handle": "Sysop", "accessLevel": 255},
		{"id": 2, "handle": "Val", "roles": ["validator"]},
		{"id": 3, "handle": "Stale", "roles": ["gone", "validator"]},
		{"id": 4, "handle": "Removed", "roles": ["gone"], "deletedUser": true}
	]`)
	got := collectChecks(root, checkRoles)
	var msgs []string
	for _, c := range got {
		if c.status != doctorWarn {
			t.Errorf("check %+v is not a warning", c)
		}
		msgs = append(msgs, c.message)
	}
	joined := strings.Join(msgs, "\n")
	for _, want := range []string{`role 2 ("typo"): "users.nope" is not a permission`, "Stale (#3) has role gone, which configs/roles.json doesn't define"} {
		if !strings.Contains(joined, want) {
			t.Errorf("checks lack %q:\n%s", want, joined)
		}
	}
	if strings.Contains(joined, "Removed") {
		t.Errorf("deleted user reported:\n%s", joined)
	}

	writeBoardFile(t, root, "configs/roles.json", `{"roles": [{"name": "validator", "permissions": ["users.validate"]}, {"name": "gone", "permissions": ["users.validate"]}]}`)
	if got := collectChecks(root, checkRoles); len(got) != 1 || got[0].status != doctorOK || got[0].message != "2 role(s) defined, 2 user(s) with a role" {
		t.Fatalf("clean: %+v", got)
	}

	// A users.json doctor can't decode is said, not passed over as OK.
	writeBoardFile(t, root, "data/users/users.json", `[{"id": 1,`)
	if got := collectChecks(root, checkRoles); len(got) != 1 || got[0].status != doctorWarn || !strings.Contains(got[0].message, "users' roles weren't checked") {
		t.Fatalf("bad users.json: %+v", got)
	}
}

// A roles.json that can't be read grants nothing, at startup or on reload,
// and removing it removes every role.
func TestLoadRolesFailsClosed(t *testing.T) {
	t.Cleanup(func() { perm.SetCurrent(nil) })
	dir := t.TempDir()
	member := &user.User{AccessLevel: 20, Roles: []string{"validator"}}
	allowed := func() bool {
		return perm.Check(member, perm.UsersValidate, perm.Scope{}, perm.Levels{SysOp: 255, CoSysOp: 250}, nil).Allowed
	}

	writeBoardFile(t, dir, perm.FileName, `{"roles": [{"name": "validator", "permissions": ["users.validate"]}]}`)
	loadRoles(dir)
	if !allowed() {
		t.Fatal("role not in force after load")
	}
	writeBoardFile(t, dir, perm.FileName, `{"roles": [{"name": "validator", "permissions": ["users.validate"]}`)
	loadRoles(dir)
	if allowed() {
		t.Fatal("role still in force after roles.json broke")
	}
	writeBoardFile(t, dir, perm.FileName, `{"roles": [{"name": "validator", "permissions": ["users.validate"]}]}`)
	loadRoles(dir)
	if err := os.Remove(filepath.Join(dir, perm.FileName)); err != nil {
		t.Fatal(err)
	}
	loadRoles(dir)
	if allowed() {
		t.Fatal("role still in force after roles.json was removed")
	}
}

func TestCheckRolesUnknownGrantTags(t *testing.T) {
	root := t.TempDir()
	writeBoardFile(t, root, "configs/message_areas.json", `[{"id": 1, "tag": "GENERAL"}]`)
	writeBoardFile(t, root, "configs/conferences.json", `[{"id": 1, "tag": "LOCAL"}]`)
	writeBoardFile(t, root, "configs/roles.json", `{"roles": [{"name": "mod", "permissions": [
		{"permission": "messages.moderate", "areas": ["general", "GENRAL"], "conferences": ["local", "FSX"]}]}]}`)
	var msgs []string
	for _, c := range collectChecks(root, checkRoles) {
		if c.status != doctorWarn {
			t.Errorf("check %+v is not a warning", c)
		}
		msgs = append(msgs, c.message)
	}
	joined := strings.Join(msgs, "\n")
	for _, want := range []string{"names area GENRAL, which message_areas.json doesn't have", "names conference FSX, which conferences.json doesn't have"} {
		if !strings.Contains(joined, want) {
			t.Errorf("checks lack %q:\n%s", want, joined)
		}
	}
	if len(msgs) != 2 {
		t.Errorf("checks = %v", msgs)
	}
}
