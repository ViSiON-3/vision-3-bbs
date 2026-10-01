package menu

import (
	"strings"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/editor"
	"github.com/ViSiON-3/vision-3-bbs/internal/editor/testterm"
	"github.com/ViSiON-3/vision-3-bbs/internal/snoop"
)

// chatSession is a scripted session carrying a tap. It embeds the concrete
// testterm.Session so the input handler finds SetReadInterrupt.
type chatSession struct {
	*testterm.Session
	tap *snoop.Tap
}

func (s *chatSession) Tap() *snoop.Tap { return s.tap }

type chatRig struct {
	term *testterm.Term
	sess *testterm.Session
	ih   *editor.InputHandler
	tap  *snoop.Tap
}

func newChatRig(t *testing.T, keys string) *chatRig {
	t.Helper()
	term := testterm.New(80, 25)
	sess := testterm.NewSession(term, keys)
	ih := editor.NewInputHandler(sess)
	t.Cleanup(ih.Close)
	tap := snoop.NewTap()
	w := tap.AttachAs("SysOp")
	t.Cleanup(w.Close)
	t.Cleanup(tap.Close)
	SetSysopChatEnv(chatEnv{
		theme: func() *config.ThemeConfig {
			return &config.ThemeConfig{ChatSysopColor: 11, ChatUserColor: 10}
		},
		strings: func() *config.StringsConfig {
			return &config.StringsConfig{SysopChatHeader: " %s chatting with %s ", SysopChatBack: "[back]"}
		},
		caller: func(*snoop.Tap) (string, int, int) { return "", 0, 0 },
	})
	return &chatRig{term: term, sess: sess, ih: ih, tap: tap}
}

// begin requests chat as SysOp and answers the request the way the session
// would, so the tap is chatting when it returns.
func (r *chatRig) begin(t *testing.T) {
	t.Helper()
	go func() {
		<-r.tap.BreakIn()
		r.tap.ChatBegan()
	}()
	if err := r.tap.RequestChat("SysOp", 2*time.Second); err != nil {
		t.Fatalf("RequestChat: %v", err)
	}
}

// run starts runSysopChat and returns a channel closed when it returns.
func (r *chatRig) run() <-chan struct{} {
	done := make(chan struct{})
	go func() {
		runSysopChat(r.ih, r.tap, r.term, ansi.OutputModeUTF8, 80, 25, "SysOp", "Caller")
		close(done)
	}()
	return done
}

func waitDone(t *testing.T, done <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal(what)
	}
}

func TestChatExitsOnCallerEscEsc(t *testing.T) {
	r := newChatRig(t, "")
	r.tap.Output([]byte("\x1b[2JMAIN MENU"))
	r.begin(t)
	done := r.run()
	// Both ESCs in one write, as a client sends a quick double press: the
	// first resolves when the second arrives, the second after the ESC
	// disambiguation window.
	r.sess.Send("hi\x1b\x1b")
	waitDone(t, done, "chat did not end on caller ESC ESC")
	if got := r.term.Row(1); !strings.Contains(got, "MAIN MENU") {
		t.Fatalf("screen not restored: %q", got)
	}
}

func TestChatEscThenKeyDoesNotExit(t *testing.T) {
	r := newChatRig(t, "")
	r.begin(t)
	done := r.run()
	r.sess.Send("\x1bx\x1b")
	time.Sleep(700 * time.Millisecond)
	select {
	case <-done:
		t.Fatal("chat ended on ESC x ESC")
	default:
	}
	_ = r.tap.StopChat("SysOp")
	waitDone(t, done, "chat ignored StopChat")
}

func TestChatEndsWhenSysopStops(t *testing.T) {
	r := newChatRig(t, "")
	r.tap.Output([]byte("\x1b[2JMAIN MENU"))
	r.begin(t)
	done := r.run()
	waitFor(t, func() bool { return strings.Contains(r.term.Row(13), "chatting with") }, "chat screen not drawn")
	_ = r.tap.StopChat("SysOp")
	waitDone(t, done, "chat ignored StopChat")
	if got := r.term.Row(1); !strings.Contains(got, "MAIN MENU") {
		t.Fatalf("screen not restored: %q", got)
	}
}

func TestChatRestoreFallsBackWhenSnapshotOverflowed(t *testing.T) {
	r := newChatRig(t, "")
	r.tap.Output(make([]byte, snoop.CatchupLimit+1))
	if _, over := r.tap.Snapshot(); !over {
		t.Fatal("catch-up buffer did not overflow")
	}
	r.begin(t)
	done := r.run()
	_ = r.tap.StopChat("SysOp")
	waitDone(t, done, "chat ignored StopChat")
	if got := r.term.Row(1); got != "[back]" {
		t.Fatalf("row 1 = %q; want [back]", got)
	}
}

func TestChatEndsWhenCallerDisconnects(t *testing.T) {
	r := newChatRig(t, "")
	r.tap.Output([]byte("\x1b[2JMAIN MENU"))
	r.begin(t)
	done := r.run()
	waitFor(t, func() bool { return strings.Contains(r.term.Row(13), "chatting with") }, "chat screen not drawn")
	r.tap.Close()
	waitDone(t, done, "chat hung after the tap closed")
	if strings.Contains(r.term.Snapshot(), "MAIN MENU") {
		t.Fatal("screen restored to a caller who has gone")
	}
}

