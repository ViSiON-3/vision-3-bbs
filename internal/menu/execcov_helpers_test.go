package menu

import (
	"bytes"
	"encoding/json"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/types"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// execcovSession is a testSession with a stderr stream, for the handlers that
// report door errors there (the embedded nil ssh.Session would panic).
type execcovSession struct {
	*testSession
	errOut bytes.Buffer
}

func (s *execcovSession) Stderr() io.ReadWriter { return &s.errOut }

// execcovMenus is a throwaway menu set the tests write their own menus into,
// so the run loop can be driven through menus built for one behaviour each.
type execcovMenus struct {
	t   *testing.T
	dir string
}

// execcovNewMenus points env's executor at an empty menu set under a temp
// directory. Nothing from menus/v3 is visible afterwards: a screen the test
// has not written is a missing file.
func execcovNewMenus(t *testing.T, env *menuEnv) *execcovMenus {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "menus", "execcov")
	for _, sub := range []string{"mnu", "cfg", "ansi", "bar"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	env.e.MenuSetPath = dir
	return &execcovMenus{t: t, dir: dir}
}

// write puts content at sub/name inside the menu set.
func (m *execcovMenus) write(sub, name, content string) {
	m.t.Helper()
	if err := os.WriteFile(filepath.Join(m.dir, sub, name), []byte(content), 0o644); err != nil {
		m.t.Fatal(err)
	}
}

// menu writes the three files of a menu: its record, its screen and its
// commands.
func (m *execcovMenus) menu(name string, rec MenuRecord, screen string, cmds ...CommandRecord) {
	m.t.Helper()
	recJSON, err := json.Marshal(rec)
	if err != nil {
		m.t.Fatal(err)
	}
	if cmds == nil {
		cmds = []CommandRecord{}
	}
	cmdJSON, err := json.Marshal(cmds)
	if err != nil {
		m.t.Fatal(err)
	}
	m.write("mnu", name+".MNU", string(recJSON))
	m.write("cfg", name+".CFG", string(cmdJSON))
	m.write("ansi", name+".ANS", screen)
}

// execcovStrings replaces the strings edit changes, leaving the rest as
// shipped, so a test can assert on text it chose rather than on the wording
// of strings.json.
func execcovStrings(env *menuEnv, edit func(s *config.StringsConfig)) {
	s := *env.e.Strings()
	edit(&s)
	env.e.SetStrings(s)
}

// execcovCall is one scripted session for MenuExecutor.Run.
type execcovCall struct {
	user    *user.User // nil for a caller who has not logged in
	start   string     // menu to start in
	input   string     // keystrokes; reads report a disconnect once they run out
	height  int        // terminal rows; 0 means 24
	started time.Time  // when the session connected; zero means now
	tracker types.AutoRunTracker
}

// execcovRun drives MenuExecutor.Run the way env.run drives a handler: a
// fresh session fed c.input, connecting from env.remoteIP in env.outputMode.
// The result's next is Run's action and user its returned user. Run's error
// is reported as it came back.
func execcovRun(env *menuEnv, c execcovCall) runResult {
	env.t.Helper()
	ts := newTestSession(c.input)
	if env.remoteIP != "" {
		ts.addr = &net.TCPAddr{IP: net.ParseIP(env.remoteIP), Port: 2222}
	}
	SetSessionOutputMode(ts, env.outputMode)
	env.t.Cleanup(func() { ClearSessionOutputMode(ts) })
	if c.height == 0 {
		c.height = 24
	}
	if c.tracker == nil {
		c.tracker = types.AutoRunTracker{}
	}
	if c.started.IsZero() {
		c.started = time.Now()
	}

	type ret struct {
		action string
		u      *user.User
		err    error
	}
	done := make(chan ret, 1)
	go func() {
		action, u, err := env.e.Run(ts, newTestTerminal(ts), env.um, c.user, c.start, 1,
			c.started, c.tracker, env.outputMode, "", 80, c.height)
		done <- ret{action, u, err}
	}()
	select {
	case r := <-done:
		return runResult{raw: ts.output(), user: r.u, next: r.action, err: r.err}
	case <-time.After(10 * time.Second):
		env.t.Fatalf("Run still going 10s after its input ran out")
		return runResult{}
	}
}

// execcovRunFn is env.run over an execcovSession, for handlers that write to
// the session's stderr. It returns what went to stderr alongside the result.
func execcovRunFn(env *menuEnv, fn RunnableFunc, u *user.User, args, input string) (runResult, string) {
	env.t.Helper()
	ts := &execcovSession{testSession: newTestSession(input)}
	SetSessionOutputMode(ts, env.outputMode)
	env.t.Cleanup(func() {
		resetSessionIH(ts)
		ClearSessionOutputMode(ts)
	})
	c := &cmdCtx{
		e:                env.e,
		s:                ts,
		terminal:         newTestTerminal(ts.testSession),
		userManager:      env.um,
		currentUser:      u,
		nodeNumber:       1,
		sessionStartTime: time.Now(),
		outputMode:       env.outputMode,
		termWidth:        80,
		termHeight:       24,
	}

	type ret struct {
		u    *user.User
		next string
		err  error
	}
	done := make(chan ret, 1)
	go func() {
		u, next, err := fn(c, args)
		done <- ret{u, next, err}
	}()
	select {
	case r := <-done:
		return runResult{raw: ts.output(), user: r.u, next: r.next, err: r.err}, ts.errOut.String()
	case <-time.After(10 * time.Second):
		env.t.Fatalf("handler still running 10s after its input ran out")
		return runResult{}, ""
	}
}

// execcovCount is how many times want appears in the stripped output.
func execcovCount(r runResult, want string) int { return strings.Count(r.text(), want) }

// execcovHandle is u's handle, or "<nil>".
func execcovHandle(u *user.User) string {
	if u == nil {
		return "<nil>"
	}
	return u.Handle
}
