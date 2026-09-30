//go:build !windows

package menu

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/gliderlabs/ssh"
)

// doorcovHas fails the test unless the door's output contains every one of
// want.
func doorcovHas(t *testing.T, s *doorcovSession, want ...string) {
	t.Helper()
	out := s.output()
	for _, w := range want {
		if !strings.Contains(out, w) {
			t.Errorf("door output lacks %q:\n%s", w, out)
		}
	}
}

// The plain path: no PTY, the door's stdin/stdout are the session. Checks
// everything the door is handed: substituted arguments, the environment, the
// working directory and a dropfile that exists for exactly as long as it runs.
func TestDoorcovNativeDoorStdio(t *testing.T) {
	env := newMenuEnv(t)
	wd := t.TempDir()
	script := doorcovScript(t, `
echo "ARGS:$1:$2:$4"
echo "DROP:$3"
echo "ENV:$BBS_USERHANDLE:$BBS_USERID:$BBS_NODE:$BBS_TIMELEFT:$BBS_USERIP:$LINES:$COLUMNS"
echo "GREETING:$DOOR_GREETING"
echo "CWD:$(pwd -P)"
echo "NAME:$(sed -n 10p "$3")"
read reply
echo "REPLY:$reply"`)

	s := newDoorcovScripted("hello door\n")
	ctx := doorcovCtx(env, s, config.DoorConfig{
		Commands:         []string{"/bin/sh", script, "{USERHANDLE}", "{NODE}", "{DROPFILE}", "{NODEDIR}"},
		WorkingDirectory: wd,
		DropfileType:     "door.sys",
		DropfileCase:     "lower",
		// A door's own BBS_* setting wins over the standard one.
		EnvironmentVars: map[string]string{"DOOR_GREETING": "hi {REALNAME}", "BBS_NODE": "node-{NODE}"},
	})
	// An inherited size must not leak through to the door.
	t.Setenv("LINES", "99")
	t.Setenv("COLUMNS", "99")

	if err := doorcovExec(t, s, executeNativeDoor, ctx); err != nil {
		t.Fatalf("executeNativeDoor: %v", err)
	}

	dropfile := filepath.Join(wd, "door.sys")
	realWD, err := filepath.EvalSymlinks(wd)
	if err != nil {
		t.Fatal(err)
	}
	doorcovHas(t, s,
		"ARGS:Neo:3:"+wd+"\n",
		"DROP:"+dropfile+"\n",
		"ENV:Neo:7:node-3:60:203.0.113.7:37:132\n",
		"GREETING:hi Thomas Anderson\n",
		"CWD:"+realWD+"\n",
		"NAME:Thomas Anderson",
		"REPLY:hello door\n",
	)
	if doorcovExists(dropfile) {
		t.Error("dropfile left behind after the door exited")
	}
}

// With dropfile_location "node" every dropfile type lands in a per-node temp
// directory that is removed, dropfile and all, when the door exits.
func TestDoorcovNativeDoorNodeDropfiles(t *testing.T) {
	tmp := doorcovIsolateTemp(t)
	env := newMenuEnv(t)
	script := doorcovScript(t, `echo "NODEDIR:$1"; echo "DROP:$2"; echo "FILES:$(ls "$1")"; echo "LINES:$(grep -c . "$2")"`)

	for name, wantLines := range map[string]string{"DOOR.SYS": "52", "DOOR32.SYS": "11", "DORINFO1.DEF": "13", "CHAIN.TXT": "30"} {
		t.Run(name, func(t *testing.T) {
			s := newDoorcovScripted("")
			ctx := doorcovCtx(env, s, config.DoorConfig{
				Commands:         []string{"/bin/sh", script, "{NODEDIR}", "{DROPFILE}"},
				DropfileType:     name,
				DropfileLocation: "Node",
			})
			if err := doorcovExec(t, s, executeNativeDoor, ctx); err != nil {
				t.Fatalf("executeNativeDoor: %v", err)
			}

			nodeDir := ctx.Subs["{NODEDIR}"]
			if !strings.HasPrefix(nodeDir, filepath.Join(tmp, "vision3_node3_")) {
				t.Errorf("node dir = %q, want a vision3_node3_ directory under %s", nodeDir, tmp)
			}
			doorcovHas(t, s, "NODEDIR:"+nodeDir+"\n", "DROP:"+filepath.Join(nodeDir, name)+"\n",
				"FILES:"+name+"\n", "LINES:"+wantLines+"\n")
			if doorcovExists(nodeDir) {
				t.Errorf("node dir %s left behind", nodeDir)
			}
		})
	}
}

