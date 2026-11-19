//go:build !windows

package menu

import (
	"bytes"
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gliderlabs/ssh"
	"golang.org/x/term"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/editor/testterm"
	"github.com/ViSiON-3/vision-3-bbs/internal/session"
	"github.com/ViSiON-3/vision-3-bbs/internal/snoop"
	"github.com/ViSiON-3/vision-3-bbs/internal/toast"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// noticeMenuScript draws a full screen and waits for keys as the board's menu
// scripts do, a frame at a time, until W.
const noticeMenuScript = `v3.console.write("\x1b[2J\x1b[HSCRIPT SCREEN\x1b[24;1HBOTTOM ROW\x1b[5;7H");
for (;;) {
  var k = v3.console.readkey(20);
  if (k.toUpperCase() === "W") { v3.prompt.answer("W"); break; }
}`

// noticeSession is a caller's connection as the transports give it: a tap,
// a context for the script engine, and a record of everything written.
type noticeSession struct {
	*toastSession
	ctx    *hookCtx
	mu     sync.Mutex
	out    bytes.Buffer
	writes []timedWrite
}

func (s *noticeSession) Context() ssh.Context { return s.ctx }
func (s *noticeSession) RemoteAddr() net.Addr { return doorcovAddr }

func (s *noticeSession) Write(p []byte) (int, error) {
	s.mu.Lock()
	s.out.Write(p)
	s.writes = append(s.writes, timedWrite{at: time.Now(), text: string(p)})
	s.mu.Unlock()
	return s.toastSession.Write(p)
}

// timedWrite is one write to a noticeSession and when it was made.
type timedWrite struct {
	at   time.Time
	text string
}

// firstWrite returns when text was first written in a single write, and
// false while it has not been.
func (s *noticeSession) firstWrite(text string) (time.Time, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, w := range s.writes {
		if strings.Contains(w.text, text) {
			return w.at, true
		}
	}
	return time.Time{}, false
}

// draws counts how many times the script has drawn its screen.
func (s *noticeSession) draws() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return strings.Count(s.out.String(), "SCRIPT SCREEN")
}

type noticeEnv struct {
	// hangUp ends the caller's session, which ends a script still running.
	hangUp context.CancelFunc
	e      *MenuExecutor
	s      *noticeSession
	term   *testterm.Term
	sess   *session.BbsSession
	u      *user.User
	st     *runLoopState
}

// newNoticeEnv is a caller at node 3 sitting at a MAIN menu drawn by
// noticeMenuScript, with toasts attached as a logged-in session has them.
func newNoticeEnv(t *testing.T) *noticeEnv {
	t.Helper()
	return newNoticeEnvWith(t, noticeMenuScript)
}

// newNoticeEnvWith is newNoticeEnv with the menu drawn by script.
func newNoticeEnvWith(t *testing.T, script string) *noticeEnv {
	t.Helper()
	oldDwell, oldRearm := toastDwell, toastRearm
	toastDwell, toastRearm = 50*time.Millisecond, 10*time.Millisecond
	t.Cleanup(func() { toastDwell, toastRearm = oldDwell, oldRearm })

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "menu.js"), []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}
	reg := session.NewSessionRegistry()
	e := &MenuExecutor{SessionRegistry: reg, MenuSetPath: dir, RootConfigPath: dir}
	e.SetServerConfig(config.ServerConfig{DataDir: t.TempDir(), CoSysOpLevel: 250})
	e.SetStrings(config.StringsConfig{ToastOverlayFormat: ">> %s", TimeLimitWarning: "|12%d minutes left"})
	e.SetDoorRegistry(map[string]config.DoorConfig{
		"DRAW": {Type: "v3_script", Script: "menu.js", WorkingDirectory: dir},
	})

	tt := testterm.New(80, 24)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	s := &noticeSession{
		toastSession: &toastSession{Session: testterm.NewSession(tt, ""), tap: snoop.NewTap()},
		ctx:          &hookCtx{Context: ctx},
	}
	u := &user.User{ID: 7, Handle: "Neo", AccessLevel: 50, TimeLimit: 60}
	sess := &session.BbsSession{NodeID: 3, StartTime: time.Now(), User: u, Width: 80, Height: 24}
	reg.Register(sess)
	SetSessionOutput(s, s)
	SetSessionOutputMode(s, ansi.OutputModeUTF8)
	sess.SetToastsReady(true)
	e.AttachToasts(s, sess)
	t.Cleanup(func() {
		resetSessionIH(s)
		sessionDeadlines.Delete(s)
		DetachToasts(s)
		ClearSessionOutput(s)
		ClearSessionOutputMode(s)
		s.tap.Close()
	})
	st := &runLoopState{e: e, s: s, terminal: term.NewTerminal(s, ""), nodeNumber: 3, sessionStartTime: time.Now(),
		outputMode: ansi.OutputModeUTF8, termWidth: 80, termHeight: 24,
		currentMenuName: "MAIN", currentUser: u}
	return &noticeEnv{hangUp: cancel, e: e, s: s, term: tt, sess: sess, u: u, st: st}
}

