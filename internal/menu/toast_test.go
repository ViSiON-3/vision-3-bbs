package menu

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mattn/go-runewidth"
	"golang.org/x/term"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
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
			keys := env.readKey()
			_ = env.e.SessionRegistry.SendToast(env.e.toastStore(), env.u.ID, toast.Toast{Text: "held"})
			time.Sleep(30 * time.Millisecond)
			if strings.Contains(env.term.Row(24), "held") {
				t.Errorf("toast drawn: %q", env.term.Row(24))
			}
			if q, _ := env.e.toastStore().Peek(env.u.ID); len(q) != 1 {
				t.Errorf("queue = %+v, want the toast still there", q)
			}
			env.s.Send("x")
			<-keys
		})
	}
}

func TestToastBodyAnnouncedOnceThenShownInlineLiterally(t *testing.T) {
	env := newToastEnv(t)
	toastDwell = time.Millisecond
	store := env.e.toastStore()
	keys := env.readKey()
	_ = env.e.SessionRegistry.SendToast(store, env.u.ID, toast.Toast{Text: "Post refused", Body: "my |15text\nline two\x1b[2J"})

	toastWaitFor(t, "the announcement", func() bool {
		q := mustPeek(t, store, env.u.ID)
		return len(q) == 1 && env.sess.ToastAnnounced(q[0].ID)
	})
	if q := mustPeek(t, store, env.u.ID); len(q) != 1 {
		t.Fatalf("queue = %+v, want the body still kept", q)
	}
	env.s.Send("x")
	<-keys
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

func TestToastEdgeRow(t *testing.T) {
	cases := []struct {
		name                 string
		edge                 string
		offset, height, want int
	}{
		{"unset edge is the bottom", "", 0, 24, 24},
		{"bottom follows the terminal", config.ToastEdgeBottom, 0, 50, 50},
		{"bottom offset moves up", config.ToastEdgeBottom, 2, 24, 22},
		{"top", config.ToastEdgeTop, 0, 24, 1},
		{"top offset moves down", config.ToastEdgeTop, 2, 24, 3},
		{"bottom offset stops at the top", config.ToastEdgeBottom, 50, 24, 1},
		{"top offset stops at the bottom", config.ToastEdgeTop, 50, 24, 24},
		{"negative offset from a hand edit", config.ToastEdgeBottom, -3, 24, 24},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := toastEdgeRow(c.edge, c.offset, c.height); got != c.want {
				t.Errorf("got %d, want %d", got, c.want)
			}
		})
	}
}

func TestToastCenterCol(t *testing.T) {
	cases := []struct {
		name                           string
		textLen, konfig, term, wantCol int
	}{
		{"on the terminal when Konfig is unset", 10, 0, 80, 36},
		{"on the Konfig width", 10, 40, 80, 16},
		{"Konfig wider than the terminal", 10, 132, 80, 36},
		{"text wider than the screen", 100, 0, 80, 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := toastCenterCol(c.textLen, c.konfig, c.term); got != c.wantCol {
				t.Errorf("got %d, want %d", got, c.wantCol)
			}
		})
	}
}

func TestToastOverlayCentersOnKonfigWidth(t *testing.T) {
	env := newToastEnv(t)
	toastDwell = 10 * time.Second
	cfg := env.e.GetServerConfig()
	cfg.ToastCenter = true
	cfg.ToastCol = 70 // ignored while centering
	env.e.SetServerConfig(cfg)
	env.sess.User.ScreenWidth = 40
	keys := env.readKey()

	env.sess.AddPage("Mid")
	toastWaitFor(t, "the overlay", func() bool { return strings.Contains(env.term.Row(24), ">> Mid") })
	// ">> Mid" is 6 wide: (40-6)/2+1.
	if got := env.term.Cell(24, 18).Rune; got != '>' {
		t.Errorf("column 18 = %q, want the toast's start; row = %q", got, env.term.Row(24))
	}
	env.s.Send("x")
	<-keys
}

func TestToastAnchorOverridesCenter(t *testing.T) {
	env := newToastEnv(t)
	toastDwell = 10 * time.Second
	cfg := env.e.GetServerConfig()
	cfg.ToastCenter = true
	env.e.SetServerConfig(cfg)
	env.sess.SetToastAnchor(3, 9)
	keys := env.readKey()

	env.sess.AddPage("Art")
	toastWaitFor(t, "the overlay", func() bool { return strings.Contains(env.term.Row(3), ">> Art") })
	if got := env.term.Cell(3, 9).Rune; got != '>' {
		t.Errorf("column 9 = %q, want the toast's start", got)
	}
	env.s.Send("x")
	<-keys
}

