package menu

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
)

func TestSignupBadUserNamesAndFailurePolicy(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "badusers.txt")
	e := &MenuExecutor{RootConfigPath: dir}
	e.SetServerConfig(config.ServerConfig{})
	check := func(handle string, want bool) {
		t.Helper()
		if got := e.validateSignupHandle(handle); got != want {
			t.Fatalf("%q: got %t, want %t", handle, got, want)
		}
	}
	for _, data := range [][]byte{nil, []byte(""), []byte("admin*\n\xff"), []byte("bad\x00name")} {
		if data != nil {
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
		}
		check("Admin", true)
		check("Felonius", true)
		for _, name := range []string{"new", "NEW", "q", "SysOp", "12", "bad/name"} {
			check(name, false)
		}
	}
	if err := os.WriteFile(path, []byte("admin*\nroot\n"), 0600); err != nil {
		t.Fatal(err)
	}
	check("Admin", false)
	check("ROOT", false)
	check("uproot", true)
	custom := filepath.Join(dir, "custom.txt")
	if err := os.WriteFile(custom, []byte("Felonius"), 0600); err != nil {
		t.Fatal(err)
	}
	e.SetServerConfig(config.ServerConfig{BadUsersPath: custom})
	check("Felonius", false)
	check("Admin", true)
}

func TestNewUser_BadNameRetriesThroughMenuCommand(t *testing.T) {
	env := newMenuEnv(t)
	if err := os.WriteFile(filepath.Join(env.e.RootConfigPath, "badusers.txt"), []byte("admin*\n"), 0600); err != nil {
		t.Fatal(err)
	}
	input := "Y\rAdmin Jane\rLegitimate\rpw123\rpw123\rReal Name\rnote\rTown\r\r"
	r := env.runCmd("NEWUSER", nil, "", input)
	if r.err != nil {
		t.Fatal(r.err)
	}
	if _, ok := env.um.GetUser("Admin Jane"); ok {
		t.Fatal("blocked name created")
	}
	mustGetUser(t, env, "Legitimate")
}

func TestUserEditor_BadNameWarnsAndAllowsOverride(t *testing.T) {
	env := newMenuEnv(t)
	if err := os.WriteFile(filepath.Join(env.e.RootConfigPath, "badusers.txt"), []byte("admin*"), 0600); err != nil {
		t.Fatal(err)
	}
	input := "j" + "a" + ueClear(6) + "Admin Jane\r" + "s" + "q"
	r := env.runCmd("ADMINLISTUSERS", env.sysop, "", input)
	if r.err != nil {
		t.Fatal(r.err)
	}
	if !r.has("Warning: handle matches bad user names list", "Changes saved for Admin Jane.") {
		t.Fatalf("output: %s", r.text())
	}
	u, ok := env.diskUser(2)
	if !ok || u.Handle != "Admin Jane" {
		t.Fatalf("override not saved: %+v", u)
	}
}
