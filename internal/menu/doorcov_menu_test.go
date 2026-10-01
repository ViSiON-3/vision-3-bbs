//go:build !windows

package menu

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/config"
)

// doorcovRegistry is a door list covering every door type, one of them
// (SECRET) out of an ordinary caller's reach.
func doorcovRegistry() map[string]config.DoorConfig {
	return map[string]config.DoorConfig{
		"LORD": {Name: "Legend of the Red Dragon", IsDOS: true, Commands: []string{"START.BAT {NODE}", "CLEAN.EXE"},
			WorkingDirectory: `C:\LORD`, DropfileType: "DOOR.SYS", SingleInstance: true},
		"HELLO":  {Name: "Hello World", Type: "v3_script", Script: "hello.js"},
		"SECRET": {Name: "Sysop Tools", Commands: []string{"/bin/true"}, MinAccessLevel: 100},
		"SYNC":   {Name: "Sync Game", Type: "synchronet_js", Script: "game.js"},
		"REMOTE": {Name: "Door Server", Type: "rlogin", Host: "doors.example.net", Port: 2513},
		"NATIVE": {Name: "Native Game", Commands: []string{"/opt/game/run", "-n", "{NODE}"}, DropfileType: "DOOR32.SYS",
			DropfileLocation: "node", IOMode: "SOCKET", UseShell: true, CleanupCommand: "/opt/game/tidy", MinAccessLevel: 5},
	}
}

func TestDoorcovListDoors(t *testing.T) {
	env := newMenuEnv(t)
	env.e.SetDoorRegistry(doorcovRegistry())

	t.Run("requires login", func(t *testing.T) {
		r := env.sub(t).runCmd("LISTDOORS", nil, "", "")
		if r.user != nil || !r.has("You must be logged in to list doors.") {
			t.Errorf("user=%v output:\n%s", r.user, r.text())
		}
	})

	// Doors are listed in code order, numbered as shown, each with its type.
	t.Run("sysop sees every door", func(t *testing.T) {
		r := env.sub(t).runCmd("LISTDOORS", env.sysop, "", "")
		if r.err != nil || r.user != env.sysop {
			t.Fatalf("err=%v user=%v", r.err, r.user)
		}
		var rows []string
		for _, line := range strings.Split(r.text(), "\n") {
			if f := strings.Fields(line); len(f) > 2 && strings.HasPrefix(f[0], "[") {
				rows = append(rows, strings.Join(f, " "))
			}
		}
		want := []string{
			"[1 ] HELLO Hello World (VPL)",
			"[2 ] LORD Legend of the Red Dragon (DOS)",
			"[3 ] NATIVE Native Game (Native)",
			"[4 ] REMOTE Door Server (RLogin)",
			"[5 ] SECRET Sysop Tools (Native)",
			"[6 ] SYNC Sync Game (Synchronet JS)",
		}
		if strings.Join(rows, "\n") != strings.Join(want, "\n") {
			t.Errorf("rows =\n%s\nwant\n%s", strings.Join(rows, "\n"), strings.Join(want, "\n"))
		}
	})

	// A door above the caller's level is left out and the numbering closes up.
	t.Run("caller does not see restricted doors", func(t *testing.T) {
		env := env.sub(t)
		env.outputMode = ansi.OutputModeCP437
		r := env.runCmd("LISTDOORS", env.caller, "", "")
		if r.has("SECRET") || !r.has("SYNC", "Sync Game") {
			t.Errorf("restricted door listed, or an open one missing:\n%s", r.text())
		}
		if !strings.Contains(strings.Join(strings.Fields(r.text()), " "), "[5 ] SYNC Sync Game") {
			t.Errorf("numbering did not close up over the hidden door:\n%s", r.text())
		}
	})

	t.Run("nothing the caller can run", func(t *testing.T) {
		env.e.SetDoorRegistry(map[string]config.DoorConfig{"SECRET": doorcovRegistry()["SECRET"]})
		defer env.e.SetDoorRegistry(doorcovRegistry())
		r := env.sub(t).runCmd("LISTDOORS", env.caller, "", "")
		if !r.has("No doors configured.") || r.has("SECRET") {
			t.Errorf("output:\n%s", r.text())
		}
	})
}

func TestDoorcovListDoorsMissingTemplates(t *testing.T) {
	env := newMenuEnv(t)
	env.e.SetDoorRegistry(doorcovRegistry())
	env.e.MenuSetPath = t.TempDir()

	r := env.runCmd("LISTDOORS", env.sysop, "", "")
	if !r.has("Error loading Door List templates.") || r.has("HELLO") {
		t.Errorf("output:\n%s", r.text())
	}
	if r.user != env.sysop {
		t.Errorf("user = %v, want the caller kept logged in", r.user)
	}
}