type menuScriptResult struct {
	line    string
	act     loopAction
	err     error
	handled bool
}

// run starts the menu's script, as the menu loop does, and waits for it to
// draw its screen.
func (env *noticeEnv) run(t *testing.T) <-chan menuScriptResult {
	t.Helper()
	done := make(chan menuScriptResult, 1)
	exited := make(chan struct{})
	go func() {
		defer close(exited)
		l, a, err, h := env.st.runMenuScript(&MenuRecord{Script: "DRAW"}, []CommandRecord{{Keys: "W", Command: "RUN:X"}})
		done <- menuScriptResult{l, a, err, h}
	}()
	// Registered after the fixture's own cleanup, so it runs first: a test
	// that stops early does not tear the fixture down under the script.
	t.Cleanup(func() {
		env.hangUp()
		select {
		case <-exited:
		case <-time.After(screenWait):
			t.Errorf("the menu script was still running %s after the test", screenWait)
		}
	})
	toastWaitFor(t, "the script's screen", func() bool { return strings.Contains(env.term.Row(1), "SCRIPT SCREEN") })
	return done
}

// finish presses W and checks the script answered with it.
func (env *noticeEnv) finish(t *testing.T, done <-chan menuScriptResult) {
	t.Helper()
	env.s.Send("w")
	select {
	case r := <-done:
		if r.err != nil || !r.handled || r.act != loopFallthrough || r.line != "W" {
			t.Errorf("runMenuScript = %q %v %v handled=%v, want W", r.line, r.act, r.err, r.handled)
		}
	case <-time.After(screenWait):
		t.Fatalf("the script did not answer W:\n%s", env.term.Snapshot())
	}
}

func TestToastReachesACallerWaitingAtAScriptedMenu(t *testing.T) {
	env := newNoticeEnv(t)
	done := env.run(t)

	if err := env.e.SessionRegistry.SendToast(env.e.toastStore(), env.u.ID, toast.Toast{Text: "Hello there"}); err != nil {
		t.Fatal(err)
	}
	// Read from what was written: the toast can come and go between two
	// looks at the screen.
	toastWaitFor(t, "the toast, with no key pressed", func() bool { return strings.Contains(env.out(), ">> Hello there") })
	toastWaitFor(t, "the toast leaving the queue", func() bool {
		q, err := env.e.toastStore().Peek(env.u.ID)
		return err == nil && len(q) == 0
	})
	toastWaitFor(t, "the script's screen drawn again", func() bool {
		return env.s.draws() >= 2 && strings.Contains(env.term.Row(24), "BOTTOM ROW")
	})
	if !strings.Contains(env.term.Row(1), "SCRIPT SCREEN") {
		t.Errorf("screen after the toast:\n%s", env.term.Snapshot())
	}
	env.finish(t, done)
}

func TestKeyThatClearsAToastReachesTheMenuScript(t *testing.T) {
	env := newNoticeEnv(t)
	toastDwell = 10 * time.Second
	done := env.run(t)

	if err := env.e.SessionRegistry.SendToast(env.e.toastStore(), env.u.ID, toast.Toast{Text: "Hello there"}); err != nil {
		t.Fatal(err)
	}
	toastWaitFor(t, "the toast", func() bool { return strings.Contains(env.term.Row(24), ">> Hello there") })
	env.finish(t, done)
}

