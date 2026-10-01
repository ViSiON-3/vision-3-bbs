//go:build !windows

package menu

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/editor"
)

// doorIdleTestTimeout is the idle timeout the tests below give the caller.
const doorIdleTestTimeout = 300 * time.Millisecond

// idleDoorCtx is doorcovCtx for a caller whose idle timeout is
// doorIdleTestTimeout.
func idleDoorCtx(t *testing.T, s *doorcovSession, doorCfg config.DoorConfig) *DoorCtx {
	t.Helper()
	ctx := doorcovCtx(newMenuEnv(t), s, doorCfg)
	ctx.IdleTimeout = doorIdleTestTimeout
	return ctx
}

// pidGone reports whether the process named in pidFile has gone within a
// few seconds. A process that has died stays visible as a zombie until it is
// reaped, so this waits rather than checking once.
func pidGone(t *testing.T, pidFile string) bool {
	t.Helper()
	var data []byte
	for range 50 { // the door writes the file as it starts
		var err error
		if data, err = os.ReadFile(pidFile); err == nil && len(strings.TrimSpace(string(data))) > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		t.Fatalf("read pid file: %q, %v", data, err)
	}
	for range 100 {
		if syscall.Kill(pid, 0) != nil {
			return true
		}
		stat, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
		if err == nil && strings.Contains(string(stat), ") Z ") {
			return true // dead, waiting to be reaped
		}
		time.Sleep(30 * time.Millisecond)
	}
	return false
}

// A door whose caller sends nothing is ended, on every way a native door can
// be run, and executeDoor reports the idle timeout.
func TestDoorIdleEndsNativeDoor(t *testing.T) {
	tests := []struct {
		name  string
		cfg   config.DoorConfig
		isPty bool
	}{
		{"stdio", config.DoorConfig{}, false},
		{"socket", config.DoorConfig{IOMode: "SOCKET"}, false},
		{"pty", config.DoorConfig{RequiresRawTerminal: true}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doorcovIsolateTemp(t)
			// The door waits for input that never comes; its background
			// child shows the hang-up reaches its whole process group.
			pidFile := filepath.Join(t.TempDir(), "child.pid")
			script := doorcovScript(t, `sleep 30 & echo $! > "$1"; cat <&0 >/dev/null; cat <&3 >/dev/null; wait`)
			s := newDoorcovSession()
			s.isPty = tt.isPty
			cfg := tt.cfg
			cfg.Commands = []string{"/bin/sh", script, pidFile}
			ctx := idleDoorCtx(t, s, cfg)

			start := time.Now()
			err := doorcovExec(t, s, executeDoor, ctx)
			if !errors.Is(err, editor.ErrIdleTimeout) {
				t.Fatalf("err = %v, want editor.ErrIdleTimeout", err)
			}
			if took := time.Since(start); took < doorIdleTestTimeout {
				t.Errorf("door ended after %v, before the idle timeout", took)
			}
			if !pidGone(t, pidFile) {
				t.Error("the door's child process survived the hang-up")
			}
		})
	}
}

// Every key the caller presses restarts the countdown.
func TestDoorIdleInputKeepsDoorRunning(t *testing.T) {
	doorcovIsolateTemp(t)
	script := doorcovScript(t, `while read line; do echo "GOT $line"; done`)
	s := newDoorcovSession()
	ctx := idleDoorCtx(t, s, config.DoorConfig{Commands: []string{"/bin/sh", script}})

	const typing = 4 * doorIdleTestTimeout
	go func() {
		deadline := time.Now().Add(typing)
		for time.Now().Before(deadline) {
			s.send("k\n")
			time.Sleep(doorIdleTestTimeout / 3)
		}
	}()

	start := time.Now()
	err := doorcovExec(t, s, executeDoor, ctx)
	if !errors.Is(err, editor.ErrIdleTimeout) {
		t.Fatalf("err = %v, want editor.ErrIdleTimeout once typing stopped", err)
	}
	if took := time.Since(start); took < typing {
		t.Errorf("door ended after %v, while the caller was still typing (until %v)", took, typing)
	}
	doorcovHas(t, s, "GOT k")
}

