//go:build !windows

package menu

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
)

// doorExitModes are the ways executeNativeDoor connects a door to the caller,
// each with a door command that prints msg and exits at once.
func doorExitModes(msg string) map[string]struct {
	cfg   config.DoorConfig
	isPty bool
} {
	return map[string]struct {
		cfg   config.DoorConfig
		isPty bool
	}{
		"stdio":  {cfg: config.DoorConfig{Commands: []string{"/bin/sh", "-c", "echo " + msg}}},
		"pty":    {cfg: config.DoorConfig{Commands: []string{"/bin/sh", "-c", "echo " + msg}, RequiresRawTerminal: true}, isPty: true},
		"socket": {cfg: config.DoorConfig{Commands: []string{"/bin/sh", "-c", "echo " + msg + " >&3"}, IOMode: "socket"}},
	}
}

// A door that prints a line and exits at once must always deliver the line:
// the PTY or socket is closed only after the output copier has drained it.
// Each mode runs the door many times, as the loss was a race.
func TestDoorFinalOutputAlwaysDelivered(t *testing.T) {
	env := newMenuEnv(t)
	for name, mode := range doorExitModes("GOODBYE") {
		t.Run(name, func(t *testing.T) {
			for i := 0; i < 25; i++ {
				s := newDoorcovScripted("")
				s.isPty = mode.isPty
				if err := doorcovExec(t, s, executeNativeDoor, doorcovCtx(env, s, mode.cfg)); err != nil {
					t.Fatalf("run %d: executeNativeDoor: %v", i, err)
				}
				if !strings.Contains(s.output(), "GOODBYE") {
					t.Fatalf("run %d: the door's last line was dropped; output %q", i, s.output())
				}
			}
		})
	}
}

// The same for a DOS door: what the emulator writes as it exits reaches the
// caller.
func TestDoorDOSFinalOutputAlwaysDelivered(t *testing.T) {
	env := newMenuEnv(t)
	t.Setenv("HOME", t.TempDir())
	doorcovFakeDosemu(t, env, `printf '\033[2JGOODBYE\n'`)
	for i := 0; i < 25; i++ {
		s := newDoorcovScripted("")
		ctx := doorcovCtx(env, s, config.DoorConfig{IsDOS: true, DriveCPath: t.TempDir()})
		if err := doorcovExec(t, s, executeDOSDoor, ctx); err != nil {
			t.Fatalf("run %d: executeDOSDoor: %v", i, err)
		}
		if !strings.Contains(s.output(), "GOODBYE") {
			t.Fatalf("run %d: the door's last line was dropped; output %q", i, s.output())
		}
	}
}

// doorcovKillOnCleanup kills, when the test ends, the process whose ID a door
// wrote to pidFile.
func doorcovKillOnCleanup(t *testing.T, pidFile string) {
	t.Cleanup(func() {
		b, err := os.ReadFile(pidFile)
		if err != nil {
			return
		}
		if pid, err := strconv.Atoi(strings.TrimSpace(string(b))); err == nil && pid > 0 {
			_ = syscall.Kill(pid, syscall.SIGKILL)
		}
	})
}

// A door that leaves a background child holding its terminal, socket or
// output open cannot hang the node: the caller gets the door's output and is
// back at the menu once the drain times out.
func TestDoorBackgroundChildDoesNotHangNode(t *testing.T) {
	old := doorOutputDrainTimeout
	doorOutputDrainTimeout = 200 * time.Millisecond
	t.Cleanup(func() { doorOutputDrainTimeout = old })

	env := newMenuEnv(t)
	for name, mode := range doorExitModes("BYE") {
		t.Run(name, func(t *testing.T) {
			pidFile := filepath.Join(t.TempDir(), "child.pid")
			doorcovKillOnCleanup(t, pidFile)
			cfg := mode.cfg
			// The child inherits the door's stdin, stdout, stderr and fd 3. It
			// inherits the ignored SIGHUP too, so it outlives the door's PTY
			// session.
			cfg.Commands = []string{"/bin/sh", "-c", `trap '' HUP; sleep 30 & echo $! > "$0"; ` + cfg.Commands[2], pidFile}

			s := newDoorcovSession()
			s.isPty = mode.isPty
			start := time.Now()
			if err := doorcovExec(t, s, executeNativeDoor, doorcovCtx(env, s, cfg)); err != nil {
				t.Fatalf("executeNativeDoor: %v", err)
			}
			if took := time.Since(start); took > 5*time.Second {
				t.Errorf("door took %v to return with a child holding its output", took)
			}
			doorcovHas(t, s, "BYE")
		})
	}
}