// doorcovPromptCases are the door-name prompt behaviours OPENDOOR and
// DOORINFO share; each ends with Q so the handler returns without acting.
func doorcovPromptCases(t *testing.T, cmd string) {
	env := newMenuEnv(t)
	env.e.SetDoorRegistry(doorcovRegistry())
	const prompt = "Door Name (?=List, Q=Quit): "

	t.Run("quit", func(t *testing.T) {
		r := env.sub(t).runCmd(cmd, env.caller, "", " q \r")
		if r.err != nil || r.user != env.caller || r.next != "" {
			t.Errorf("err=%v user=%v next=%q", r.err, r.user, r.next)
		}
		if got := strings.Count(r.text(), prompt); got != 1 {
			t.Errorf("prompted %d times, want once:\n%s", got, r.text())
		}
	})

	t.Run("blank re-prompts", func(t *testing.T) {
		r := env.sub(t).runCmd(cmd, env.caller, "", "\r  \rQ\r")
		if got := strings.Count(r.text(), prompt); got != 3 {
			t.Errorf("prompted %d times, want 3:\n%s", got, r.text())
		}
	})

	t.Run("question mark lists doors", func(t *testing.T) {
		r := env.sub(t).runCmd(cmd, env.caller, "", "?\rQ\r")
		if !r.has("Hello World", "Legend of the Red Dragon") || r.has("Sysop Tools") {
			t.Errorf("door list wrong:\n%s", r.text())
		}
		if got := strings.Count(r.text(), prompt); got != 2 {
			t.Errorf("prompted %d times, want 2:\n%s", got, r.text())
		}
	})

	// The name is echoed back as typed, and matched case-insensitively.
	t.Run("unknown door", func(t *testing.T) {
		r := env.sub(t).runCmd(cmd, env.caller, "", "Tetris\rQ\r")
		if !r.has("Door 'Tetris' not found. Use ? to list available doors.") {
			t.Errorf("output:\n%s", r.text())
		}
	})

	t.Run("door above the caller's level", func(t *testing.T) {
		r := env.sub(t).runCmd(cmd, env.caller, "", "secret\rQ\r")
		if !r.has("Access denied: you do not have permission to run 'SECRET'.") {
			t.Errorf("output:\n%s", r.text())
		}
		if r.has("Sysop Tools", "/bin/true") {
			t.Errorf("restricted door's details shown:\n%s", r.text())
		}
	})

	t.Run("disconnect at the prompt", func(t *testing.T) {
		r := env.sub(t).runCmd(cmd, env.caller, "", "LOR")
		if r.next != "LOGOFF" || r.user != nil {
			t.Errorf("next=%q user=%v, want a logoff", r.next, r.user)
		}
	})
}

func TestDoorcovOpenDoorPrompt(t *testing.T) { doorcovPromptCases(t, "OPENDOOR") }
func TestDoorcovDoorInfoPrompt(t *testing.T) { doorcovPromptCases(t, "DOORINFO") }

func TestDoorcovDoorRunnablesRequireLogin(t *testing.T) {
	env := newMenuEnv(t)
	for cmd, want := range map[string]string{
		"OPENDOOR": "You must be logged in to list doors.",
		"DOORINFO": "You must be logged in to view door info.",
	} {
		r := env.runCmd(cmd, nil, "", "LORD\r")
		if r.user != nil || r.next != "" || !r.has(want) {
			t.Errorf("%s: user=%v next=%q output:\n%s", cmd, r.user, r.next, r.text())
		}
	}
}

func TestDoorcovDoorInfo(t *testing.T) {
	env := newMenuEnv(t)
	env.e.SetDoorRegistry(doorcovRegistry())

	for _, tc := range []struct {
		door string
		want []string
		not  []string
	}{
		{"lord", []string{"Door: LORD", "Type: DOS (dosemu2)", "Commands: START.BAT {NODE}, CLEAN.EXE",
			`Directory: C:\LORD`, "Dropfile: DOOR.SYS (startup)", "Single Instance: Yes"},
			[]string{"Server:", "I/O Mode:", "Min Access:", "Use Shell:", "Cleanup:"}},
		{"NATIVE", []string{"Door: NATIVE", "Type: Native Linux", "Commands: /opt/game/run, -n, {NODE}",
			"Dropfile: DOOR32.SYS (node)", "I/O Mode: SOCKET", "Min Access: 5", "Use Shell: Yes", "Cleanup: /opt/game/tidy"},
			[]string{"Directory:", "Single Instance:", "Server:"}},
		{"HELLO", []string{"Door: HELLO", "Type: VPL Script"}, []string{"Commands:", "Dropfile:"}},
		{"SYNC", []string{"Door: SYNC", "Type: Synchronet JS"}, []string{"Commands:"}},
		{"REMOTE", []string{"Door: REMOTE", "Type: RLogin (remote)", "Server: doors.example.net:2513"}, []string{"Commands:"}},
	} {
		t.Run(tc.door, func(t *testing.T) {
			r := env.sub(t).runCmd("DOORINFO", env.caller, "", tc.door+"\r")
			if r.err != nil || r.user != env.caller || !r.has(tc.want...) {
				t.Errorf("err=%v user=%v, want %q in:\n%s", r.err, r.user, tc.want, r.text())
			}
			for _, n := range tc.not {
				if r.has(n) {
					t.Errorf("%q shown for a door that does not set it:\n%s", n, r.text())
				}
			}
		})
	}
}