func TestTimeLeftWarningAtAScriptedMenu(t *testing.T) {
	t.Run("comes due while the caller waits", func(t *testing.T) {
		env := newNoticeEnv(t)
		deadline := time.Now().Add(timeLimitWarnWindow + 300*time.Millisecond)
		sessionDeadlines.Store(env.s, deadline)
		done := env.run(t)
		// Read from what was written, and when: the warning can come and go
		// between two looks at the screen.
		toastWaitFor(t, "the warning on the bottom row", func() bool {
			out := env.out()
			return strings.Contains(out, "\x1b[24;1H\x1b[2K") && strings.Contains(out, "5 minutes left")
		})
		if at, _ := env.s.firstWrite("minutes left"); at.Before(deadline.Add(-timeLimitWarnWindow)) {
			t.Errorf("warning written %v before it was due", deadline.Add(-timeLimitWarnWindow).Sub(at))
		}
		toastWaitFor(t, "the script's screen drawn again", func() bool {
			return env.s.draws() >= 2 && strings.Contains(env.term.Row(24), "BOTTOM ROW")
		})
		// Once per menu, as at a built-in menu.
		time.Sleep(10 * toastDwell)
		if n := env.s.draws(); n != 2 {
			t.Errorf("screen drawn %d times, want 2", n)
		}
		env.finish(t, done)
	})

	t.Run("already due when the menu is shown", func(t *testing.T) {
		env := newNoticeEnv(t)
		sessionDeadlines.Store(env.s, time.Now().Add(2*time.Minute+30*time.Second))
		done := env.run(t)
		// The warning can come and go before a look at the screen sees it.
		toastWaitFor(t, "the warning on the bottom row", func() bool {
			out := env.out()
			return strings.Contains(out, "\x1b[24;1H\x1b[2K") && strings.Contains(out, "3 minutes left")
		})
		toastWaitFor(t, "the script's screen drawn again", func() bool {
			return env.s.draws() >= 2 && strings.Contains(env.term.Row(24), "BOTTOM ROW")
		})
		env.finish(t, done)
	})

	t.Run("given before a waiting toast", func(t *testing.T) {
		env := newNoticeEnv(t)
		if err := env.e.SessionRegistry.SendToast(env.e.toastStore(), env.u.ID, toast.Toast{Text: "Hello there"}); err != nil {
			t.Fatal(err)
		}
		sessionDeadlines.Store(env.s, time.Now().Add(2*time.Minute+30*time.Second))
		done := env.run(t)
		toastWaitFor(t, "the warning and the toast", func() bool {
			out := env.out()
			return strings.Contains(out, "3 minutes left") && strings.Contains(out, ">> Hello there")
		})
		if out := env.out(); strings.Index(out, "3 minutes left") > strings.Index(out, ">> Hello there") {
			t.Errorf("toast shown before the warning:\n%q", out)
		}
		env.finish(t, done)
	})

	t.Run("not given before the window", func(t *testing.T) {
		env := newNoticeEnv(t)
		sessionDeadlines.Store(env.s, time.Now().Add(timeLimitWarnWindow+time.Minute))
		done := env.run(t)
		time.Sleep(10 * toastDwell)
		if strings.Contains(env.out(), "minutes left") || env.s.draws() != 1 {
			t.Errorf("draws=%d, output:\n%s", env.s.draws(), env.term.Snapshot())
		}
		env.finish(t, done)
	})
}

func (env *noticeEnv) out() string {
	env.s.mu.Lock()
	defer env.s.mu.Unlock()
	return env.s.out.String()
}

