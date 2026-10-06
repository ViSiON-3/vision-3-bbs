package menu

import (
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gliderlabs/ssh"
	"golang.org/x/term"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/editor"
	"github.com/ViSiON-3/vision-3-bbs/internal/editor/testterm"
)

// konfigPTY models the real terminal separately from the caller's saved size.
type konfigPTY struct {
	*testterm.Session
	mu     sync.Mutex
	screen *testterm.Term
	window ssh.Window
	events chan ssh.Window
}

func (s *konfigPTY) Pty() (ssh.Pty, <-chan ssh.Window, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return ssh.Pty{Window: s.window}, s.events, true
}
func (s *konfigPTY) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.screen.Write(p)
}
func (s *konfigPTY) terminal() *testterm.Term {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.screen
}
func (s *konfigPTY) resize(width, height int) {
	s.mu.Lock()
	s.window = ssh.Window{Width: width, Height: height}
	s.screen = testterm.New(width, height)
	win := s.window
	s.mu.Unlock()
	s.events <- win
}

func TestKonfigResponsiveTerminalSizes(t *testing.T) {
	for _, size := range [][2]int{{40, 21}, {60, 21}, {79, 21}, {80, 21}, {132, 25}, {255, 60}} {
		for _, mode := range []ansi.OutputMode{ansi.OutputModeUTF8, ansi.OutputModeCP437} {
			t.Run(fmt.Sprintf("%dx%d/%v", size[0], size[1], mode), func(t *testing.T) {
				um, u := newUserConfigTestUser(t)
				var opts []testterm.Option
				if mode == ansi.OutputModeCP437 {
					opts = append(opts, testterm.CP437())
				}
				screen := testterm.New(size[0], size[1], opts...)
				sess := &konfigPTY{Session: testterm.NewSession(nil, "q"), screen: screen, window: ssh.Window{Width: size[0], Height: size[1]}}
				t.Cleanup(func() { resetSessionIH(sess); sessionTermSizes.Delete(sess) })
				c := &cmdCtx{e: konfigStockExecutor(t), s: sess, terminal: term.NewTerminal(sess, ""), userManager: um, currentUser: u, outputMode: mode, termWidth: 80, termHeight: 25}
				if _, _, err := runUserKonfig(c, ""); err != nil {
					t.Fatal(err)
				}
				snap := screen.Snapshot()
				for _, label := range []string{"[A] Screen Width", "[F] Auto-Signature", "[G] Real Name", "[L] File Columns", "Terminal", "Messages", "Personal", "Files"} {
					if !strings.Contains(snap, label) {
						t.Errorf("missing %q:\n%s", label, snap)
					}
				}
				if size[0] < 80 {
					if !strings.Contains(screen.Row(1), c.e.Strings().KonfigTitle) {
						t.Errorf("compact title missing: %q", screen.Row(1))
					}
					if !strings.Contains(screen.Row(17), "[L] File Columns") {
						t.Errorf("compact layout displaced: %s", snap)
					}
					if !strings.Contains(screen.Row(19), "Columns your terminal") {
						t.Errorf("help missing: %q", screen.Row(19))
					}
				} else if !strings.Contains(screen.Row(konfigTopRow), "Terminal") {
					t.Errorf("normal layout displaced: %s", snap)
				}
				if !strings.Contains(screen.Row(21), "ESC") {
					t.Errorf("exit legend missing: %q", screen.Row(21))
				}
				if len(screen.Unhandled()) > 0 {
					t.Errorf("unhandled output: %v", screen.Unhandled())
				}
			})
		}
	}
}

func TestKonfigCompactLayoutFitsMinimumHeight(t *testing.T) {
	items, headings := layoutKonfig(konfigSections(konfigTestExecutor(t).Strings()), 40)
	if len(headings) != 4 || len(items) != 12 {
		t.Fatalf("headings/items=%d/%d", len(headings), len(items))
	}
	for _, h := range headings {
		if h.col != 2 || h.row < 2 || h.row >= 18 {
			t.Errorf("heading outside form: %+v", h)
		}
	}
	for _, it := range items {
		if it.col != 2 || it.column != 0 && it.column != 1 || it.row < 2 || it.row >= 18 {
			t.Errorf("item outside form: %+v", it)
		}
	}
	if items[len(items)-1].row != 17 {
		t.Errorf("last item row=%d", items[len(items)-1].row)
	}
}

