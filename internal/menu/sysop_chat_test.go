package menu

import (
	"fmt"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/editor"
	"github.com/ViSiON-3/vision-3-bbs/internal/editor/testterm"
	"github.com/ViSiON-3/vision-3-bbs/internal/session"
	"github.com/ViSiON-3/vision-3-bbs/internal/snoop"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
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
func (r *chatRig) run() <-chan struct{} { return r.runSized(80, 25) }

func (r *chatRig) runSized(width, height int) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		runSysopChat(r.ih, r.tap, r.term, ansi.OutputModeUTF8, width, height, "SysOp", "Caller")
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

// promptRig is a caller part-way through a line prompt on a session wired
// for break-in through getSessionIH.
type promptRig struct {
	term *testterm.Term
	sess *testterm.Session
	tap  *snoop.Tap
	keys chan int
	line chan string
}

func newPromptRig(t *testing.T) *promptRig {
	t.Helper()
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
	r := &promptRig{term: term, sess: sess, tap: tap, keys: make(chan int, 16), line: make(chan string, 1)}
	go func() {
		var b []byte
		for {
			k, err := ih.ReadKey()
			if err != nil || k == editor.KeyEnter {
				r.line <- string(b)
				return
			}
			r.keys <- k
			b = append(b, byte(k))
		}
	}()
	sess.Send("ab")
	for range 2 {
		r.nextKey(t)
	}
	return r
}

func (r *promptRig) nextKey(t *testing.T) int {
	t.Helper()
	select {
	case k := <-r.keys:
		return k
	case <-time.After(3 * time.Second):
		t.Fatal("prompt read no key")
		return 0
	}
}

func (r *promptRig) openChat(t *testing.T) {
	t.Helper()
	if err := r.tap.RequestChat("SysOp", 2*time.Second); err != nil {
		t.Fatalf("RequestChat: %v", err)
	}
	waitFor(t, func() bool { return strings.Contains(r.term.Row(13), "chatting with") }, "chat screen not drawn")
}

func (r *promptRig) finish(t *testing.T, rest, want string) {
	t.Helper()
	r.sess.Send(rest)
	select {
	case got := <-r.line:
		if got != want {
			t.Fatalf("line = %q; want %q", got, want)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("prompt did not finish the line")
	}
}

func TestBreakInMidLinePreservesPartialInput(t *testing.T) {
	r := newPromptRig(t)
	r.openChat(t)
	if err := r.tap.StopChat("SysOp"); err != nil {
		t.Fatalf("StopChat: %v", err)
	}
	waitFor(t, func() bool { return !r.tap.Chatting() }, "chat did not end")
	if got := r.term.Row(1); !strings.Contains(got, "Name: ab") {
		t.Fatalf("partial line not restored: %q", got)
	}
	r.finish(t, "c\r", "abc")
}

func TestChatEscEscKeyGoesToPromptFirst(t *testing.T) {
	r := newPromptRig(t)
	r.openChat(t)
	// x arrives inside the ESC window, so ReadKey pushes it back.
	r.sess.Send("\x1b\x1bx")
	if k := r.nextKey(t); k != 'x' {
		t.Fatalf("prompt key after chat = %q; want x", k)
	}
	r.finish(t, "z\r", "abxz")
}

// paneOn is a pane drawn on its own terminal, for layout tests.
func paneOn(width, rows int) (*testterm.Term, *chatPane) {
	term := testterm.New(40, rows)
	return term, &chatPane{w: term, first: 1, last: rows, width: width, row: 1, col: 1}
}

func TestChatPaneWordWrap(t *testing.T) {
	term, p := paneOn(10, 5)
	p.put([]byte("one two thr"))
	if r1, r2 := term.Row(1), term.Row(2); r1 != "one two" || r2 != "thr" {
		t.Fatalf("rows = %q, %q; want \"one two\", \"thr\"", r1, r2)
	}
	p.put([]byte("\b\bree"))
	if r2 := term.Row(2); r2 != "tree" {
		t.Fatalf("row 2 after backspace = %q; want tree", r2)
	}
}

func TestChatPaneLongWordBreaksAtWidth(t *testing.T) {
	term, p := paneOn(5, 5)
	p.put([]byte("abcdefgh"))
	if r1, r2 := term.Row(1), term.Row(2); r1 != "abcde" || r2 != "fgh" {
		t.Fatalf("rows = %q, %q; want abcde, fgh", r1, r2)
	}
}

func TestChatPaneCRLFIsOneNewline(t *testing.T) {
	term, p := paneOn(20, 5)
	p.put([]byte("a\r\nb\r"))
	p.put([]byte("\x00c\r"))
	p.put([]byte("\nd"))
	got := []string{term.Row(1), term.Row(2), term.Row(3), term.Row(4)}
	want := []string{"a", "b", "c", "d"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("rows = %q; want %q", got, want)
		}
	}
}

var hhmm = regexp.MustCompile(`─ [0-2][0-9]:[0-5][0-9]$`)

func TestChatDividerShowsTime(t *testing.T) {
	r := newChatRig(t, "")
	r.begin(t)
	done := r.run()
	waitFor(t, func() bool { return strings.Contains(r.term.Row(13), "chatting with") }, "chat screen not drawn")
	// Row trims the trailing blank of the " HH:MM " field.
	if got := r.term.Row(13); !hhmm.MatchString(got) || len([]rune(got)) != 79 {
		t.Fatalf("divider = %q; want the fill then HH:MM in the last field", got)
	}
	_ = r.tap.StopChat("SysOp")
	waitDone(t, done, "chat ignored StopChat")
}

func TestChatTinyScreenKeepsPanesApart(t *testing.T) {
	r := newChatRig(t, "")
	r.begin(t)
	done := r.runSized(80, 2)
	r.tap.Inject("SysOp", []byte("from sysop"))
	r.sess.Send("from caller")
	waitFor(t, func() bool {
		return r.term.Row(1) == "from sysop" && strings.Contains(r.term.Row(3), "chatting with") &&
			r.term.Row(4) == "from caller"
	}, "panes overlap on a 2-row screen")
	_ = r.tap.StopChat("SysOp")
	waitDone(t, done, "chat ignored StopChat")
}

func TestChatTimeIsCreditedToTimeLimit(t *testing.T) {
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
		ClearSessionIdleTimeout(s)
	})
	limitEnds := time.Now().Add(500 * time.Millisecond)
	applySessionDeadline(s, limitEnds)
	ih := getSessionIH(s)
	keys := make(chan error, 1)
	go func() {
		_, err := ih.ReadKey()
		keys <- err
	}()

	if err := tap.RequestChat("SysOp", 2*time.Second); err != nil {
		t.Fatalf("RequestChat: %v", err)
	}
	time.Sleep(800 * time.Millisecond)
	if err := tap.StopChat("SysOp"); err != nil {
		t.Fatalf("StopChat: %v", err)
	}
	waitFor(t, func() bool { return !tap.Chatting() }, "chat did not end")

	if c := chatCredit(s); c < 800*time.Millisecond {
		t.Fatalf("chat credit = %v, want >= 800ms", c)
	}
	// The next menu re-arms the limit from the session start, as Run does.
	applySessionDeadline(s, limitEnds)
	if timeLimitReached(s) {
		t.Fatal("caller logged off for time spent in chat")
	}
	d, _ := sessionDeadlines.Load(s)
	if left := time.Until(d.(time.Time)); left < 300*time.Millisecond {
		t.Fatalf("time left after chat = %v, want the chat credited back", left)
	}
	sess.Send("x")
	select {
	case err := <-keys:
		if err != nil {
			t.Fatalf("read after chat = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("read after chat did not get the key")
	}
}

func TestCallerEndedChatIsReported(t *testing.T) {
	r := newPromptRig(t)
	ended := make(chan *snoop.Tap, 1)
	env := *sysopChat.Load()
	env.ended = func(tp *snoop.Tap) { ended <- tp }
	SetSysopChatEnv(env)
	if err := r.tap.TakeKeyboard("SysOp"); err != nil {
		t.Fatal(err)
	}
	r.openChat(t)
	r.sess.Send("\x1b\x1b")
	select {
	case tp := <-ended:
		if tp != r.tap {
			t.Fatal("ended reported for another tap")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("caller-ended chat not reported")
	}
	if h := r.tap.KeyboardHolder(); h != "" {
		t.Fatalf("holder after caller ended chat = %q; want none", h)
	}
	if n := r.tap.Inject("SysOp", []byte("ok, bye\r")); n != 0 {
		t.Fatal("sysop keys reached the caller's prompt after chat")
	}
	r.finish(t, "c\r", "abc")
}

type chatEndPager struct {
	fakePager
	mu    sync.Mutex
	ended []string
}

func (p *chatEndPager) ChatEnded(node int, handle string) {
	p.mu.Lock()
	p.ended = append(p.ended, fmt.Sprintf("%d %s", node, handle))
	p.mu.Unlock()
}

func TestExecutorChatEndedNotifiesPager(t *testing.T) {
	env := newMenuEnv(t)
	p := &chatEndPager{}
	env.e.Pager = p
	tap := snoop.NewTap()
	t.Cleanup(tap.Close)
	env.e.SessionRegistry.Register(&session.BbsSession{NodeID: 4, Tap: tap, User: &user.User{Handle: "caller"}})
	env.e.chatEnded(snoop.NewTap())
	env.e.chatEnded(tap)
	if got := strings.Join(p.ended, ","); got != "4 caller" {
		t.Fatalf("ChatEnded calls = %q, want %q", got, "4 caller")
	}
}