// A door with no recognised dropfile type gets none: {DROPFILE} is empty and
// {NODEDIR} is the working directory.
func TestDoorcovNativeDoorNoDropfile(t *testing.T) {
	env := newMenuEnv(t)
	wd := t.TempDir()
	s := newDoorcovScripted("")
	ctx := doorcovCtx(env, s, config.DoorConfig{
		Commands:         []string{"/bin/sh", "-c", `echo "DROP:[$0] NODEDIR:[$1]"; ls -A`, "{DROPFILE}", "{NODEDIR}"},
		WorkingDirectory: wd,
		DropfileType:     "NONE",
	})
	if err := doorcovExec(t, s, executeNativeDoor, ctx); err != nil {
		t.Fatalf("executeNativeDoor: %v", err)
	}
	if got, want := s.output(), "DROP:[] NODEDIR:["+wd+"]\n"; got != want {
		t.Errorf("output = %q, want %q", got, want)
	}
}

// use_shell runs the command through /bin/sh without letting the shell
// reinterpret the arguments: a substituted value is one argument, verbatim.
func TestDoorcovNativeDoorUseShell(t *testing.T) {
	env := newMenuEnv(t)
	s := newDoorcovScripted("")
	ctx := doorcovCtx(env, s, config.DoorConfig{
		Commands: []string{"printf", `[%s]`, "{REALNAME}", "$HOME; echo pwned", "`id`"},
		UseShell: true,
	})
	if err := doorcovExec(t, s, executeNativeDoor, ctx); err != nil {
		t.Fatalf("executeNativeDoor: %v", err)
	}
	if got, want := s.output(), "[Thomas Anderson][$HOME; echo pwned][`id`]"; got != want {
		t.Errorf("output = %q, want %q", got, want)
	}
}

// The door's exit status is the caller's error, and the cleanup command runs
// regardless, while the dropfile is still there for it to collect.
func TestDoorcovNativeDoorExitStatusAndCleanup(t *testing.T) {
	env := newMenuEnv(t)
	wd := t.TempDir()
	s := newDoorcovScripted("")
	ctx := doorcovCtx(env, s, config.DoorConfig{
		Commands:         []string{"/bin/sh", "-c", "exit 7"},
		WorkingDirectory: wd,
		DropfileType:     "DOOR32.SYS",
		CleanupCommand:   "/bin/sh",
		CleanupArgs:      []string{"-c", `cp "$0" saved.sys && echo "$1" > who`, "{DROPFILE}", "{USERHANDLE} on {NODE}"},
	})

	err := doorcovExec(t, s, executeNativeDoor, ctx)
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 7 {
		t.Fatalf("err = %v, want exit status 7", err)
	}
	if who, err := os.ReadFile(filepath.Join(wd, "who")); err != nil || string(who) != "Neo on 3\n" {
		t.Errorf("cleanup marker = %q, err %v; want %q", who, err, "Neo on 3\n")
	}
	if got := doorcovLines(t, filepath.Join(wd, "saved.sys")); len(got) != 11 || got[6] != "Neo" {
		t.Errorf("cleanup did not see the dropfile: copied %q", got)
	}
	if doorcovExists(filepath.Join(wd, "DOOR32.SYS")) {
		t.Error("dropfile left behind after cleanup")
	}
}