func TestKonfigCompactEditingAndWidthRecovery(t *testing.T) {
	um, u := newUserConfigTestUser(t)
	for visit := 0; visit < 3; visit++ {
		u.ScreenWidth = 255
		if err := um.UpdateUser(u); err != nil {
			t.Fatal(err)
		}
		screen := testterm.New(40, 21)
		keys := "l" + keyDown + " " + keyEsc + "g" + keyClear + "Discard" + keyEsc + "g" + keyClear + "New Name\ra" + keyClear + "132\rq"
		sess := &konfigPTY{Session: testterm.NewSession(nil, keys), screen: screen, window: ssh.Window{Width: 40, Height: 21}}
		t.Cleanup(func() { resetSessionIH(sess); sessionTermSizes.Delete(sess) })
		c := &cmdCtx{e: konfigTestExecutor(t), s: sess, terminal: term.NewTerminal(sess, ""), userManager: um, currentUser: u, outputMode: ansi.OutputModeUTF8, termWidth: 255, termHeight: 21}
		got, next, err := runUserKonfig(c, "MAIN")
		if err != nil {
			t.Fatal(err)
		}
		if got.RealName != "New Name" || got.ScreenWidth != 132 || next != "GOTO:MAIN" {
			t.Fatalf("name/width/next=%q/%d/%q", got.RealName, got.ScreenWidth, next)
		}
		if !strings.Contains(screen.Row(17), "[L] File Columns") {
			t.Fatalf("wrong stored width hid form:\n%s", screen.Snapshot())
		}
		w, _, ok := takeSessionTermSize(sess)
		if !ok || w != 132 {
			t.Fatalf("recovery session width=%d (%v)", w, ok)
		}
		if reloadUser(t, um).ScreenWidth != 132 {
			t.Fatal("width not persisted")
		}
		u = got
	}
}

func waitKonfigScreen(t *testing.T, sess *konfigPTY, ready func(*testterm.Term) bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if ready(sess.terminal()) {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out waiting for Konfig:\n%s", sess.terminal().Snapshot())
}

func TestKonfigResizeRetainsFocusAndEditors(t *testing.T) {
	um, u := newUserConfigTestUser(t)
	sess := &konfigPTY{Session: testterm.NewSession(nil, ""), screen: testterm.New(80, 24), window: ssh.Window{Width: 80, Height: 24}, events: make(chan ssh.Window, 1)}
	c := &cmdCtx{e: konfigTestExecutor(t), s: sess, terminal: term.NewTerminal(sess, ""), userManager: um, currentUser: u, outputMode: ansi.OutputModeUTF8, termWidth: 80, termHeight: 24}
	// Model production: main is the sole consumer and updates shared state.
	var physicalWidth, mainResizes atomic.Int32
	physicalWidth.Store(80)
	RegisterTerminalPhysicalWidth(c.terminal, &physicalWidth)
	mainDone := make(chan struct{})
	var closeEvents sync.Once
	go func() {
		defer close(mainDone)
		for win := range sess.events {
			physicalWidth.Store(int32(win.Width))
			_ = c.terminal.SetSize(win.Width, win.Height)
			mainResizes.Add(1)
		}
	}()
	t.Cleanup(func() {
		closeEvents.Do(func() { close(sess.events) })
		<-mainDone
		ClearTerminalPhysicalWidth(c.terminal)
	})
	done := make(chan error, 1)
	go func() { _, _, err := runUserKonfig(c, ""); done <- err }()
	t.Cleanup(func() { sess.Send("q"); resetSessionIH(sess); sessionTermSizes.Delete(sess) })
	waitKonfigScreen(t, sess, func(s *testterm.Term) bool { return strings.Contains(s.Row(21), "ESC") })
	sess.Send(keyDown)
	waitKonfigScreen(t, sess, func(s *testterm.Term) bool { return s.Cell(9, 2).Bg == 46 })
	sess.resize(40, 21)
	waitKonfigScreen(t, sess, func(s *testterm.Term) bool { return s.Cell(4, 2).Bg == 46 && strings.Contains(s.Row(21), "ESC") })
	// Tab advances in the single column; arrow navigation and focus survive.
	sess.Send("\t")
	waitKonfigScreen(t, sess, func(s *testterm.Term) bool { return s.Cell(5, 2).Bg == 46 })
	sess.Send("g" + keyClear + "Unfinished")
	waitKonfigScreen(t, sess, func(s *testterm.Term) bool { return strings.Contains(s.Row(20), "Unfinished") })
	sess.resize(80, 24)
	waitKonfigScreen(t, sess, func(s *testterm.Term) bool { return strings.Contains(s.Row(19), "Unfinished") && s.CursorVisible() })
	sess.resize(40, 21)
	waitKonfigScreen(t, sess, func(s *testterm.Term) bool { return strings.Contains(s.Row(20), "Unfinished") && s.CursorVisible() })
	sess.resize(80, 24)
	waitKonfigScreen(t, sess, func(s *testterm.Term) bool { return strings.Contains(s.Row(19), "Unfinished") && s.CursorVisible() })
	sess.Send(" Name\r")
	waitKonfigScreen(t, sess, func(s *testterm.Term) bool {
		return strings.Contains(s.Snapshot(), "Unfinished Name") && !s.CursorVisible()
	})
	sess.Send("l" + keyDown)
	waitKonfigScreen(t, sess, func(s *testterm.Term) bool { return s.Cell(9, 23).Bg == 46 })
	sess.resize(40, 21)
	waitKonfigScreen(t, sess, func(s *testterm.Term) bool { return s.Cell(4, 4).Bg == 46 && strings.Contains(s.Snapshot(), "┌") })
	sess.Send(" " + keyEsc)
	waitKonfigScreen(t, sess, func(s *testterm.Term) bool {
		return strings.Contains(s.Row(17), "[L] File Columns") && !strings.Contains(s.Snapshot(), "┌")
	})
	closeEvents.Do(func() { close(sess.events) }) // Main can close its channel without blocking Konfig.
	sess.Send("q")
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Konfig did not exit")
	}
	if reloadUser(t, um).RealName != "Unfinished Name" {
		t.Fatal("resize lost edited text")
	}
	if mainResizes.Load() != 5 || physicalWidth.Load() != 40 {
		t.Fatalf("main missed resizes: count=%d, width=%d", mainResizes.Load(), physicalWidth.Load())
	}
}

