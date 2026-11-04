//go:build !windows

package menu

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// doorACSFixture is a native door that leaves RAN in its working directory
// when it runs, and a DOOR.SYS dropfile there when it is set up, so a test can
// tell whether a refused door got as far as either.
type doorACSFixture struct {
	dir string
}

func newDoorACSFixture(t *testing.T) doorACSFixture {
	t.Helper()
	doorcovIsolateTemp(t)
	return doorACSFixture{dir: t.TempDir()}
}

func (f doorACSFixture) door(code string, minLevel int, acs string, hidden bool, category string) config.DoorConfig {
	return config.DoorConfig{Code: code, Name: code, Commands: []string{"/bin/sh", "-c", "touch RAN"},
		WorkingDirectory: f.dir, DropfileType: "DOOR.SYS", MinAccessLevel: minLevel, ACS: acs,
		Hidden: hidden, Category: category}
}

func (f doorACSFixture) ran() bool      { return fileExistsAt(filepath.Join(f.dir, "RAN")) }
func (f doorACSFixture) dropfile() bool { return fileExistsAt(filepath.Join(f.dir, "DOOR.SYS")) }
func (f doorACSFixture) reset(t *testing.T) {
	t.Helper()
	for _, n := range []string{"RAN", "DOOR.SYS"} {
		if err := os.Remove(filepath.Join(f.dir, n)); err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
	}
}

func fileExistsAt(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// withFlags is a copy of u with its flags set to flags.
func withFlags(u *user.User, flags string) *user.User {
	c := *u
	c.Flags = flags
	return &c
}

// DOOR:CODE (menu commands, the generated menu, login steps) launches a door
// only when both of its rules pass, and refuses before the dropfile is
// written or the program started.
func TestDoorACSDirectLaunch(t *testing.T) {
	f := newDoorACSFixture(t)
	env := newMenuEnv(t)
	env.e.SetDoorRegistry(map[string]config.DoorConfig{
		"PLAIN":  f.door("PLAIN", 0, "", false, ""),
		"LEVEL":  f.door("LEVEL", 50, "", false, ""),
		"FLAG":   f.door("FLAG", 0, "FG", false, ""),
		"BOTH":   f.door("BOTH", 10, "FG", false, ""),
		"HIDDEN": f.door("HIDDEN", 0, "FG", true, ""),
		"BADACS": f.door("BADACS", 0, "S10&", false, ""),
		"NOTQ":   f.door("NOTQ", 0, "!Q", false, ""),
	})
	door := env.e.RunRegistry["DOOR:"]
	flagged := withFlags(env.caller, "G")

	for _, tc := range []struct {
		name string
		code string
		u    *user.User
		want bool
	}{
		{"no acs: numeric rule only", "PLAIN", env.caller, true},
		{"level too low", "LEVEL", env.caller, false},
		{"acs not met", "FLAG", env.caller, false},
		{"acs met", "FLAG", flagged, true},
		{"both met", "BOTH", flagged, true},
		{"level met, acs not", "BOTH", env.caller, false},
		{"hidden but allowed still launches", "HIDDEN", flagged, true},
		{"hidden and not allowed", "HIDDEN", env.caller, false},
		{"malformed acs denies", "BADACS", env.sysop, false},
		// strict: an unknown term negated does not pass
		{"!Q denies the caller", "NOTQ", flagged, false},
		{"!Q denies a sysop", "NOTQ", env.sysop, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f.reset(t)
			s := newDoorcovSession()
			r := doorcovRun(env.sub(t), s, door, tc.u, tc.code)
			if r.err != nil {
				t.Fatalf("err = %v", r.err)
			}
			if f.ran() != tc.want {
				t.Errorf("door ran = %v, want %v", f.ran(), tc.want)
			}
			if !tc.want && f.dropfile() {
				t.Error("refused door had its dropfile written")
			}
		})
	}
}

// The rule is checked at launch, not only when the menu was drawn: a flag
// taken away after the generated menu listed the door stops the launch.
func TestDoorACSRecheckedAtLaunch(t *testing.T) {
	f := newDoorACSFixture(t)
	env := newMenuEnv(t)
	doors := map[string]config.DoorConfig{"FLAG": f.door("FLAG", 0, "FG", false, "")}
	env.e.SetDoorRegistry(doors)
	u := withFlags(env.caller, "G")

	allows := func(acs string) bool { return checkACS(acs, u, nil, nil, time.Now()) }
	if got := doorMenuCodes(buildDoorMenuEntries(doors, config.ServerConfig{}, "", u.AccessLevel, allows, "Other")); len(got) != 1 {
		t.Fatalf("menu lists %v, want FLAG", got)
	}
	u.Flags = "" // the flag is revoked while the menu is on screen

	doorcovRun(env, newDoorcovSession(), env.e.RunRegistry["DOOR:"], u, "FLAG")
	if f.ran() || f.dropfile() {
		t.Error("door launched after its acs stopped passing")
	}
}

