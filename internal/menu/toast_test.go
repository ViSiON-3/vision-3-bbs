package menu

import (
	"errors"
	"strings"
	"testing"
	"time"

	"golang.org/x/term"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/editor"
	"github.com/ViSiON-3/vision-3-bbs/internal/editor/testterm"
	"github.com/ViSiON-3/vision-3-bbs/internal/session"
	"github.com/ViSiON-3/vision-3-bbs/internal/snoop"
	"github.com/ViSiON-3/vision-3-bbs/internal/toast"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// toastSession is a caller's connection with the tap the real transports have.
type toastSession struct {
	*testterm.Session
	tap *snoop.Tap
}

func (s *toastSession) Tap() *snoop.Tap { return s.tap }

func (s *toastSession) Write(p []byte) (int, error) {
	s.tap.Output(p)
	return s.Session.Write(p)
}

type toastEnv struct {
	e    *MenuExecutor
	s    *toastSession
	term *testterm.Term
	sess *session.BbsSession
	u    *user.User
}

func newToastEnv(t *testing.T) *toastEnv {
	t.Helper()
	oldDwell, oldRearm := toastDwell, toastRearm
	toastDwell, toastRearm = 50*time.Millisecond, 10*time.Millisecond
	t.Cleanup(func() { toastDwell, toastRearm = oldDwell, oldRearm })

	reg := session.NewSessionRegistry()
	e := &MenuExecutor{SessionRegistry: reg}
	e.SetServerConfig(config.ServerConfig{DataDir: t.TempDir()})
	e.SetStrings(config.StringsConfig{ToastOverlayFormat: ">> %s", ToastBodyHint: " (more)"})

	term := testterm.New(80, 24)
	s := &toastSession{Session: testterm.NewSession(term, ""), tap: snoop.NewTap()}
	u := &user.User{ID: 5, Handle: "Reader"}
	sess := &session.BbsSession{ID: 1, NodeID: 1, User: u, Width: 80, Height: 24}
	reg.Register(sess)
	SetSessionOutput(s, s)
	SetSessionOutputMode(s, 0)
	sess.SetToastsReady(true)
	e.AttachToasts(s, sess)
	t.Cleanup(func() {
		resetSessionIH(s)
		DetachToasts(s)
		ClearSessionOutput(s)
		ClearSessionOutputMode(s)
		s.tap.Close()
	})
	_, _ = s.Write([]byte("\x1b[2J\x1b[HFULL SCREEN\x1b[24;1HBOTTOM TEXT\x1b[5;7H"))
	return &toastEnv{e: e, s: s, term: term, sess: sess, u: u}
}

// readKey runs a key read as a screen would, returning its result on a channel.
func (env *toastEnv) readKey() <-chan int {
	ih := getSessionIH(env.s)
	ch := make(chan int, 1)
	go func() {
		k, err := ih.ReadKey()
		if err != nil {
			k = -1
		}
		ch <- k
	}()
	return ch
}

func toastWaitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func TestToastOverlayShowsDuringKeyWaitAndRestoresScreen(t *testing.T) {
	env := newToastEnv(t)
	toastDwell = 10 * time.Second
	keys := env.readKey()

	if err := env.e.SessionRegistry.SendToast(env.e.toastStore(), env.u.ID, toast.Toast{Text: "Hello there"}); err != nil {
		t.Fatal(err)
	}
	toastWaitFor(t, "the overlay", func() bool { return strings.Contains(env.term.Row(24), ">> Hello there") })
	if row, col := env.term.Cursor(); row != 5 || col != 7 {
		t.Errorf("cursor at %d,%d under the overlay, want 5,7", row, col)
	}
	if strings.Contains(env.term.Row(24), "BOTTOM") {
		t.Errorf("overlay row kept old text: %q", env.term.Row(24))
	}
	toastWaitFor(t, "the toast leaving the queue", func() bool {
		q, _ := env.e.toastStore().Peek(env.u.ID)
		return len(q) == 0
	})

	// The key that clears the overlay still reaches the screen.
	env.s.Send("x")
	select {
	case k := <-keys:
		if k != 'x' {
			t.Errorf("read key %d, want x", k)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("key never reached the read")
	}
	if !strings.Contains(env.term.Row(24), "BOTTOM TEXT") || !strings.Contains(env.term.Row(1), "FULL SCREEN") {
		t.Errorf("screen not restored:\n%s", env.term.Snapshot())
	}
	if row, col := env.term.Cursor(); row != 5 || col != 7 {
		t.Errorf("cursor at %d,%d after restore, want 5,7", row, col)
	}
}

func TestToastOverlayClearsAfterDwellAndReadKeepsWaiting(t *testing.T) {
	env := newToastEnv(t)
	keys := env.readKey()
	env.sess.AddPage("Live one")
	env.sess.AddPage("Live two")

	toastWaitFor(t, "both toasts shown", func() bool {
		_, pending := env.sess.PeekPage()
		return !pending
	})
	toastWaitFor(t, "the screen restored", func() bool { return strings.Contains(env.term.Row(24), "BOTTOM TEXT") })
	select {
	case k := <-keys:
		t.Fatalf("read returned %d with no key pressed", k)
	default:
	}
	env.s.Send("y")
	if k := <-keys; k != 'y' {
		t.Errorf("read key %d, want y", k)
	}
}

func TestToastsDoNotExtendTheIdleTimeout(t *testing.T) {
	env := newToastEnv(t)
	toastDwell = time.Millisecond
	ih := getSessionIH(env.s)
	ih.SetSessionIdleTimeout(200 * time.Millisecond)

	stop := make(chan struct{})
	defer close(stop)
	go func() {
		for {
			select {
			case <-stop:
				return
			case <-time.After(5 * time.Millisecond):
				env.sess.AddPage("noise")
			}
		}
	}()
	done := make(chan error, 1)
	go func() {
		_, err := ih.ReadKey()
		done <- err
	}()
	select {
	case err := <-done:
		if !errors.Is(err, editor.ErrIdleTimeout) {
			t.Fatalf("read ended with %v, want idle timeout", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("toasts kept an idle caller connected")
	}
}

func TestToastsHeldWhenTheyCannotBeDrawn(t *testing.T) {
	cases := map[string]func(*toastEnv){
		"login not finished": func(env *toastEnv) { env.sess.SetToastsReady(false) },
		"teleconference":     func(env *toastEnv) { env.s.tap.SetMode(snoop.ModeTeleconf) },
		"door":               func(env *toastEnv) { env.s.tap.SetMode(snoop.ModeDoor) },
		"snapshot overflowed": func(env *toastEnv) {
			env.s.tap.Output([]byte(strings.Repeat("x", 2*snoop.CatchupLimit+1)))
		},
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			env := newToastEnv(t)
			setup(env)
			env.readKey()
			_ = env.e.SessionRegistry.SendToast(env.e.toastStore(), env.u.ID, toast.Toast{Text: "held"})
			time.Sleep(30 * time.Millisecond)
			if strings.Contains(env.term.Row(24), "held") {
				t.Errorf("toast drawn: %q", env.term.Row(24))
			}
			if q, _ := env.e.toastStore().Peek(env.u.ID); len(q) != 1 {
				t.Errorf("queue = %+v, want the toast still there", q)
			}
			env.s.Send("x")
		})
	}
}

func TestToastBodyAnnouncedOnceThenShownInlineLiterally(t *testing.T) {
	env := newToastEnv(t)
	toastDwell = time.Millisecond
	store := env.e.toastStore()
	env.readKey()
	_ = env.e.SessionRegistry.SendToast(store, env.u.ID, toast.Toast{Text: "Post refused", Body: "my |15text\nline two\x1b[2J"})

	toastWaitFor(t, "the announcement", func() bool {
		q := mustPeek(t, store, env.u.ID)
		return len(q) == 1 && env.sess.ToastAnnounced(q[0].ID)
	})
	if q := mustPeek(t, store, env.u.ID); len(q) != 1 {
		t.Fatalf("queue = %+v, want the body still kept", q)
	}
	env.s.Send("x")
	toastWaitFor(t, "the read to end", func() bool { return strings.Contains(env.term.Row(24), "BOTTOM") })

	body := string(toastBody("my |15text\nline two\x1b[2J"))
	if !strings.Contains(body, "my |15text\r\nline two[2J") {
		t.Errorf("body = %q, want pipe codes literal and the escape removed", body)
	}
}

func TestDeliverToastsInlineKeepsOrderAndRemoves(t *testing.T) {
	env := newToastEnv(t)
	store := env.e.toastStore()
	for _, text := range []string{"first", "second", "third"} {
		if err := store.Enqueue(env.u.ID, toast.Toast{Text: text}); err != nil {
			t.Fatal(err)
		}
	}
	terminal := term.NewTerminal(env.s, "")
	if !env.e.deliverToastsInline(env.s, terminal, 1, 0) {
		t.Fatal("nothing written")
	}
	shot := env.term.Snapshot()
	a, b, c := strings.Index(shot, "first"), strings.Index(shot, "second"), strings.Index(shot, "third")
	if a < 0 || b < a || c < b {
		t.Errorf("toasts out of order:\n%s", shot)
	}
	if q := mustPeek(t, store, env.u.ID); len(q) != 0 {
		t.Errorf("queue = %+v, want empty", q)
	}
	if env.e.deliverToastsInline(env.s, terminal, 1, 0) {
		t.Error("toasts shown a second time")
	}
}

func mustPeek(t *testing.T, store *toast.Store, id int) []toast.Toast {
	t.Helper()
	q, err := store.Peek(id)
	if err != nil {
		t.Fatal(err)
	}
	return q
}
