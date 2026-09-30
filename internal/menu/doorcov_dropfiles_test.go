//go:build !windows

package menu

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/config"
)

// doorcovDropCtx is a DoorCtx with every field the dropfile generators read
// set to a distinct value, so a swapped line shows up.
func doorcovDropCtx() *DoorCtx {
	return &DoorCtx{
		Executor: newExecutorWithServerConfig(config.ServerConfig{BoardName: "Test BBS"}),
		User: doorUserInfo{
			ID: 7, Handle: "Neo", RealName: "Thomas Anderson", AccessLevel: 50,
			TimesCalled: 12, GroupLocation: "Zion", ScreenWidth: 132, ScreenHeight: 37,
		},
		SessionStartTime: time.Now(),
		NodeNumStr:       "3",
		UserIDStr:        "7",
		TimeLeftMin:      30,
	}
}

func TestDoorcovBuildDoorCtx(t *testing.T) {
	env := newMenuEnv(t)
	s := newDoorcovSession()
	cfg := config.DoorConfig{WorkingDirectory: "/doors/lord"}

	ctx := buildDoorCtx(env.e, s, nil, 7, "Neo", "Thomas Anderson", 50, 60, 12, "Zion",
		132, 37, 3, time.Now().Add(-15*time.Minute), ansi.OutputModeCP437, cfg, "LORD")

	wantSubs := map[string]string{
		"{NODE}": "3", "{PORT}": "3", "{TIMELEFT}": "45", "{BAUD}": "38400",
		"{USERHANDLE}": "Neo", "{USERID}": "7", "{REALNAME}": "Thomas Anderson",
		"{LEVEL}": "50", "{USERIP}": "203.0.113.7", "{STARTUPDIR}": "/doors/lord",
		"{DROPFILE}": "", "{NODEDIR}": "",
	}
	for k, want := range wantSubs {
		if got, ok := ctx.Subs[k]; !ok || got != want {
			t.Errorf("Subs[%s] = %q (present=%v), want %q", k, got, ok, want)
		}
	}
	if len(ctx.Subs) != len(wantSubs) {
		t.Errorf("Subs has %d placeholders, want %d: %v", len(ctx.Subs), len(wantSubs), ctx.Subs)
	}
	wantUser := doorUserInfo{ID: 7, Handle: "Neo", RealName: "Thomas Anderson", AccessLevel: 50,
		TimeLimit: 60, TimesCalled: 12, GroupLocation: "Zion", ScreenWidth: 132, ScreenHeight: 37}
	if ctx.User != wantUser {
		t.Errorf("User = %+v, want %+v", ctx.User, wantUser)
	}
	if ctx.TimeLeftMin != 45 || ctx.TimeLeftStr != "45" || ctx.NodeNumStr != "3" ||
		ctx.PortStr != "3" || ctx.UserIDStr != "7" || ctx.BaudStr != "38400" {
		t.Errorf("precomputed values wrong: %+v", ctx)
	}
	if ctx.DoorName != "LORD" || ctx.NodeNumber != 3 || ctx.OutputMode != ansi.OutputModeCP437 || ctx.Session != s {
		t.Errorf("context not carried through: %+v", ctx)
	}
}

// A caller past their time limit gets zero minutes, never a negative count,
// and a door with no working directory starts in ".".
func TestDoorcovBuildDoorCtxExpiredAndDefaults(t *testing.T) {
	env := newMenuEnv(t)
	ctx := buildDoorCtx(env.e, nil, nil, 2, "Caller", "Carl Caller", 10, 60, 0, "",
		0, 0, 1, time.Now().Add(-2*time.Hour), ansi.OutputModeUTF8, config.DoorConfig{}, "X")

	if ctx.TimeLeftMin != 0 || ctx.Subs["{TIMELEFT}"] != "0" {
		t.Errorf("time left = %d / %q, want 0", ctx.TimeLeftMin, ctx.Subs["{TIMELEFT}"])
	}
	if got := ctx.Subs["{STARTUPDIR}"]; got != "." {
		t.Errorf("{STARTUPDIR} = %q, want %q", got, ".")
	}
	if got := ctx.Subs["{USERIP}"]; got != "" {
		t.Errorf("{USERIP} = %q with no session, want empty", got)
	}
}