// OPENDOOR runs the named door as the caller and returns to the menu.
func TestDoorcovOpenDoorRunsDoor(t *testing.T) {
	doorcovIsolateTemp(t)
	env := newMenuEnv(t)
	env.e.SetDoorRegistry(map[string]config.DoorConfig{"GAME": {
		Commands:       []string{"/bin/sh", "-c", `echo "PLAYER:$0:$1:$2:$3:$LINES:$COLUMNS"`, "{USERHANDLE}", "{REALNAME}", "{LEVEL}", "{TIMELEFT}"},
		SingleInstance: true,
	}})

	// The caller stays connected: a session that ended would be a hang-up,
	// which ends the door and logs the caller off.
	s := newDoorcovSession()
	s.send("game\r")
	r := doorcovRun(env, s, runOpenDoor, env.caller, "")
	if r.err != nil || r.user != env.caller || r.next != "" {
		t.Errorf("err=%v user=%v next=%q", r.err, r.user, r.next)
	}
	// The door gets the session's terminal size, not a stored preference.
	if !r.has("PLAYER:Caller:Carl Caller:10:60:24:80") {
		t.Errorf("door did not run as the caller:\n%s", r.text())
	}
	// The single-instance lock is released once the door exits.
	if err := acquireDoorLock("GAME", 9); err != nil {
		t.Errorf("lock still held after the door exited: %v", err)
	}
	releaseDoorLock("GAME", 9)
}

func TestDoorcovOpenDoorBusy(t *testing.T) {
	doorcovIsolateTemp(t)
	env := newMenuEnv(t)
	marker := filepath.Join(t.TempDir(), "ran")
	env.e.SetDoorRegistry(map[string]config.DoorConfig{"GAME": {Commands: []string{"touch", marker}, SingleInstance: true}})

	// Node 9 is in the door.
	if err := acquireDoorLock("game", 9); err != nil {
		t.Fatalf("acquireDoorLock: %v", err)
	}
	defer releaseDoorLock("game", 9)

	r := doorcovRun(env, newDoorcovScripted("GAME\r"), runOpenDoor, env.caller, "")
	if r.err != nil || r.user != env.caller || !r.has("Door 'GAME' is currently in use by another user.") {
		t.Errorf("err=%v user=%v output:\n%s", r.err, r.user, r.text())
	}

	// A strings.json without the busy message still tells the caller.
	setStringsField(env.e, func(s *config.StringsConfig) { s.DoorBusyFormat = " " })
	r = doorcovRun(env, newDoorcovScripted("GAME\r"), runOpenDoor, env.caller, "")
	if !r.has("Door is currently in use: GAME") {
		t.Errorf("fallback busy message missing:\n%s", r.text())
	}
	if doorcovExists(marker) {
		t.Error("door ran while another node held its lock")
	}
}

// A door that fails to run is reported to the caller, who stays logged in.
func TestDoorcovOpenDoorReportsFailure(t *testing.T) {
	env := newMenuEnv(t)
	env.e.SetDoorRegistry(map[string]config.DoorConfig{"BROKEN": {Commands: []string{"/nonexistent/door"}}})

	r := doorcovRun(env, newDoorcovScripted("broken\r"), runOpenDoor, env.caller, "")
	if r.err != nil || r.user != env.caller || r.next != "" {
		t.Errorf("err=%v user=%v next=%q", r.err, r.user, r.next)
	}
	if !r.has("Error running door 'BROKEN':", "/nonexistent/door", "Press Enter to continue...") {
		t.Errorf("failure not reported:\n%s", r.text())
	}
}