// A sysop's chat request that comes while a toast is up at a scripted menu
// opens chat, and the script draws its screen again afterwards.
func TestChatRequestDuringAToastAtAScriptedMenu(t *testing.T) {
	env := newNoticeEnv(t)
	toastDwell = 10 * time.Second
	sysop := env.s.tap.AttachAs("SysOp")
	t.Cleanup(sysop.Close)
	if prev := sysopChat.Load(); prev != nil {
		t.Cleanup(func() { sysopChat.Store(prev) })
	} else {
		t.Cleanup(func() { sysopChat.Store(nil) })
	}
	SetSysopChatEnv(chatEnv{
		theme:   func() *config.ThemeConfig { return &config.ThemeConfig{} },
		strings: func() *config.StringsConfig { return &config.StringsConfig{SysopChatHeader: " %s chatting with %s "} },
		caller:  func(*snoop.Tap) (string, int, int) { return "Neo", 80, 24 },
	})
	done := env.run(t)
	// Registered after run's, so it runs first: a test that stops early
	// ends the chat, which the hang-up alone does not.
	t.Cleanup(func() { _ = env.s.tap.StopChat("SysOp") })

	if err := env.e.SessionRegistry.SendToast(env.e.toastStore(), env.u.ID, toast.Toast{Text: "Hello there"}); err != nil {
		t.Fatal(err)
	}
	toastWaitFor(t, "the toast", func() bool { return strings.Contains(env.term.Row(24), ">> Hello there") })
	started := make(chan error, 1)
	go func() {
		ok, err := env.s.tap.RequestChat("SysOp", 2*time.Second)
		if err == nil && !ok {
			err = errors.New("chat not started")
		}
		started <- err
	}()
	select {
	case err := <-started:
		if err != nil {
			t.Fatalf("RequestChat: %v", err)
		}
	case <-time.After(screenWait):
		t.Fatal("RequestChat did not return")
	}
	toastWaitFor(t, "the chat screen", func() bool { return strings.Contains(env.term.Snapshot(), "chatting with") })
	_ = env.s.tap.StopChat("SysOp")
	toastWaitFor(t, "the script's screen drawn again", func() bool {
		return env.s.draws() >= 2 && strings.Contains(env.term.Row(24), "BOTTOM ROW")
	})
	env.finish(t, done)
}

// A script that does not paint the toast's row does not keep the toast on
// it once the script runs again.
func TestToastRowClearedForAScriptThatDoesNotRepaintIt(t *testing.T) {
	env := newNoticeEnvWith(t, `v3.console.write("\x1b[HSCRIPT SCREEN");
for (;;) {
  var k = v3.console.readkey(20);
  if (k.toUpperCase() === "W") { v3.prompt.answer("W"); break; }
}`)
	done := env.run(t)
	if err := env.e.SessionRegistry.SendToast(env.e.toastStore(), env.u.ID, toast.Toast{Text: "Hello there"}); err != nil {
		t.Fatal(err)
	}
	toastWaitFor(t, "the toast", func() bool { return strings.Contains(env.out(), ">> Hello there") })
	toastWaitFor(t, "the script's screen drawn again", func() bool { return env.s.draws() >= 2 })
	if row := env.term.Row(24); strings.Contains(row, "Hello") {
		t.Errorf("toast still on row 24 after the script ran again: %q", row)
	}
	env.finish(t, done)
}

// A key that takes a toast down holds off the next one for toastRearm, as at
// a key wait, so a queue of toasts does not flash past someone typing.
func TestKeyedToastHoldsOffTheNextAtAScriptedMenu(t *testing.T) {
	env := newNoticeEnv(t)
	toastDwell, toastRearm = 10*time.Second, 400*time.Millisecond
	done := env.run(t)
	for _, text := range []string{"First toast", "Second toast"} {
		if err := env.e.SessionRegistry.SendToast(env.e.toastStore(), env.u.ID, toast.Toast{Text: text}); err != nil {
			t.Fatal(err)
		}
	}
	toastWaitFor(t, "the first toast", func() bool { return strings.Contains(env.out(), ">> First toast") })
	keyed := time.Now()
	env.s.Send("x") // not a command; the script reads it and waits on
	toastWaitFor(t, "the second toast", func() bool { return strings.Contains(env.out(), ">> Second toast") })
	second, _ := env.s.firstWrite(">> Second toast")
	if waited := second.Sub(keyed); waited < toastRearm {
		t.Errorf("second toast %v after the key, want at least %v", waited, toastRearm)
	}
	env.finish(t, done)
}