// A cleanup command that fails is logged, not reported: it must not turn a
// door that ran fine into an error.
func TestDoorcovCleanupFailureDoesNotMaskDoorResult(t *testing.T) {
	env := newMenuEnv(t)
	for _, cleanup := range []string{"false", "/nonexistent/cleanup"} {
		s := newDoorcovScripted("")
		ctx := doorcovCtx(env, s, config.DoorConfig{
			Commands:       []string{"echo", "played"},
			CleanupCommand: cleanup,
		})
		if err := doorcovExec(t, s, executeNativeDoor, ctx); err != nil {
			t.Errorf("cleanup %q: executeNativeDoor = %v, want nil", cleanup, err)
		}
		doorcovHas(t, s, "played")
	}
}

func TestDoorcovNativeDoorConfigErrors(t *testing.T) {
	env := newMenuEnv(t)

	for name, cmds := range map[string][]string{"no commands": nil, "blank command": {"", "arg"}} {
		t.Run(name, func(t *testing.T) {
			s := newDoorcovScripted("")
			err := doorcovExec(t, s, executeNativeDoor, doorcovCtx(env, s, config.DoorConfig{Commands: cmds}))
			if err == nil || !strings.Contains(err.Error(), `door "TESTDOOR" has no command configured`) {
				t.Errorf("err = %v, want a no-command error", err)
			}
		})
	}

	t.Run("command not found", func(t *testing.T) {
		s := newDoorcovScripted("")
		err := doorcovExec(t, s, executeNativeDoor, doorcovCtx(env, s, config.DoorConfig{
			Commands:            []string{"/nonexistent/door"},
			RequiresRawTerminal: true, // no PTY on this session: falls back to plain I/O
		}))
		if !errors.Is(err, os.ErrNotExist) {
			t.Errorf("err = %v, want not-exist", err)
		}
	})

	t.Run("node directory cannot be created", func(t *testing.T) {
		marker := filepath.Join(t.TempDir(), "ran")
		t.Setenv("TMPDIR", filepath.Join(t.TempDir(), "missing"))
		s := newDoorcovScripted("")
		err := doorcovExec(t, s, executeNativeDoor, doorcovCtx(env, s, config.DoorConfig{
			Commands:         []string{"touch", marker},
			DropfileLocation: "node",
		}))
		if !errors.Is(err, os.ErrNotExist) || !strings.Contains(err.Error(), "failed to create node dropfile directory") {
			t.Errorf("err = %v, want a node directory failure", err)
		}
		if doorcovExists(marker) {
			t.Error("door ran although its node directory could not be created")
		}
	})

	// The dropfile cannot be written, so the door is never started and the
	// caller is told why.
	t.Run("dropfile cannot be written", func(t *testing.T) {
		marker := filepath.Join(t.TempDir(), "ran")
		s := newDoorcovScripted("")
		err := doorcovExec(t, s, executeNativeDoor, doorcovCtx(env, s, config.DoorConfig{
			Commands:         []string{"touch", marker},
			WorkingDirectory: filepath.Join(t.TempDir(), "missing"),
			DropfileType:     "CHAIN.TXT",
		}))
		if !errors.Is(err, os.ErrNotExist) {
			t.Errorf("err = %v, want not-exist", err)
		}
		doorcovHas(t, s, "Error creating system file for door 'TESTDOOR'.")
		if doorcovExists(marker) {
			t.Error("door ran although its dropfile could not be written")
		}
	})
}

// doorcovReply reads the file a door script recorded the caller's reply in.
// The scripts report what they read through a file rather than by printing
// it, so the check does not also depend on the door's final output being
// relayed (TestDoorFinalOutputAlwaysDelivered covers that).
func doorcovReply(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("door never recorded a reply: %v", err)
	}
	return string(b)
}