// A door that ignores SIGHUP is killed once the grace period is over.
func TestDoorIdleKillsDoorIgnoringHangup(t *testing.T) {
	doorcovIsolateTemp(t)
	old := doorHangupGrace
	doorHangupGrace = 200 * time.Millisecond
	t.Cleanup(func() { doorHangupGrace = old })

	script := doorcovScript(t, `trap '' HUP; while :; do sleep 0.1; done`)
	s := newDoorcovSession()
	ctx := idleDoorCtx(t, s, config.DoorConfig{Commands: []string{"/bin/sh", script}})
	if err := doorcovExec(t, s, executeDoor, ctx); !errors.Is(err, editor.ErrIdleTimeout) {
		t.Errorf("err = %v, want editor.ErrIdleTimeout", err)
	}
}

// A door that finishes in time is unaffected, and a caller exempt from the
// idle timeout is never hung up on.
func TestDoorIdleLeavesOtherDoorsAlone(t *testing.T) {
	doorcovIsolateTemp(t)
	quick := doorcovScript(t, "echo DONE")
	s := newDoorcovSession()
	ctx := idleDoorCtx(t, s, config.DoorConfig{Commands: []string{"/bin/sh", quick}})
	if err := doorcovExec(t, s, executeDoor, ctx); err != nil {
		t.Errorf("door that finished in time: err = %v", err)
	}
	doorcovHas(t, s, "DONE")

	slow := doorcovScript(t, "sleep 1; echo EXEMPT-DONE")
	s = newDoorcovSession()
	ctx = idleDoorCtx(t, s, config.DoorConfig{Commands: []string{"/bin/sh", slow}})
	ctx.IdleTimeout = 0
	if err := doorcovExec(t, s, executeDoor, ctx); err != nil {
		t.Errorf("exempt caller: err = %v", err)
	}
	doorcovHas(t, s, "EXEMPT-DONE")
}

// Through the menu command, an idle caller is logged off with the idle error,
// which the menu loop turns into the idle timeout notice.
func TestDoorIdleLogsCallerOff(t *testing.T) {
	doorcovIsolateTemp(t)
	old := sessionIdleUnit
	sessionIdleUnit = doorIdleTestTimeout
	t.Cleanup(func() { sessionIdleUnit = old })

	env := newMenuEnv(t)
	cfg := env.e.GetServerConfig()
	cfg.SessionIdleTimeoutMinutes = 1 // one doorIdleTestTimeout
	cfg.CoSysOpLevel, cfg.SysOpLevel = 250, 255
	env.e.SetServerConfig(cfg)
	env.e.SetDoorRegistry(map[string]config.DoorConfig{"GAME": {
		Commands: []string{"/bin/sh", "-c", "cat >/dev/null"},
	}})

	s := newDoorcovSession()
	s.send("game\r")
	r := doorcovRun(env, s, runOpenDoor, env.caller, "")
	if !errors.Is(r.err, editor.ErrIdleTimeout) || r.next != "LOGOFF" {
		t.Errorf("err=%v next=%q, want editor.ErrIdleTimeout and LOGOFF", r.err, r.next)
	}
}

// A child that ignores SIGHUP is killed with the rest of the door's process
// group, even after the door itself has exited.
func TestDoorIdleKillsChildIgnoringHangup(t *testing.T) {
	doorcovIsolateTemp(t)
	old := doorHangupGrace
	doorHangupGrace = 200 * time.Millisecond
	t.Cleanup(func() { doorHangupGrace = old })

	pidFile := filepath.Join(t.TempDir(), "child.pid")
	script := doorcovScript(t, `(trap '' HUP; sleep 30) & echo $! > "$1"; cat >/dev/null`)
	s := newDoorcovSession()
	ctx := idleDoorCtx(t, s, config.DoorConfig{Commands: []string{"/bin/sh", script, pidFile}})
	if err := doorcovExec(t, s, executeDoor, ctx); !errors.Is(err, editor.ErrIdleTimeout) {
		t.Fatalf("err = %v, want editor.ErrIdleTimeout", err)
	}
	if !pidGone(t, pidFile) {
		t.Error("a child that ignored SIGHUP outlived the hang-up")
	}
}

// Time spent in the cleanup command after the door has exited is not the
// caller being idle.
func TestDoorIdleIgnoresCleanupTime(t *testing.T) {
	doorcovIsolateTemp(t)
	s := newDoorcovSession()
	ctx := idleDoorCtx(t, s, config.DoorConfig{
		Commands:       []string{"/bin/sh", "-c", "echo DONE"},
		CleanupCommand: "sleep",
		CleanupArgs:    []string{"1"},
	})
	if err := doorcovExec(t, s, executeDoor, ctx); err != nil {
		t.Errorf("err = %v; a slow cleanup after the door exited must not count as idle", err)
	}
}