// A toast that comes as the script ends on a key, after its last key wait,
// is shown at the next key wait: here the key wait of the screen the key
// opened.
func TestToastAsTheScriptEndsOnAKeyIsShownAtTheNextKeyWait(t *testing.T) {
	env := newNoticeEnvWith(t, `v3.console.write("\x1b[2J\x1b[HSCRIPT SCREEN\x1b[24;1HBOTTOM ROW\x1b[5;7H");
for (;;) {
  var k = v3.console.readkey(20);
  if (k.toUpperCase() !== "W") continue;
  v3.console.write("<chosen>");
  // The work a script does after its last key wait; the test sends the
  // toast during it.
  for (var start = Date.now(); Date.now() - start < 2000;) {}
  v3.prompt.answer("W");
  break;
}`)
	done := env.run(t)
	env.s.Send("w")
	toastWaitFor(t, "the key taken", func() bool { return strings.Contains(env.out(), "<chosen>") })
	if err := env.e.SessionRegistry.SendToast(env.e.toastStore(), env.u.ID, toast.Toast{Text: "Hello there"}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
		t.Fatal("the script ended before the toast was sent")
	default:
	}
	select {
	case r := <-done:
		if r.err != nil || !r.handled || r.line != "W" {
			t.Fatalf("runMenuScript = %q %v %v handled=%v, want W", r.line, r.act, r.err, r.handled)
		}
	case <-time.After(screenWait):
		t.Fatal("the script did not answer W")
	}
	if strings.Contains(env.out(), ">> Hello there") {
		t.Fatal("toast shown before the next key wait")
	}

	// The screen W opened waits for a key.
	read := make(chan error, 1)
	go func() {
		_, err := getSessionIH(env.s).ReadKeyWithTimeout(screenWait)
		read <- err
	}()
	toastWaitFor(t, "the toast at the next key wait", func() bool { return strings.Contains(env.out(), ">> Hello there") })
	env.s.Send("x")
	if err := <-read; err != nil {
		t.Errorf("key wait = %v", err)
	}
}

// A toast that comes between the two keys of a command, as the board's menu
// scripts read them, does not change the command: /W runs, not W, and the
// toast is shown at the next key wait.
func TestToastBetweenTheKeysOfACommandAtAScriptedMenu(t *testing.T) {
	env := newNoticeEnvWith(t, `v3.console.write("\x1b[2J\x1b[HSCRIPT SCREEN\x1b[24;1HBOTTOM ROW\x1b[5;7H");
for (;;) {
  var k = v3.console.readkey(20);
  if (k.toUpperCase() === "W") { v3.prompt.answer("W"); break; }
  if (k !== "/") continue;
  v3.console.write("<gap>");
  var next = v3.console.readkey(500);
  if (next.toUpperCase() === "W") { v3.prompt.answer("/W"); break; }
}`)
	done := env.run(t)
	env.s.Send("/")
	toastWaitFor(t, "the first key taken", func() bool { return strings.Contains(env.out(), "<gap>") })
	if err := env.e.SessionRegistry.SendToast(env.e.toastStore(), env.u.ID, toast.Toast{Text: "Hello there"}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond) // the toast fires in the gap
	env.s.Send("w")
	select {
	case r := <-done:
		if r.err != nil || !r.handled || r.line != "/W" {
			t.Fatalf("runMenuScript = %q %v %v handled=%v, want /W", r.line, r.act, r.err, r.handled)
		}
	case <-time.After(screenWait):
		t.Fatalf("the script did not answer:\n%s", env.term.Snapshot())
	}
	if strings.Contains(env.out(), ">> Hello there") {
		t.Fatal("toast shown before the next key wait")
	}

	read := make(chan error, 1)
	go func() {
		_, err := getSessionIH(env.s).ReadKeyWithTimeout(screenWait)
		read <- err
	}()
	toastWaitFor(t, "the toast at the next key wait", func() bool { return strings.Contains(env.out(), ">> Hello there") })
	env.s.Send("x")
	if err := <-read; err != nil {
		t.Errorf("key wait = %v", err)
	}
}
