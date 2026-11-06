//go:build !windows

package menu

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/editor"
)

// scriptDoorSession is a session for running a v3_script door the way a menu
// does. It counts the reads blocked on it, so a test can tell that nothing
// the script started is still reading once the door returns: a reader left
// behind would take the caller's next key from the menu.
type scriptDoorSession struct {
	hookSession
	cancel context.CancelFunc // ends the session's context, as a hang-up does

	mu      sync.Mutex
	reading int
}

func (s *scriptDoorSession) Read(p []byte) (int, error) {
	s.mu.Lock()
	s.reading++
	s.mu.Unlock()
	n, err := s.hookSession.Read(p)
	s.mu.Lock()
	s.reading--
	s.mu.Unlock()
	return n, err
}

// runScriptDoor runs src as a v3_script door from a menu: the menu's input
// handler is handed off, the door runs under executeDoor (and so the door
// watch), and the door's error is returned. setup, if not nil, adjusts the
// door before it starts.
func runScriptDoor(t *testing.T, src string, setup func(*DoorCtx, *scriptDoorSession)) (*scriptDoorSession, error) {
	t.Helper()
	env := newMenuEnv(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "door.js"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	ds := newDoorcovSession()
	sctx, cancel := context.WithCancel(context.Background())
	s := &scriptDoorSession{hookSession: hookSession{doorcovSession: ds, ctx: &hookCtx{Context: sctx}}, cancel: cancel}
	t.Cleanup(func() {
		cancel()
		ds.eof()
		resetSessionIH(s)
		sessionTypeAhead.Delete(s)
		takeEnterTrailer(s)
	})

	// The menu was reading the session before the door, as at a prompt.
	_ = getSessionIH(s)
	handOffSessionIH(s)

	ctx := buildDoorCtx(env.e, s, newDoorcovTerminal(ds),
		7, "Neo", "Thomas Anderson", 50, 60, 12, "Zion", 80, 24,
		3, time.Now(), ansi.OutputModeUTF8,
		config.DoorConfig{Type: "v3_script", Script: "door.js", WorkingDirectory: dir}, "SCRIPT")
	if setup != nil {
		setup(ctx, s)
	}
	return s, doorcovExec(t, ds, executeDoor, ctx)
}

// checkReleased fails the test if the door left a read blocked on the
// session or its read interrupt set.
func (s *scriptDoorSession) checkReleased(t *testing.T) {
	t.Helper()
	s.mu.Lock()
	reading := s.reading
	s.mu.Unlock()
	if reading != 0 {
		t.Errorf("%d read(s) still blocked on the session after the door", reading)
	}
	s.doorcovSession.mu.Lock()
	intr := s.intr
	s.doorcovSession.mu.Unlock()
	if intr != nil {
		t.Error("the door left its read interrupt on the session")
	}
}

// checkMenuReads fails the test unless the menu's next input handler reads
// want (keys the caller typed ahead during the door) and then a key typed
// after the door: the menu owns the session's input again.
func (s *scriptDoorSession) checkMenuReads(t *testing.T, want string) {
	t.Helper()
	ih := getSessionIH(s)
	stop := make(chan struct{})
	timer := time.AfterFunc(2*time.Second, func() { close(stop) })
	defer timer.Stop()
	s.send("m")
	for _, w := range []byte(want + "m") {
		if b, err := ih.ReadByteUntil(stop); err != nil || b != w {
			t.Fatalf("menu read %q, %v; want %q (of %q)", b, err, w, want+"m")
		}
	}
}

// waitForKey is a script that says READY and then waits for keys until the
// door is ended.
const waitForKey = `v3.console.write("READY"); for (;;) { v3.console.getkey(); }`

// A script door that ends, by finishing, by throwing or by reaching its run
// time limit, leaves nothing reading the session, and the menu reads next:
// first the keys the caller typed ahead that the script never read, then
// whatever the caller types after.
func TestV3ScriptDoorReturnsInputToMenu(t *testing.T) {
	for _, tc := range []struct {
		name     string
		script   string
		runLimit time.Duration
		wantErr  string // "" for a normal end
	}{
		{"completion", `v3.console.write("GOT:" + v3.console.getkey());`, 0, ""},
		{"exit", `v3.console.write("GOT:" + v3.console.getkey()); exit(3);`, 0, ""},
		{"error", `v3.console.write("GOT:" + v3.console.getkey()); throw new Error("boom");`, 0, "boom"},
		{"run time limit, computing", `v3.console.write("GOT:" + v3.console.getkey()); for (;;) {}`, 300 * time.Millisecond, ""},
		{"run time limit, sleeping", `v3.console.write("GOT:" + v3.console.getkey()); v3.util.sleep(60000);`, 300 * time.Millisecond, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.runLimit > 0 {
				orig := v3ScriptRunLimit
				v3ScriptRunLimit = func(config.DoorConfig) time.Duration { return tc.runLimit }
				t.Cleanup(func() { v3ScriptRunLimit = orig })
			}
			s, err := runScriptDoor(t, tc.script, func(_ *DoorCtx, s *scriptDoorSession) {
				// One read: the key the script takes and three it doesn't.
				s.send("axyz")
			})
			switch {
			case tc.wantErr == "" && err != nil:
				t.Fatalf("door error = %v, want a normal end", err)
			case tc.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tc.wantErr)):
				t.Fatalf("door error = %v, want one containing %q", err, tc.wantErr)
			}
			if out := s.output(); !strings.Contains(out, "GOT:a") {
				t.Fatalf("script output = %q, want GOT:a", out)
			}
			s.checkReleased(t)
			s.checkMenuReads(t, "xyz")
		})
	}
}

// When the BBS ends a script door, because the caller went idle, ran out of
// time or hung up, the door reports why and leaves nothing reading the
// session.
func TestV3ScriptDoorEndedByBBS(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(*DoorCtx, *scriptDoorSession)
		want  error
	}{
		{"caller idle", func(ctx *DoorCtx, _ *scriptDoorSession) {
			ctx.IdleTimeout = 300 * time.Millisecond
		}, editor.ErrIdleTimeout},
		{"time limit", func(ctx *DoorCtx, _ *scriptDoorSession) {
			ctx.User.TimeLimit = 1
			ctx.SessionStartTime = time.Now().Add(-time.Minute + 300*time.Millisecond)
		}, editor.ErrTimeLimit},
		{"caller hangs up", func(_ *DoorCtx, s *scriptDoorSession) {
			s.whenOutput("READY", s.eof)
		}, io.EOF},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, err := runScriptDoor(t, waitForKey, tc.setup)
			// errors.Is is exact here: ErrTimeLimit wraps ErrIdleTimeout,
			// but a plain idle error doesn't match ErrTimeLimit.
			if !errors.Is(err, tc.want) {
				t.Errorf("door error = %v, want %v", err, tc.want)
			}
			s.checkReleased(t)
		})
	}
}

// A caller who hangs up while the script is computing, not reading, ends the
// script through the session's context: the door returns and nothing is left
// reading the session. The script says LOOPING from inside its loop, and the
// hang-up comes once that write has returned, so it is the loop that is
// interrupted.
func TestV3ScriptDoorHangUpWhileComputing(t *testing.T) {
	const script = `for (var i = 0; ; i++) { if (i === 1000) v3.console.write("LOOPING"); }`
	s, err := runScriptDoor(t, script, func(_ *DoorCtx, s *scriptDoorSession) {
		s.whenOutput("LOOPING", func() { go s.cancel() })
	})
	if err != nil {
		t.Errorf("door error = %v, want a normal end (the menu's next read sees the hang-up)", err)
	}
	s.checkReleased(t)
}