func TestDoorcovGenerateDoorSys(t *testing.T) {
	dir := t.TempDir()
	if err := generateDoorSys(doorcovDropCtx(), dir, "DOOR.SYS"); err != nil {
		t.Fatalf("generateDoorSys: %v", err)
	}
	lines := doorcovLines(t, filepath.Join(dir, "DOOR.SYS"))
	if len(lines) != 52 {
		t.Fatalf("DOOR.SYS has %d lines, want 52", len(lines))
	}
	doorcovWantLines(t, lines, map[int]string{
		1: "COM1:", 4: "3", 10: "Thomas Anderson", 11: "Zion", 15: "50", 16: "12",
		18: "1800", 21: "37", 26: "7", 35: "Test BBS", 36: "Neo", 52: "0",
	})

	// No saved screen height: the door is told 25 lines.
	ctx := doorcovDropCtx()
	ctx.User.ScreenHeight = 0
	if err := generateDoorSys(ctx, dir, "door.sys"); err != nil {
		t.Fatalf("generateDoorSys: %v", err)
	}
	doorcovWantLines(t, doorcovLines(t, filepath.Join(dir, "door.sys")), map[int]string{21: "25"})
}

func TestDoorcovGenerateDorInfo(t *testing.T) {
	for _, tc := range []struct {
		name, realName, location string
		first, last, wantLoc     string
	}{
		{"first and last", "Thomas Anderson", "Zion", "Thomas", "Anderson", "Zion"},
		{"everything after the first space is the last name", "Thomas A. Anderson", "Zion", "Thomas", "A. Anderson", "Zion"},
		{"single name", "Neo", "Zion", "Neo", " ", "Zion"},
		{"trailing space", "Neo ", "Zion", "Neo", " ", "Zion"},
		{"no location", "Thomas Anderson", "", "Thomas", "Anderson", "Somewhere"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			ctx := doorcovDropCtx()
			ctx.User.RealName, ctx.User.GroupLocation = tc.realName, tc.location
			if err := generateDorInfo(ctx, dir, "DORINFO1.DEF"); err != nil {
				t.Fatalf("generateDorInfo: %v", err)
			}
			lines := doorcovLines(t, filepath.Join(dir, "DORINFO1.DEF"))
			if len(lines) != 13 {
				t.Fatalf("DORINFO1.DEF has %d lines, want 13", len(lines))
			}
			doorcovWantLines(t, lines, map[int]string{
				1: "Test BBS", 4: "COM1", 7: tc.first, 8: tc.last, 9: tc.wantLoc,
				11: "50", 12: "30", 13: "-1",
			})
		})
	}
}

func TestDoorcovGenerateChainTxt(t *testing.T) {
	dir := t.TempDir()
	ctx := doorcovDropCtx()
	ctx.SessionStartTime = time.Now().Add(-10*time.Minute - 30*time.Second)
	if err := generateChainTxt(ctx, dir, "CHAIN.TXT"); err != nil {
		t.Fatalf("generateChainTxt: %v", err)
	}
	lines := doorcovLines(t, filepath.Join(dir, "CHAIN.TXT"))
	if len(lines) != 30 {
		t.Fatalf("CHAIN.TXT has %d lines, want 30", len(lines))
	}
	doorcovWantLines(t, lines, map[int]string{
		1: "7", 2: "Neo", 3: "Thomas Anderson", 5: "10", 9: "132", 10: "37", 11: "50",
		16: "1800", 17: dir, 18: dir, 22: "Test BBS", 30: "8N1",
	})

	// No saved screen size: the door is told 80x25.
	ctx.User.ScreenWidth, ctx.User.ScreenHeight = 0, 0
	if err := generateChainTxt(ctx, dir, "chain.txt"); err != nil {
		t.Fatalf("generateChainTxt: %v", err)
	}
	doorcovWantLines(t, doorcovLines(t, filepath.Join(dir, "chain.txt")), map[int]string{9: "80", 10: "25"})
}