// A raw-terminal door on a PTY session gets a PTY sized from the caller's
// saved screen size, and the session is bridged to it in both directions.
func TestDoorcovNativeDoorPTY(t *testing.T) {
	env := newMenuEnv(t)
	reply := filepath.Join(t.TempDir(), "reply")
	script := doorcovScript(t, `
echo "SIZE:$(stty size):$LINES:$COLUMNS"
if [ -t 0 ]; then echo "TTY:yes"; fi
printf 'NAME?'
read name
echo "$name" > "$1"`)

	s := newDoorcovSession()
	s.isPty = true
	// A client resize during the door is drained and ignored.
	s.winCh <- ssh.Window{Width: 40, Height: 10}
	s.whenOutput("NAME?", func() { s.send("trinity\n") })
	ctx := doorcovCtx(env, s, config.DoorConfig{
		Commands:            []string{"/bin/sh", script, reply},
		RequiresRawTerminal: true,
	})

	if err := doorcovExec(t, s, executeNativeDoor, ctx); err != nil {
		t.Fatalf("executeNativeDoor: %v", err)
	}
	doorcovHas(t, s, "SIZE:37 132:37:132", "TTY:yes")
	if got := doorcovReply(t, reply); got != "trinity\n" {
		t.Errorf("door read %q, want %q", got, "trinity\n")
	}
}

// A caller with no saved screen size, or an impossible one, gets 80x25.
func TestDoorcovNativeDoorPTYDefaultSize(t *testing.T) {
	env := newMenuEnv(t)
	for _, size := range [][2]int{{0, 0}, {70000, 70000}} {
		s := newDoorcovSession()
		s.isPty = true
		s.whenOutput("KEY?", func() { s.send("\n") })
		ctx := doorcovCtx(env, s, config.DoorConfig{
			Commands:            []string{"/bin/sh", "-c", `echo "SIZE:$(stty size) KEY?"; read key`},
			RequiresRawTerminal: true,
		})
		ctx.User.ScreenWidth, ctx.User.ScreenHeight = size[0], size[1]

		if err := doorcovExec(t, s, executeNativeDoor, ctx); err != nil {
			t.Fatalf("size %v: executeNativeDoor: %v", size, err)
		}
		doorcovHas(t, s, "SIZE:25 80 KEY?")
	}
}

func TestDoorcovNativeDoorPTYStartFailure(t *testing.T) {
	env := newMenuEnv(t)
	s := newDoorcovSession()
	s.isPty = true
	err := doorcovExec(t, s, executeNativeDoor, doorcovCtx(env, s, config.DoorConfig{
		Commands:            []string{"/nonexistent/door"},
		RequiresRawTerminal: true,
	}))
	if err == nil || !strings.Contains(err.Error(), "failed to start pty for door 'TESTDOOR'") || !errors.Is(err, os.ErrNotExist) {
		t.Errorf("err = %v, want a PTY start failure wrapping not-exist", err)
	}
}

// io_mode SOCKET hands the door one end of a socket pair as descriptor 3 and
// bridges the other end to the session.
func TestDoorcovNativeDoorSocket(t *testing.T) {
	env := newMenuEnv(t)
	reply := filepath.Join(t.TempDir(), "reply")
	script := doorcovScript(t, `
echo "FD:$DOOR_SOCKET_FD NAME?" >&3
read name <&3
echo "$name" > "$1"
exit 4`)

	s := newDoorcovSession()
	s.whenOutput("NAME?", func() { s.send("morpheus\n") })
	ctx := doorcovCtx(env, s, config.DoorConfig{
		Commands: []string{"/bin/sh", script, reply},
		IOMode:   "socket",
	})

	err := doorcovExec(t, s, executeNativeDoor, ctx)
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 4 {
		t.Errorf("err = %v, want exit status 4", err)
	}
	doorcovHas(t, s, "FD:3 NAME?\n")
	if got := doorcovReply(t, reply); got != "morpheus\n" {
		t.Errorf("door read %q, want %q", got, "morpheus\n")
	}
}

func TestDoorcovNativeDoorSocketStartFailure(t *testing.T) {
	env := newMenuEnv(t)
	s := newDoorcovSession()
	err := doorcovExec(t, s, executeNativeDoor, doorcovCtx(env, s, config.DoorConfig{
		Commands: []string{"/nonexistent/door"},
		IOMode:   "SOCKET",
	}))
	if err == nil || !strings.Contains(err.Error(), "failed to start door 'TESTDOOR' with socket I/O") || !errors.Is(err, os.ErrNotExist) {
		t.Errorf("err = %v, want a socket start failure wrapping not-exist", err)
	}
}