func TestToastPlacement(t *testing.T) {
	cases := []struct {
		name                 string
		cfgRow, cfgCol, w, h int
		wantRow, wantCol     int
	}{
		{"column 0 reads as 1", 24, 0, 80, 24, 24, 1},
		{"position used as set", 3, 10, 80, 24, 3, 10},
		{"row past the bottom", 40, 1, 80, 24, 24, 1},
		{"column too far right", 1, 75, 80, 24, 1, 60},
		{"narrow terminal", 1, 30, 15, 24, 1, 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			row, col := toastPlacement(c.cfgRow, c.cfgCol, c.w, c.h)
			if row != c.wantRow || col != c.wantCol {
				t.Errorf("got %d,%d, want %d,%d", row, col, c.wantRow, c.wantCol)
			}
		})
	}
}

func TestToastOverlayDrawsAtConfiguredPosition(t *testing.T) {
	env := newToastEnv(t)
	toastDwell = 10 * time.Second
	cfg := env.e.GetServerConfig()
	cfg.ToastEdge, cfg.ToastOffset, cfg.ToastCol = config.ToastEdgeTop, 0, 6
	env.e.SetServerConfig(cfg)
	keys := env.readKey()

	env.sess.AddPage("Placed")
	toastWaitFor(t, "the overlay", func() bool { return strings.Contains(env.term.Row(1), ">> Placed") })
	// Text left of the column stays; the row's old text from there on goes.
	if got := env.term.Row(1); !strings.HasPrefix(got, "FULL >> Placed") || strings.Contains(got, "SCREEN") {
		t.Errorf("row 1 = %q", got)
	}
	if !strings.Contains(env.term.Row(24), "BOTTOM TEXT") {
		t.Errorf("bottom row touched: %q", env.term.Row(24))
	}
	if row, col := env.term.Cursor(); row != 5 || col != 7 {
		t.Errorf("cursor at %d,%d, want 5,7", row, col)
	}
	env.s.Send("x")
	<-keys
	if !strings.Contains(env.term.Row(1), "FULL SCREEN") {
		t.Errorf("row 1 not restored: %q", env.term.Row(1))
	}
}

func TestToastAnchorFromMenuArtOverridesConfiguredPosition(t *testing.T) {
	env := newToastEnv(t)
	toastDwell = 10 * time.Second
	cfg := env.e.GetServerConfig()
	cfg.ToastEdge, cfg.ToastOffset, cfg.ToastCol = config.ToastEdgeBottom, 4, 1
	env.e.SetServerConfig(cfg)

	res, err := ansi.ProcessAnsiAndExtractCoords([]byte("\x1b[3;9H~TO"), ansi.OutputModeCP437)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(res.DisplayBytes), "TO") {
		t.Errorf("code left in the art: %q", res.DisplayBytes)
	}

	// Without a cleared screen the art's coordinates mean nothing.
	env.e.setToastAnchor(1, res, false)
	if _, _, ok := env.sess.ToastAnchor(); ok {
		t.Error("anchor set on a menu that does not clear the screen")
	}

	env.e.setToastAnchor(1, res, true)
	keys := env.readKey()
	env.sess.AddPage("Anchored")
	toastWaitFor(t, "the overlay", func() bool { return strings.Contains(env.term.Row(3), ">> Anchored") })
	if strings.Contains(env.term.Row(20), "Anchored") {
		t.Errorf("drawn at the configured row too: %q", env.term.Row(20))
	}
	if got := env.term.Cell(3, 9).Rune; got != '>' {
		t.Errorf("column 9 of row 3 = %q, want the toast's start", got)
	}
	env.s.Send("x")
	<-keys

	// Leaving the menu hands toasts back to the configured position.
	env.e.clearToastAnchor(1)
	keys = env.readKey()
	env.sess.AddPage("Back")
	toastWaitFor(t, "the overlay", func() bool { return strings.Contains(env.term.Row(20), ">> Back") })
	env.s.Send("x")
	<-keys
}