func TestChatEndsOnReadError(t *testing.T) {
	r := newChatRig(t, "")
	r.tap.Output([]byte("\x1b[2JMAIN MENU"))
	r.begin(t)
	done := r.run()
	waitFor(t, func() bool { return strings.Contains(r.term.Row(13), "chatting with") }, "chat screen not drawn")
	r.ih.Close()
	waitDone(t, done, "chat hung after the input closed")
	if strings.Contains(r.term.Snapshot(), "MAIN MENU") {
		t.Fatal("wrote to a caller whose input is gone")
	}
}

func TestChatShowsBothSides(t *testing.T) {
	r := newChatRig(t, "")
	r.begin(t)
	done := r.run()
	r.tap.Inject("SysOp", []byte("from sysop"))
	r.sess.Send("from caller")
	waitFor(t, func() bool {
		return r.term.Row(1) == "from sysop" && r.term.Row(14) == "from caller"
	}, "both panes not rendered")
	if got := r.term.Row(13); !strings.Contains(got, " SysOp chatting with Caller ") {
		t.Fatalf("divider = %q", got)
	}
	if got := r.term.Cell(1, 1).Fg; got != 96 {
		t.Errorf("sysop pane fg = %d; want 96", got)
	}
	if got := r.term.Cell(14, 1).Fg; got != 92 {
		t.Errorf("caller pane fg = %d; want 92", got)
	}
	_ = r.tap.StopChat("SysOp")
	waitDone(t, done, "chat ignored StopChat")
}

func TestChatPaneEditing(t *testing.T) {
	r := newChatRig(t, "")
	r.begin(t)
	done := r.run()
	r.sess.Send("abd\x7fc\rnext")
	waitFor(t, func() bool {
		return r.term.Row(14) == "abc" && r.term.Row(15) == "next"
	}, "caller edits not rendered")
	_ = r.tap.StopChat("SysOp")
	waitDone(t, done, "chat ignored StopChat")
}

func TestChatPaneWrapsAndScrolls(t *testing.T) {
	var out strings.Builder
	p := &chatPane{w: &out, first: 1, last: 2, width: 3, row: 1, col: 1}
	p.put([]byte("abcdefg"))
	if p.row != 2 || p.col != 2 {
		t.Fatalf("cursor = %d,%d; want 2,2", p.row, p.col)
	}
	if !strings.Contains(out.String(), "\x1b[1;2r\x1b[2;1H\n\x1b[r") {
		t.Fatalf("no region scroll in %q", out.String())
	}
}

func TestServiceSysopChatSkipsExpiredRequest(t *testing.T) {
	r := newChatRig(t, "")
	s := &chatSession{r.sess, r.tap}
	SetSessionOutput(s, r.term)
	t.Cleanup(func() { ClearSessionOutput(s) })
	serviceSysopChat(s, r.ih, r.tap)
	if got := r.term.Snapshot(); got != "" {
		t.Fatalf("drew chat with no pending request:\n%s", got)
	}
}

func TestBreakInMidLinePreservesPartialInput(t *testing.T) {
	term := testterm.New(80, 25)
	tap := snoop.NewTap()
	t.Cleanup(tap.Close)
	w := tap.AttachAs("SysOp")
	t.Cleanup(w.Close)
	sess := testterm.NewSession(term, "")
	s := &chatSession{sess, tap}
	newChatRig(t, "") // installs the chat env
	SetSessionOutput(s, term)
	SetSessionOutputMode(s, ansi.OutputModeUTF8)
	t.Cleanup(func() {
		resetSessionIH(s)
		ClearSessionOutput(s)
		ClearSessionOutputMode(s)
	})
	ih := getSessionIH(s)

	tap.Output([]byte("\x1b[2JName: ab"))
	keys := make(chan int, 16)
	line := make(chan string, 1)
	go func() {
		var b []byte
		for {
			k, err := ih.ReadKey()
			if err != nil || k == editor.KeyEnter {
				line <- string(b)
				return
			}
			keys <- k
			b = append(b, byte(k))
		}
	}()
	sess.Send("ab")
	for range 2 {
		select {
		case <-keys:
		case <-time.After(3 * time.Second):
			t.Fatal("prompt did not read ab")
		}
	}

	if err := tap.RequestChat("SysOp", 2*time.Second); err != nil {
		t.Fatalf("RequestChat: %v", err)
	}
	waitFor(t, func() bool { return strings.Contains(term.Row(13), "chatting with") }, "chat screen not drawn")
	if err := tap.StopChat("SysOp"); err != nil {
		t.Fatalf("StopChat: %v", err)
	}
	waitFor(t, func() bool { return !tap.Chatting() }, "chat did not end")
	if got := term.Row(1); !strings.Contains(got, "Name: ab") {
		t.Fatalf("partial line not restored: %q", got)
	}
	sess.Send("c\r")
	select {
	case got := <-line:
		if got != "abc" {
			t.Fatalf("line = %q; want abc", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("prompt did not finish the line")
	}
}