// A stdio door returns to the menu as soon as it exits, without waiting for
// the caller to press a key, and the caller's next key is left for the menu.
func TestDoorStdioReturnsWithoutInput(t *testing.T) {
	env := newMenuEnv(t)
	s := newDoorcovSession() // input stays open and empty
	ctx := doorcovCtx(env, s, config.DoorConfig{Commands: []string{"echo", "played"}})

	if err := doorcovExec(t, s, executeNativeDoor, ctx); err != nil {
		t.Fatalf("executeNativeDoor: %v", err)
	}
	doorcovHas(t, s, "played")

	s.send("x")
	buf := make([]byte, 8)
	n, err := s.Read(buf)
	if err != nil || string(buf[:n]) != "x" {
		t.Errorf("menu read %q, %v after the door; want the caller's key %q", buf[:n], err, "x")
	}
}

// A stdio door still gets the caller's typing, and end of file when the
// caller's input ends.
func TestDoorStdioInputReachesDoor(t *testing.T) {
	env := newMenuEnv(t)
	s := newDoorcovScripted("one\ntwo\n")
	ctx := doorcovCtx(env, s, config.DoorConfig{Commands: []string{"/bin/sh", "-c", `echo "GOT:$(cat | tr '\n' ',')"`}})

	if err := doorcovExec(t, s, executeNativeDoor, ctx); err != nil {
		t.Fatalf("executeNativeDoor: %v", err)
	}
	doorcovHas(t, s, "GOT:one,two,\n")
}

// A door with a dropfile but no working directory gets it in a per-node temp
// directory, not the BBS's own current directory, and the directory is gone
// when the door exits.
func TestDoorDefaultDropfileLocationIsPerNode(t *testing.T) {
	tmp := doorcovIsolateTemp(t)
	bbsDir := t.TempDir()
	t.Chdir(bbsDir)
	env := newMenuEnv(t)

	for _, loc := range []string{"", "startup"} {
		t.Run("location "+strconv.Quote(loc), func(t *testing.T) {
			s := newDoorcovScripted("")
			ctx := doorcovCtx(env, s, config.DoorConfig{
				Commands:         []string{"/bin/sh", "-c", `echo "NODEDIR:$0"; echo "FOUND:$(ls "$1")"`, "{NODEDIR}", "{DROPFILE}"},
				DropfileType:     "DOOR.SYS",
				DropfileLocation: loc,
			})
			if err := doorcovExec(t, s, executeNativeDoor, ctx); err != nil {
				t.Fatalf("executeNativeDoor: %v", err)
			}

			nodeDir := ctx.Subs["{NODEDIR}"]
			if !strings.HasPrefix(nodeDir, filepath.Join(tmp, "vision3_node3_")) {
				t.Errorf("node dir = %q, want a vision3_node3_ directory under %s", nodeDir, tmp)
			}
			if want := filepath.Join(nodeDir, "DOOR.SYS"); ctx.Subs["{DROPFILE}"] != want {
				t.Errorf("{DROPFILE} = %q, want %q", ctx.Subs["{DROPFILE}"], want)
			}
			doorcovHas(t, s, "NODEDIR:"+nodeDir+"\n", "FOUND:"+filepath.Join(nodeDir, "DOOR.SYS")+"\n")
			if doorcovExists(nodeDir) {
				t.Errorf("node dir %s left behind", nodeDir)
			}
			if doorcovExists(filepath.Join(bbsDir, "DOOR.SYS")) {
				t.Error("dropfile written to the BBS's current directory")
			}
		})
	}
}

// A relative working directory is resolved against the BBS's current
// directory, and {DROPFILE} and {NODEDIR} name it absolutely, so the door,
// which runs inside that directory, still finds its dropfile.
func TestDoorRelativeWorkingDirectoryDropfile(t *testing.T) {
	bbsDir := t.TempDir()
	if err := os.Mkdir(filepath.Join(bbsDir, "doors"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(bbsDir)
	env := newMenuEnv(t)

	s := newDoorcovScripted("")
	ctx := doorcovCtx(env, s, config.DoorConfig{
		Commands:         []string{"/bin/sh", "-c", `test -f "$0" && echo "FOUND:$0"; echo "NODEDIR:$1"`, "{DROPFILE}", "{NODEDIR}"},
		WorkingDirectory: "doors",
		DropfileType:     "DOOR32.SYS",
	})
	if err := doorcovExec(t, s, executeNativeDoor, ctx); err != nil {
		t.Fatalf("executeNativeDoor: %v", err)
	}
	wd := filepath.Join(bbsDir, "doors")
	doorcovHas(t, s, "FOUND:"+filepath.Join(wd, "DOOR32.SYS")+"\n", "NODEDIR:"+wd+"\n")
}