// A toast's text comes from another caller, so screen codes in it print as text.
func TestToastTextCannotRunScreenCodes(t *testing.T) {
	env := newToastEnv(t)
	var out bytes.Buffer
	if _, err := env.e.drawToastOverlay(env.s, &out, env.sess, toast.Toast{Text: "a|CLb|CRc|Pd|04e"}); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if strings.Contains(got, "\x1b[2J") || !strings.Contains(got, "a|CLb|CRc|Pd") {
		t.Errorf("overlay = %q, want the screen codes as text", got)
	}
	if !strings.Contains(got, string(ansi.ReplacePipeCodes([]byte("|04")))+"e") {
		t.Errorf("overlay = %q, want the colour code expanded", got)
	}

	store := env.e.toastStore()
	if err := store.Enqueue(env.u.ID, toast.Toast{Text: "stored|CLtoast"}); err != nil {
		t.Fatal(err)
	}
	env.sess.AddPage("live|CLpage")
	terminal := term.NewTerminal(env.s, "")
	if !env.e.deliverToastsInline(env.s, terminal, 1, 0) {
		t.Fatal("nothing written")
	}
	shot := env.term.Snapshot()
	if !strings.Contains(shot, "FULL SCREEN") {
		t.Errorf("inline delivery cleared the screen:\n%s", shot)
	}
	if !strings.Contains(shot, "stored|CLtoast") || !strings.Contains(shot, "live|CLpage") {
		t.Errorf("screen codes not shown as text:\n%s", shot)
	}
}

// Page senders' handles can hold pipes, and their own text must not be taken for
// colour or screen codes.
func TestPageEscapesTheSendersHandle(t *testing.T) {
	env := newMenuEnv(t)
	chatcovNode(env, 1, env.caller)
	chatcovNode(env, 2, env.sysop)
	env.caller.Handle = "Caller|04"

	if r := env.run(runPage, env.caller, "", "2\rhi\r"); r.err != nil {
		t.Fatal(r.err)
	}
	queued, err := env.e.toastStore().Peek(env.sysop.ID)
	if err != nil || len(queued) != 1 {
		t.Fatalf("queue = %+v, %v", queued, err)
	}
	if got := string(toastPipes(queued[0].Text)); !strings.Contains(got, "Caller|04") {
		t.Errorf("rendered page = %q, want the handle as typed", got)
	}
}

// The overlay measures terminal cells: a wide character takes two.
func TestToastOverlayClipsByColumns(t *testing.T) {
	env := newToastEnv(t)
	SetSessionOutputMode(env.s, ansi.OutputModeUTF8)
	var out bytes.Buffer
	clipped, err := env.e.drawToastOverlay(env.s, &out, env.sess, toast.Toast{Text: strings.Repeat("漢", 60)})
	if err != nil {
		t.Fatal(err)
	}
	if !clipped {
		t.Error("clipped = false for text wider than the row")
	}
	_, line, _ := strings.Cut(out.String(), ">> ")
	line, _, _ = strings.Cut(line, "\x1b[0m\x1b8")
	if w := runewidth.StringWidth(ansi.StripAnsi(line)); w > 79 {
		t.Errorf("overlay is %d columns wide, want at most 79: %q", w, line)
	}
}

// A toast too long for the row stays queued, once drawn, for all of it to show
// inline.
func TestLongToastDrawnOnceThenShownWholeInline(t *testing.T) {
	env := newToastEnv(t)
	toastDwell = time.Millisecond
	store := env.e.toastStore()
	long := "HEAD " + strings.Repeat("x ", 80) + "TAIL"
	keys := env.readKey()
	if err := env.e.SessionRegistry.SendToast(store, env.u.ID, toast.Toast{Text: long}); err != nil {
		t.Fatal(err)
	}
	toastWaitFor(t, "the first drawing", func() bool {
		q := mustPeek(t, store, env.u.ID)
		return len(q) == 1 && env.sess.ToastAnnounced(q[0].ID)
	})
	time.Sleep(20 * time.Millisecond)
	if q := mustPeek(t, store, env.u.ID); len(q) != 1 {
		t.Fatalf("queue = %+v, want the long toast kept", q)
	}
	if _, _, ok := env.e.nextOverlayToast(env.sess); ok {
		t.Error("long toast offered to the overlay a second time")
	}
	env.s.Send("x")
	<-keys

	// A board with only lightbar menus has no prompt, so it goes between menus.
	env.s.Send("\r")
	terminal := term.NewTerminal(env.s, "")
	env.e.deliverToastBodies(env.s, terminal, 1, 0, 80, 24)
	if !strings.Contains(env.term.Snapshot(), "TAIL") {
		t.Errorf("the end of the toast never shown:\n%s", env.term.Snapshot())
	}
	if q := mustPeek(t, store, env.u.ID); len(q) != 0 {
		t.Errorf("queue = %+v, want empty", q)
	}
}