func TestDoorcovGenerateDoor32Sys(t *testing.T) {
	dir := t.TempDir()
	if err := generateDoor32Sys(doorcovDropCtx(), dir, "DOOR32.SYS"); err != nil {
		t.Fatalf("generateDoor32Sys: %v", err)
	}
	lines := doorcovLines(t, filepath.Join(dir, "DOOR32.SYS"))
	want := []string{"0", "0", "38400", "Test BBS", "7", "Thomas Anderson", "Neo", "50", "30", "1", "3"}
	if strings.Join(lines, "|") != strings.Join(want, "|") {
		t.Errorf("DOOR32.SYS = %q\nwant %q", lines, want)
	}
}

func TestDoorcovGenerateAllDropfiles(t *testing.T) {
	// The directory is created on demand, private to the BBS user.
	dir := filepath.Join(t.TempDir(), "nodes", "temp3")
	if err := generateAllDropfiles(doorcovDropCtx(), dir, `C:\NODES\TEMP1`); err != nil {
		t.Fatalf("generateAllDropfiles: %v", err)
	}
	for name, wantLines := range map[string]int{"DOOR.SYS": 52, "DOOR32.SYS": 11, "DORINFO1.DEF": 13, "CHAIN.TXT": 30} {
		if got := len(doorcovLines(t, filepath.Join(dir, name))); got != wantLines {
			t.Errorf("%s has %d lines, want %d", name, got, wantLines)
		}
		// Dropfiles carry the caller's details: nobody else gets to read them.
		if fi, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("stat %s: %v", name, err)
		} else if fi.Mode().Perm() != 0o600 {
			t.Errorf("%s: mode %v, want 0600", name, fi.Mode().Perm())
		}
	}
}

// Each generator's failure is reported under the name of the file it could not
// write. A directory squatting on the filename makes exactly that write fail.
func TestDoorcovGenerateAllDropfilesErrors(t *testing.T) {
	for _, name := range []string{"DOOR.SYS", "DOOR32.SYS", "DORINFO1.DEF", "CHAIN.TXT"} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.Mkdir(filepath.Join(dir, name), 0o755); err != nil {
				t.Fatal(err)
			}
			err := generateAllDropfiles(doorcovDropCtx(), dir, `C:\NODES\TEMP1`)
			if err == nil || !strings.Contains(err.Error(), "failed to generate "+name) {
				t.Errorf("err = %v, want a failure naming %s", err, name)
			}
		})
	}

	t.Run("directory cannot be created", func(t *testing.T) {
		file := filepath.Join(t.TempDir(), "not-a-dir")
		if err := os.WriteFile(file, nil, 0o644); err != nil {
			t.Fatal(err)
		}
		err := generateAllDropfiles(doorcovDropCtx(), filepath.Join(file, "node"), `C:\NODES\TEMP1`)
		if err == nil || !strings.Contains(err.Error(), "failed to create dropfile directory") {
			t.Errorf("err = %v, want a directory creation failure", err)
		}
	})
}

func TestDoorcovCleanupDropfiles(t *testing.T) {
	dir := t.TempDir()
	if err := generateAllDropfiles(doorcovDropCtx(), dir, `C:\NODES\TEMP1`); err != nil {
		t.Fatalf("generateAllDropfiles: %v", err)
	}
	for _, name := range []string{"EXTERNAL.BAT", "SAVEGAME.DAT"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	cleanupDropfiles(dir)

	for _, name := range []string{"DOOR.SYS", "DOOR32.SYS", "DORINFO1.DEF", "CHAIN.TXT", "EXTERNAL.BAT"} {
		if doorcovExists(filepath.Join(dir, name)) {
			t.Errorf("%s survived cleanup", name)
		}
	}
	// Only the files the BBS generated go: the door's own data stays.
	if !doorcovExists(filepath.Join(dir, "SAVEGAME.DAT")) {
		t.Error("cleanup removed a file it did not generate")
	}

	// Cleaning an already-clean directory, or one where a file cannot be
	// removed, is not an error: the rest are still removed.
	if err := os.MkdirAll(filepath.Join(dir, "DOOR.SYS", "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "CHAIN.TXT"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	cleanupDropfiles(dir)
	if doorcovExists(filepath.Join(dir, "CHAIN.TXT")) {
		t.Error("CHAIN.TXT survived cleanup after an earlier removal failed")
	}
}