// A category's rules filter the generated menu only. A door in a category the
// caller can't see still launches by DOOR:CODE when its own rules pass.
func TestDoorACSCategoryIsAMenuFilter(t *testing.T) {
	f := newDoorACSFixture(t)
	env := newMenuEnv(t)
	doors := map[string]config.DoorConfig{"LOUNGE": f.door("LOUNGE", 0, "", false, "VIP")}
	env.e.SetDoorRegistry(doors)
	cfg := config.ServerConfig{DoorCategories: []config.DoorCategory{{Code: "VIP", Name: "VIP", ACS: "FV"}}}

	allows := func(acs string) bool { return checkACS(acs, env.caller, nil, nil, time.Now()) }
	if got := doorMenuCodes(buildDoorMenuEntries(doors, cfg, "VIP", env.caller.AccessLevel, allows, "Other")); len(got) != 0 {
		t.Errorf("generated menu lists %v in a category the caller fails", got)
	}
	doorcovRun(env, newDoorcovSession(), env.e.RunRegistry["DOOR:"], env.caller, "LOUNGE")
	if !f.ran() {
		t.Error("direct launch refused because of the category's rule")
	}
}

// OPENDOOR, DOORINFO and LISTDOORS apply the same rule; a denied door's
// details are never shown, and DOORINFO keeps the acs itself to itself.
func TestDoorACSListOpenInfo(t *testing.T) {
	f := newDoorACSFixture(t)
	env := newMenuEnv(t)
	env.e.SetDoorRegistry(map[string]config.DoorConfig{
		"OPEN":  f.door("OPEN", 0, "", false, ""),
		"FLAG":  f.door("FLAG", 0, "FG", false, ""),
		"OTHER": f.door("OTHER", 0, "FX", false, ""),
		"NOTQ":  f.door("NOTQ", 0, "!Q", false, ""),
		"BROKE": f.door("BROKE", 0, "S10&", false, ""),
	})
	flagged := withFlags(env.caller, "G")

	t.Run("LISTDOORS", func(t *testing.T) {
		r := env.sub(t).runCmd("LISTDOORS", env.caller, "", "")
		if r.has("FLAG") || r.has("OTHER") || !r.has("OPEN") {
			t.Errorf("caller without flags:\n%s", r.text())
		}
		r = env.sub(t).runCmd("LISTDOORS", flagged, "", "")
		if !r.has("FLAG") || r.has("OTHER") {
			t.Errorf("caller with flag G:\n%s", r.text())
		}
		// strict door acs: !Q and a malformed string list for no one
		r = env.sub(t).runCmd("LISTDOORS", env.sysop, "", "")
		if r.has("NOTQ") || r.has("BROKE") || !r.has("OPEN") {
			t.Errorf("sysop, strict door acs:\n%s", r.text())
		}
	})

	t.Run("DOORINFO", func(t *testing.T) {
		r := env.sub(t).runCmd("DOORINFO", env.caller, "", "flag\r")
		if !r.has("Access denied") || r.has("Door: FLAG") || r.has(f.dir) {
			t.Errorf("denied door's details shown:\n%s", r.text())
		}
		r = env.sub(t).runCmd("DOORINFO", flagged, "", "flag\r")
		if !r.has("Door: FLAG") || r.has("FG") {
			t.Errorf("allowed caller:\n%s", r.text())
		}
		for _, code := range []string{"notq", "broke"} {
			r = env.sub(t).runCmd("DOORINFO", env.sysop, "", code+"\r")
			if !r.has("Access denied") || r.has("Door: "+strings.ToUpper(code)) {
				t.Errorf("sysop, strict door acs (%s):\n%s", code, r.text())
			}
		}
	})

	t.Run("OPENDOOR", func(t *testing.T) {
		f.reset(t)
		s := newDoorcovSession()
		s.send("other\rq\r") // denied, back at the prompt: quit
		r := doorcovRun(env.sub(t), s, runOpenDoor, flagged, "")
		if f.ran() || f.dropfile() || !r.has("Access denied") {
			t.Errorf("OPENDOOR launched a door the caller fails (ran=%v):\n%s", f.ran(), r.text())
		}
		f.reset(t)
		s = newDoorcovSession()
		s.send("flag\r")
		doorcovRun(env.sub(t), s, runOpenDoor, flagged, "")
		if !f.ran() {
			t.Error("OPENDOOR refused a door the caller passes")
		}
		for _, code := range []string{"notq", "broke"} {
			f.reset(t)
			s = newDoorcovSession()
			s.send(code + "\rq\r")
			r = doorcovRun(env.sub(t), s, runOpenDoor, env.sysop, "")
			if f.ran() || f.dropfile() || !r.has("Access denied") {
				t.Errorf("OPENDOOR, strict door acs (%s), ran=%v:\n%s", code, f.ran(), r.text())
			}
		}
	})
}

// A denial says only that access is denied.
func TestDoorACSDeniedText(t *testing.T) {
	f := newDoorACSFixture(t)
	env := newMenuEnv(t)
	env.e.SetDoorRegistry(map[string]config.DoorConfig{"FLAG": f.door("FLAG", 0, "FG", false, "")})
	_, stderr := execcovRunFn(env, env.e.RunRegistry["DOOR:"], env.caller, "flag", "")
	got := testAnsiEscape.ReplaceAllString(stderr, "")
	if !strings.Contains(got, "Access denied") || strings.Contains(got, "FG") {
		t.Errorf("denial text = %q", got)
	}
}