func TestLongLivePageHeldForInline(t *testing.T) {
	env := newToastEnv(t)
	toastDwell = time.Millisecond
	long := "HEAD " + strings.Repeat("x ", 80) + "TAIL"
	keys := env.readKey()
	env.sess.AddPage(long)
	toastWaitFor(t, "the page held", func() bool {
		_, held := env.sess.PeekHeldPage()
		return held
	})
	if _, pending := env.sess.PeekPage(); pending {
		t.Error("held page still offered to the overlay")
	}
	env.s.Send("x")
	<-keys
	if !env.e.deliverToastsInline(env.s, term.NewTerminal(env.s, ""), 1, 0) {
		t.Fatal("nothing written")
	}
	if !strings.Contains(env.term.Snapshot(), "TAIL") {
		t.Errorf("the end of the page never shown:\n%s", env.term.Snapshot())
	}
	if _, held := env.sess.PeekHeldPage(); held {
		t.Error("page still held after delivery")
	}
}

// With the store refusing writes, a shown toast cannot leave the queue. It must
// not be drawn again; the next prompt retries the removal without showing it.
func TestFailedRemoveDoesNotShowTheToastAgain(t *testing.T) {
	env := newToastEnv(t)
	store := env.e.toastStore()
	if err := store.Enqueue(env.u.ID, toast.Toast{ID: "a", Text: "only once"}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(env.e.GetServerConfig().DataDir, "toasts.json")
	good, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{broken"), 0o644); err != nil {
		t.Fatal(err)
	}
	env.e.settleOverlayToast(env.sess, toast.Toast{ID: "a", Text: "only once"}, true, false)
	if err := os.WriteFile(path, good, 0o644); err != nil {
		t.Fatal(err)
	}

	if q, _, ok := env.e.nextOverlayToast(env.sess); ok {
		t.Errorf("overlay offered %+v again after a failed remove", q)
		store.Release(q.ID)
	}
	before := env.term.Snapshot()
	env.e.deliverToastsInline(env.s, term.NewTerminal(env.s, ""), 1, 0)
	if env.term.Snapshot() != before {
		t.Errorf("toast written again:\n%s", env.term.Snapshot())
	}
	if q := mustPeek(t, store, env.u.ID); len(q) != 0 {
		t.Errorf("queue = %+v, want the removal retried", q)
	}
}

// A dwell ends with the read's wait, however long toastDwell is.
func TestToastDwellStopsAtTheIdleTimeout(t *testing.T) {
	env := newToastEnv(t)
	toastDwell = time.Hour
	ih := getSessionIH(env.s)
	ih.SetSessionIdleTimeout(100 * time.Millisecond)
	env.sess.AddPage("long dwell")
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
		env.s.Send("x")
		t.Fatal("the overlay's dwell outlasted the idle timeout")
	}
}

// Toasts arriving faster than they dwell must not hold the read in the hook.
func TestSteadyToastsYieldToTheIdleTimeout(t *testing.T) {
	env := newToastEnv(t)
	toastDwell = 20 * time.Millisecond
	ih := getSessionIH(env.s)
	ih.SetSessionIdleTimeout(150 * time.Millisecond)
	env.sess.AddPage("first")
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
		t.Fatal("a steady stream of toasts starved the idle timeout")
	}
}

// A toast an overflowing screen held has no other wake coming, so a later screen
// must call for it.
func TestHeldToastRearmedForTheNextScreen(t *testing.T) {
	env := newToastEnv(t)
	toastDwell = 10 * time.Second
	env.s.tap.Output([]byte(strings.Repeat("x", 2*snoop.CatchupLimit+1)))
	keys := env.readKey()
	if err := env.e.SessionRegistry.SendToast(env.e.toastStore(), env.u.ID, toast.Toast{Text: "held"}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(30 * time.Millisecond)
	if strings.Contains(env.term.Row(24), "held") {
		t.Fatalf("toast drawn over an overflowed screen: %q", env.term.Row(24))
	}

	_, _ = env.s.Write([]byte("\x1b[2J\x1b[HNEW SCREEN"))
	env.e.rearmToasts(1)
	toastWaitFor(t, "the held toast", func() bool { return strings.Contains(env.term.Row(24), ">> held") })
	env.s.Send("x")
	<-keys
}