// executeDoor picks the executor from the door's type; each of these fails
// in a way only its own executor does.
func TestDoorcovExecuteDoorDispatch(t *testing.T) {
	env := newMenuEnv(t)
	setServerField(env.e, func(c *config.ServerConfig) { c.DosemuPath = "/nonexistent/dosemu2.bin" })

	for _, tc := range []struct {
		name string
		cfg  config.DoorConfig
		want string
	}{
		{"synchronet_js", config.DoorConfig{Type: "synchronet_js"}, "synchronet_js door 'TESTDOOR' has no script configured"},
		{"v3_script", config.DoorConfig{Type: "v3_script"}, "v3_script door 'TESTDOOR' has no script configured"},
		{"rlogin", config.DoorConfig{Type: "rlogin"}, `door "TESTDOOR" has no host configured`},
		{"telnet", config.DoorConfig{Type: "telnet"}, `door "TESTDOOR" has no host configured`},
		{"dos", config.DoorConfig{IsDOS: true, DriveCPath: t.TempDir()}, "failed to start dosemu2 with pty"},
		{"native", config.DoorConfig{}, `door "TESTDOOR" has no command configured`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newDoorcovSession()
			err := doorcovExec(t, s, executeDoor, doorcovCtx(env, s, tc.cfg))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestDoorcovExecuteDoorSingleInstance(t *testing.T) {
	tmp := doorcovIsolateTemp(t)
	env := newMenuEnv(t)
	cfg := config.DoorConfig{Commands: []string{"echo", "played"}, SingleInstance: true}

	t.Run("busy", func(t *testing.T) {
		if err := acquireDoorLock("TESTDOOR", 9); err != nil {
			t.Fatalf("acquireDoorLock: %v", err)
		}
		defer releaseDoorLock("TESTDOOR", 9)

		s := newDoorcovScripted("")
		err := doorcovExec(t, s, executeDoor, doorcovCtx(env, s, cfg))
		if !errors.Is(err, ErrDoorBusy) || !strings.Contains(err.Error(), "TESTDOOR") {
			t.Errorf("err = %v, want ErrDoorBusy naming the door", err)
		}
		if s.output() != "" {
			t.Errorf("door ran while locked: %q", s.output())
		}
	})

	t.Run("free", func(t *testing.T) {
		s := newDoorcovSession() // still connected: see TestDoorcovOpenDoorRunsDoor
		if err := doorcovExec(t, s, executeDoor, doorcovCtx(env, s, cfg)); err != nil {
			t.Fatalf("executeDoor: %v", err)
		}
		if s.output() != "played\n" {
			t.Errorf("output = %q, want the door's", s.output())
		}
		// The lock file records who held it, for a sysop chasing a stale lock.
		b, err := os.ReadFile(filepath.Join(tmp, "vision3_doorlocks", "TESTDOOR.lock"))
		if err != nil || !strings.HasPrefix(string(b), "node=3\npid=") {
			t.Errorf("lock file = %q, err %v", b, err)
		}
	})

	// Anything but "busy" is a lock failure, reported as such rather than
	// as the door being in use.
	t.Run("lock directory unusable", func(t *testing.T) {
		file := filepath.Join(t.TempDir(), "not-a-dir")
		if err := os.WriteFile(file, nil, 0o644); err != nil {
			t.Fatal(err)
		}
		t.Setenv("TMPDIR", file)

		s := newDoorcovScripted("")
		err := doorcovExec(t, s, executeDoor, doorcovCtx(env, s, cfg))
		if err == nil || errors.Is(err, ErrDoorBusy) || !strings.Contains(err.Error(), "failed to acquire door lock") {
			t.Errorf("err = %v, want a lock failure that is not ErrDoorBusy", err)
		}
		if s.output() != "" {
			t.Errorf("door ran without its lock: %q", s.output())
		}
	})
}

// The cleanup command runs in the door's working directory with placeholders
// filled in, and only when one is configured.
func TestDoorcovRunDoorCleanup(t *testing.T) {
	env := newMenuEnv(t)
	wd := t.TempDir()
	s := newDoorcovSession()

	ctx := doorcovCtx(env, s, config.DoorConfig{WorkingDirectory: wd, CleanupArgs: []string{"-c", "touch ran"}})
	runDoorCleanup(ctx)
	if doorcovExists(filepath.Join(wd, "ran")) {
		t.Fatal("cleanup ran with no cleanup command configured")
	}

	ctx.Config.CleanupCommand = "/bin/sh"
	ctx.Config.CleanupArgs = []string{"-c", `echo "$0|$1" > ran`, "{USERHANDLE}@{USERIP}", "{STARTUPDIR}"}
	runDoorCleanup(ctx)
	got, err := os.ReadFile(filepath.Join(wd, "ran"))
	if want := "Neo@203.0.113.7|" + wd + "\n"; err != nil || string(got) != want {
		t.Errorf("cleanup wrote %q, err %v; want %q", got, err, want)
	}
}