func TestKonfigRealWidthOverridesSmallerStoredWidth(t *testing.T) {
	um, u := newUserConfigTestUser(t)
	u.ScreenWidth = 40
	screen := testterm.New(80, 21)
	sess := &konfigPTY{Session: testterm.NewSession(nil, "q"), screen: screen, window: ssh.Window{Width: 80, Height: 21}}
	t.Cleanup(func() { resetSessionIH(sess) })
	c := &cmdCtx{e: konfigStockExecutor(t), s: sess, terminal: term.NewTerminal(sess, ""), userManager: um, currentUser: u, outputMode: ansi.OutputModeUTF8, termWidth: 40, termHeight: 21}
	if _, _, err := runUserKonfig(c, ""); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(screen.Row(konfigTopRow), "Personal") || !strings.HasSuffix(screen.Row(1), "[ViSiON/3]") {
		t.Fatalf("smaller stored width changed normal layout:\n%s", screen.Snapshot())
	}
}

func TestKonfigCompactFieldScrollsWithoutTruncatingValue(t *testing.T) {
	um, u := newUserConfigTestUser(t)
	screen := testterm.New(40, 21)
	sess := testterm.NewSession(screen, "\x1b[D\x7fZ\r")
	t.Cleanup(func() { resetSessionIH(sess) })
	c := &cmdCtx{e: konfigTestExecutor(t), s: sess, terminal: term.NewTerminal(sess, ""), userManager: um, currentUser: u, outputMode: ansi.OutputModeUTF8, termWidth: 40, termHeight: 21}
	st := &konfigState{c: c, ih: getSessionIH(sess)}
	st.relayout()
	if err := st.renderAll(); err != nil {
		t.Fatal(err)
	}
	value, ok, err := st.readField("Long field", strings.Repeat("x", 64), 72, false, "")
	if err != nil || !ok || value != strings.Repeat("x", 62)+"Zx" {
		t.Fatalf("field=%q (%v), error=%v", value, ok, err)
	}
	if !strings.Contains(screen.Row(21), "ESC") {
		t.Fatalf("field wrapped over legend:\n%s", screen.Snapshot())
	}
}

func TestKonfigUsesLiveWidthWhenPTYSnapshotIsStale(t *testing.T) {
	um, u := newUserConfigTestUser(t)
	screen := testterm.New(40, 21)
	sess := &konfigPTY{Session: testterm.NewSession(nil, "q"), screen: screen, window: ssh.Window{Width: 80, Height: 24}}
	t.Cleanup(func() { resetSessionIH(sess) })
	c := &cmdCtx{e: konfigTestExecutor(t), s: sess, terminal: term.NewTerminal(sess, ""), userManager: um, currentUser: u, outputMode: ansi.OutputModeUTF8, termWidth: 80, termHeight: 24}
	var physicalWidth atomic.Int32
	physicalWidth.Store(40)
	RegisterTerminalPhysicalWidth(c.terminal, &physicalWidth)
	defer ClearTerminalPhysicalWidth(c.terminal)
	if _, _, err := runUserKonfig(c, ""); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(screen.Row(17), "[L] File Columns") || !strings.Contains(screen.Row(1), "User Konfig") {
		t.Fatalf("stale PTY hid compact layout:\n%s", screen.Snapshot())
	}
}

func TestKonfigWidthWatcherPreservesIdleTimeout(t *testing.T) {
	um, u := newUserConfigTestUser(t)
	sess := testterm.NewSession(testterm.New(80, 24), "")
	t.Cleanup(func() { resetSessionIH(sess) })
	c := &cmdCtx{e: konfigTestExecutor(t), s: sess, terminal: term.NewTerminal(sess, ""), userManager: um, currentUser: u, outputMode: ansi.OutputModeUTF8, termWidth: 80, termHeight: 24}
	var physicalWidth atomic.Int32
	physicalWidth.Store(80)
	RegisterTerminalPhysicalWidth(c.terminal, &physicalWidth)
	defer ClearTerminalPhysicalWidth(c.terminal)
	st := &konfigState{c: c, ih: getSessionIH(sess), physicalWidth: 80}
	stopWatching := st.watchWidth()
	defer stopWatching()
	// Longer than a polling tick: unchanged widths must never wake the reader
	// and restart its idle timer. The deadline bounds a regression's failure.
	st.ih.SetSessionIdleTimeout(250 * time.Millisecond)
	st.ih.SetSessionDeadline(time.Now().Add(time.Second))
	if _, err := st.readKey(nil); err != editor.ErrIdleTimeout {
		t.Fatalf("idle timeout defeated by width watcher: %v", err)
	}
}
