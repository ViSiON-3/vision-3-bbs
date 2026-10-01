package wfcui

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/muesli/cancelreader"

	"github.com/ViSiON-3/vision-3-bbs/internal/admin"
	"github.com/ViSiON-3/vision-3-bbs/internal/snoop"
)

type fakeRaw struct{ w, h int }

func (f fakeRaw) MakeRaw() (func(), error) { return func() {}, nil }
func (f fakeRaw) Size() (int, int, error)  { return f.w, f.h, nil }

type fakeCtl struct {
	mu      sync.Mutex
	calls   []string
	chatErr error
}

func (f *fakeCtl) rec(s string) {
	f.mu.Lock()
	f.calls = append(f.calls, s)
	f.mu.Unlock()
}

func (f *fakeCtl) TypeIn(on bool) error {
	f.rec(map[bool]string{true: "type+", false: "type-"}[on])
	return nil
}

func (f *fakeCtl) Chat(on bool) error {
	f.rec(map[bool]string{true: "chat+", false: "chat-"}[on])
	return f.chatErr
}

func (f *fakeCtl) got() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return strings.Join(f.calls, ",")
}

type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func (s *syncBuf) waitFor(t *testing.T, sub string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(s.String(), sub) {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("output never contained %q: %q", sub, s.String())
}

type snoopRig struct {
	cmd    *snoopCmd
	server net.Conn
	in     *io.PipeWriter
	out    *syncBuf
	ctl    *fakeCtl
	done   chan error
}

func newRig(t *testing.T, hdr admin.SnoopHeader, w, h int) *snoopRig {
	t.Helper()
	a, server := net.Pipe()
	t.Cleanup(func() { server.Close() })
	r := &snoopRig{server: server, out: &syncBuf{}, ctl: &fakeCtl{}, done: make(chan error, 1)}
	inR, inW := io.Pipe()
	t.Cleanup(func() { inW.Close() })
	r.in = inW
	r.cmd = newSnoopCmd(admin.NewSnoopStream(hdr, a), r.ctl, 3, fakeRaw{w: w, h: h})
	r.cmd.SetStdin(inR)
	r.cmd.SetStdout(r.out)
	go func() { r.done <- r.cmd.Run() }()
	return r
}

func (r *snoopRig) send(t *testing.T, s string) {
	t.Helper()
	if _, err := r.in.Write([]byte(s)); err != nil {
		t.Fatal(err)
	}
}

func (r *snoopRig) wait(t *testing.T) {
	t.Helper()
	select {
	case err := <-r.done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("snoop did not return")
	}
}

// reads collects what the server end receives.
func (r *snoopRig) reads() chan string {
	ch := make(chan string, 16)
	go func() {
		b := make([]byte, 64)
		for {
			n, err := r.server.Read(b)
			if err != nil {
				return
			}
			ch <- string(b[:n])
		}
	}()
	return ch
}

func expect(t *testing.T, ch chan string, want string) {
	t.Helper()
	select {
	case s := <-ch:
		if s != want {
			t.Fatalf("forwarded %q, want %q", s, want)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("nothing forwarded, want %q", want)
	}
}

var utf8Hdr = admin.SnoopHeader{OutputMode: "utf8", Width: 80, Height: 25, Handle: "caller"}

func TestSnoopWatchDropsKeysAndExitsOnAltX(t *testing.T) {
	r := newRig(t, utf8Hdr, 100, 40)
	got := r.reads()
	r.send(t, "abc")
	_, _ = r.server.Write([]byte("MENU"))
	r.out.waitFor(t, "MENU")
	r.out.waitFor(t, "NODE 3")
	r.send(t, "\x1bx")
	r.wait(t)
	select {
	case b := <-got:
		t.Fatalf("watch mode forwarded %q", b)
	default:
	}
}

func TestSnoopTypeInForwardsAndReleasesOnExit(t *testing.T) {
	r := newRig(t, admin.SnoopHeader{OutputMode: "utf8", Width: 80, Height: 25}, 80, 25)
	got := r.reads()
	r.send(t, "\x1bt")
	r.send(t, "q")
	expect(t, got, "q")
	r.send(t, "\x1b\x1b")
	expect(t, got, "\x1b")
	r.send(t, "\x1bx")
	r.wait(t)
	if c := r.ctl.got(); c != "type+,type-" {
		t.Fatalf("calls %s", c)
	}
}

func TestSnoopLoneEscForwardedAfterTimeout(t *testing.T) {
	r := newRig(t, utf8Hdr, 80, 25)
	got := r.reads()
	r.send(t, "\x1bt")
	r.send(t, "\x1b")
	expect(t, got, "\x1b")
	r.send(t, "\x1bx")
	r.wait(t)
}

func TestSnoopEscThenOtherByteForwardsBoth(t *testing.T) {
	r := newRig(t, utf8Hdr, 80, 25)
	got := r.reads()
	r.send(t, "\x1bt")
	r.send(t, "\x1bz")
	expect(t, got, "\x1bz")
	r.send(t, "\x1bx")
	r.wait(t)
}

func TestSnoopEscThenOtherByteDroppedInWatch(t *testing.T) {
	r := newRig(t, utf8Hdr, 80, 25)
	got := r.reads()
	r.send(t, "\x1bz")
	r.send(t, "\x1bx")
	r.wait(t)
	select {
	case b := <-got:
		t.Fatalf("watch mode forwarded %q", b)
	default:
	}
}

func TestSnoopChatToggleAndRelease(t *testing.T) {
	r := newRig(t, utf8Hdr, 80, 25)
	r.send(t, "\x1bc")
	r.send(t, "\x1bc")
	r.send(t, "\x1bc")
	r.send(t, "\x1bx")
	r.wait(t)
	if c := r.ctl.got(); c != "chat+,chat-,chat+,chat-" {
		t.Fatalf("calls %s", c)
	}
}

func TestSnoopTypeInIgnoredDuringChat(t *testing.T) {
	r := newRig(t, utf8Hdr, 80, 25)
	r.send(t, "\x1bc")
	r.send(t, "\x1bt")
	r.send(t, "\x1bx")
	r.wait(t)
	if c := r.ctl.got(); c != "chat+,chat-" {
		t.Fatalf("calls %s", c)
	}
}

func TestSnoopControlErrorShownNotFatal(t *testing.T) {
	r := newRig(t, utf8Hdr, 100, 40)
	r.ctl.chatErr = errors.New("chat already ended")
	r.send(t, "\x1bc")
	r.out.waitFor(t, "chat already ended")
	r.send(t, "\x1bx")
	r.wait(t)
}

func TestSnoopStatusHiddenOnShortTerminal(t *testing.T) {
	r := newRig(t, utf8Hdr, 80, 24)
	_, _ = r.server.Write([]byte("x"))
	r.out.waitFor(t, "x")
	r.send(t, "\x1bx")
	r.wait(t)
	if strings.Contains(r.out.String(), "NODE 3") {
		t.Fatal("status bar drawn over a short terminal")
	}
}

func TestSnoopStatusToggleAndBounds(t *testing.T) {
	r := newRig(t, utf8Hdr, 80, 24)
	r.send(t, "\x1bh")
	r.out.waitFor(t, "NODE 3")
	r.send(t, "\x1bx")
	r.wait(t)
	out := r.out.String()
	if !strings.Contains(out, "\x1b[24;1H") || !strings.Contains(out, "\x1b[1;1H") {
		t.Fatalf("bar not on the last row with an absolute cursor restore: %q", out)
	}
	if strings.Contains(out, "\x1b7") || strings.Contains(out, "\x1b8") {
		t.Fatalf("bar used the shared cursor save slot: %q", out)
	}
	if strings.Contains(out, "\x1b[25;") {
		t.Fatal("bar written past the last row")
	}
}

func TestSnoopCP437Decoded(t *testing.T) {
	r := newRig(t, admin.SnoopHeader{OutputMode: "cp437", Width: 80, Height: 25}, 80, 25)
	_, _ = r.server.Write([]byte{0xDB})
	r.out.waitFor(t, "█")
	r.send(t, "\x1bx")
	r.wait(t)
}

func TestSnoopCP437EscapeSplitAcrossReads(t *testing.T) {
	r := newRig(t, admin.SnoopHeader{OutputMode: "cp437", Width: 80, Height: 25}, 80, 25)
	_, _ = r.server.Write([]byte("A\x1b[3"))
	r.out.waitFor(t, "A\x1b[3")
	_, _ = r.server.Write([]byte("1mB\xdb"))
	r.out.waitFor(t, "█")
	if !strings.Contains(r.out.String(), "A\x1b[31mB█") {
		t.Fatalf("escape mangled: %q", r.out.String())
	}
	r.send(t, "\x1bx")
	r.wait(t)
}

func TestSnoopReturnsWhenCallerLeaves(t *testing.T) {
	r := newRig(t, utf8Hdr, 80, 25)
	r.server.Close()
	r.wait(t)
	if r.cmd.Result().reason == "" {
		t.Fatal("no disconnect reason")
	}
}

func TestSnoopReleasesKeyboardWhenCallerLeaves(t *testing.T) {
	r := newRig(t, utf8Hdr, 80, 25)
	r.send(t, "\x1bt")
	deadline := time.Now().Add(2 * time.Second)
	for r.ctl.got() != "type+" && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	r.server.Close()
	r.wait(t)
	if c := r.ctl.got(); c != "type+,type-" {
		t.Fatalf("calls %s", c)
	}
}

// cancelFeed is a cancelable reader over a byte channel. It counts bytes
// handed out and, once canceled, returns without taking any more.
type cancelFeed struct {
	ch     chan byte
	n      atomic.Int32
	cancel chan struct{}
	once   sync.Once
}

func newCancelFeed() *cancelFeed {
	return &cancelFeed{ch: make(chan byte), cancel: make(chan struct{})}
}

func (f *cancelFeed) Read(p []byte) (int, error) {
	select {
	case <-f.cancel:
		return 0, cancelreader.ErrCanceled
	default:
	}
	select {
	case b := <-f.ch:
		f.n.Add(1)
		p[0] = b
		return 1, nil
	case <-f.cancel:
		return 0, cancelreader.ErrCanceled
	}
}

func (f *cancelFeed) Cancel() bool {
	f.once.Do(func() { close(f.cancel) })
	return true
}

func (f *cancelFeed) Close() error { return nil }

func TestSnoopStdinReaderTakesNothingAfterRun(t *testing.T) {
	a, server := net.Pipe()
	defer server.Close()
	in := newCancelFeed()
	cmd := newSnoopCmd(admin.NewSnoopStream(utf8Hdr, a), &fakeCtl{}, 3, fakeRaw{w: 80, h: 25})
	cmd.openIn = func(io.Reader) (cancelreader.CancelReader, error) { return in, nil }
	cmd.SetStdout(io.Discard)
	done := make(chan error, 1)
	go func() { done <- cmd.Run() }()
	in.ch <- 0x1b
	in.ch <- 'x'
	<-done
	before := in.n.Load()
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		for _, b := range []byte("zyw") {
			select {
			case in.ch <- b:
			case <-stop:
				return
			}
		}
	}()
	time.Sleep(50 * time.Millisecond)
	if got := in.n.Load() - before; got != 0 {
		t.Fatalf("reader took %d bytes after Run", got)
	}
}

func TestSnoopBarNotSplicedIntoCSI(t *testing.T) {
	r := newRig(t, utf8Hdr, 100, 40)
	_, _ = r.server.Write([]byte("A\x1b[3"))
	r.out.waitFor(t, "A\x1b[3")
	time.Sleep(20 * time.Millisecond)
	_, _ = r.server.Write([]byte("1mB"))
	r.out.waitFor(t, "B")
	r.send(t, "\x1bx")
	r.wait(t)
	out := r.out.String()
	if !strings.Contains(out, "A\x1b[31mB") {
		t.Fatalf("CSI interrupted: %q", out)
	}
	if !strings.Contains(out[strings.Index(out, "B"):], "NODE 3") {
		t.Fatal("bar not redrawn after the sequence completed")
	}
}

func TestSnoopBarNotSplicedIntoRune(t *testing.T) {
	r := newRig(t, utf8Hdr, 100, 40)
	_, _ = r.server.Write([]byte{'A', 0xe2, 0x96})
	r.out.waitFor(t, "A\xe2\x96")
	time.Sleep(20 * time.Millisecond)
	_, _ = r.server.Write([]byte{0x88})
	r.out.waitFor(t, "A█")
	r.send(t, "\x1bx")
	r.wait(t)
	out := r.out.String()
	i := strings.Index(out, "A█")
	if !strings.Contains(out[i:], "NODE 3") {
		t.Fatal("bar not redrawn after the rune completed")
	}
}

func TestSnoopChatFromTypeReturnsToType(t *testing.T) {
	r := newRig(t, utf8Hdr, 80, 25)
	got := r.reads()
	r.send(t, "\x1bt")
	r.send(t, "\x1bc")
	r.send(t, "\x1bc")
	r.send(t, "q")
	expect(t, got, "q")
	r.send(t, "\x1bx")
	r.wait(t)
	if c := r.ctl.got(); c != "type+,chat+,chat-,type-" {
		t.Fatalf("calls %s", c)
	}
}

func TestSnoopExitFromChatEnteredFromTypeReleasesBoth(t *testing.T) {
	r := newRig(t, utf8Hdr, 80, 25)
	r.send(t, "\x1bt")
	r.send(t, "\x1bc")
	r.send(t, "\x1bx")
	r.wait(t)
	if c := r.ctl.got(); c != "type+,chat+,chat-,type-" {
		t.Fatalf("calls %s", c)
	}
}

type failRaw struct{ fakeRaw }

func (failRaw) MakeRaw() (func(), error) { return nil, errors.New("no tty") }

func TestSnoopResultCarriesNodeWhenRawFails(t *testing.T) {
	a, server := net.Pipe()
	defer server.Close()
	cmd := newSnoopCmd(admin.NewSnoopStream(utf8Hdr, a), &fakeCtl{}, 7, failRaw{})
	cmd.SetStdin(strings.NewReader(""))
	cmd.SetStdout(io.Discard)
	if err := cmd.Run(); err == nil {
		t.Fatal("want error")
	}
	if cmd.Result().node != 7 {
		t.Fatalf("node %d", cmd.Result().node)
	}
}

func TestSeqTrackerRecovers(t *testing.T) {
	var tr seqTracker
	tr.feed([]byte("\x1b]0;title"))
	if tr.safe() {
		t.Fatal("inside an OSC")
	}
	tr.feed([]byte("\x1b[31m"))
	if !tr.safe() {
		t.Fatal("ESC [ did not abort the OSC and complete a CSI")
	}
	tr.feed([]byte("\x1b[3"))
	tr.feed([]byte{0x18})
	if !tr.safe() {
		t.Fatal("CAN did not end the CSI")
	}
	tr.feed(append([]byte("\x1b]"), bytes.Repeat([]byte("x"), maxOSC+1)...))
	if !tr.safe() {
		t.Fatal("overlong OSC did not recover")
	}
}

func TestSnoopBarRedrawsAfterUnterminatedOSC(t *testing.T) {
	r := newRig(t, utf8Hdr, 100, 40)
	_, _ = r.server.Write([]byte("\x1b]0;stuck"))
	r.out.waitFor(t, "stuck")
	_, _ = r.server.Write([]byte("\x1b[31mtext"))
	r.out.waitFor(t, "text")
	r.send(t, "\x1bx")
	r.wait(t)
	out := r.out.String()
	if !strings.Contains(out[strings.Index(out, "text"):], "NODE 3") {
		t.Fatal("bar not redrawn after the OSC was abandoned")
	}
}

func TestSnoopChatEndedByCallerDropsToWatch(t *testing.T) {
	r := newRig(t, utf8Hdr, 100, 40)
	got := r.reads()
	r.send(t, "\x1bt")
	r.send(t, "\x1bc")
	r.out.waitFor(t, "CHAT")
	// The caller left chat; the server no longer has this sysop holding
	// the keyboard, and the error arrives over RPC as text.
	r.ctl.mu.Lock()
	r.ctl.chatErr = errors.New(snoop.ErrNotHolder.Error())
	r.ctl.mu.Unlock()
	r.send(t, "\x1bc")
	r.out.waitFor(t, "WATCH · chat ended by caller")
	r.send(t, "ok")
	r.send(t, "\x1bx")
	r.wait(t)
	select {
	case b := <-got:
		t.Fatalf("watch mode forwarded %q", b)
	default:
	}
	if c := r.ctl.got(); c != "type+,chat+,chat-" {
		t.Fatalf("calls %s", c)
	}
}

// sizedRaw is a terminal whose size the test changes.
type sizedRaw struct {
	mu   sync.Mutex
	w, h int
}

func (f *sizedRaw) MakeRaw() (func(), error) { return func() {}, nil }

func (f *sizedRaw) Size() (int, int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.w, f.h, nil
}

func (f *sizedRaw) set(w, h int) {
	f.mu.Lock()
	f.w, f.h = w, h
	f.mu.Unlock()
}

func newResizeRig(t *testing.T, raw *sizedRaw) *snoopRig {
	t.Helper()
	a, server := net.Pipe()
	t.Cleanup(func() { server.Close() })
	r := &snoopRig{server: server, out: &syncBuf{}, ctl: &fakeCtl{}, done: make(chan error, 1)}
	inR, inW := io.Pipe()
	t.Cleanup(func() { inW.Close() })
	r.in = inW
	r.cmd = newSnoopCmd(admin.NewSnoopStream(utf8Hdr, a), r.ctl, 3, raw)
	r.cmd.SetStdin(inR)
	r.cmd.SetStdout(r.out)
	r.cmd.resize = make(chan struct{}, 1)
	go func() { r.done <- r.cmd.Run() }()
	return r
}

func TestSnoopResizeShowsBarOnTallerTerminal(t *testing.T) {
	raw := &sizedRaw{w: 80, h: utf8Hdr.Height}
	r := newResizeRig(t, raw)
	_, _ = r.server.Write([]byte("hi"))
	r.out.waitFor(t, "hi")
	if strings.Contains(r.out.String(), "NODE 3") {
		t.Fatal("bar drawn on a terminal with no spare row")
	}
	raw.set(100, utf8Hdr.Height+10)
	r.cmd.resize <- struct{}{}
	r.out.waitFor(t, fmt.Sprintf("\x1b[%d;1H\x1b[0;30;47m", utf8Hdr.Height+10))
	r.send(t, "\x1bx")
	r.wait(t)
}

func TestSnoopResizeHidesAndClearsBarOnShorterTerminal(t *testing.T) {
	raw := &sizedRaw{w: 100, h: utf8Hdr.Height + 10}
	r := newResizeRig(t, raw)
	r.out.waitFor(t, "NODE 3")
	raw.set(80, utf8Hdr.Height)
	r.cmd.resize <- struct{}{}
	r.out.waitFor(t, "\x1b[r\x1b[1;1H")
	n := strings.Count(r.out.String(), "NODE 3")
	_, _ = r.server.Write([]byte("more"))
	r.out.waitFor(t, "more")
	r.send(t, "\x1bx")
	r.wait(t)
	if strings.Contains(r.out.String(), "\x1b[2K") {
		t.Fatal("cleared a row that is off the shorter terminal")
	}
	if got := strings.Count(r.out.String(), "NODE 3"); got != n {
		t.Fatal("bar redrawn on a terminal with no spare row")
	}
}

func TestSnoopResizeKeepsSysopToggle(t *testing.T) {
	raw := &sizedRaw{w: 100, h: utf8Hdr.Height + 10}
	r := newResizeRig(t, raw)
	r.out.waitFor(t, "NODE 3")
	r.send(t, "\x1bh")
	r.out.waitFor(t, "\x1b[2K")
	raw.set(100, utf8Hdr.Height+12)
	r.cmd.resize <- struct{}{}
	time.Sleep(50 * time.Millisecond)
	n := strings.Count(r.out.String(), "NODE 3")
	_, _ = r.server.Write([]byte("more"))
	r.out.waitFor(t, "more")
	r.send(t, "\x1bx")
	r.wait(t)
	if got := strings.Count(r.out.String(), "NODE 3"); got != n {
		t.Fatal("resize re-enabled a bar the sysop hid")
	}
}

func TestSnoopResizeClearsBarRowStillOnScreen(t *testing.T) {
	raw := &sizedRaw{w: 100, h: utf8Hdr.Height + 10}
	r := newResizeRig(t, raw)
	r.out.waitFor(t, "NODE 3")
	raw.set(100, utf8Hdr.Height+20)
	r.cmd.resize <- struct{}{}
	r.out.waitFor(t, fmt.Sprintf("\x1b[%d;1H\x1b[0m\x1b[2K", utf8Hdr.Height+10))
	r.send(t, "\x1bx")
	r.wait(t)
}

func TestSnoopNoWriteAfterRunReturns(t *testing.T) {
	for i := 0; i < 30; i++ {
		raw := &sizedRaw{w: 100, h: utf8Hdr.Height + 10}
		r := newResizeRig(t, raw)
		r.out.waitFor(t, "NODE 3")
		raw.set(100, utf8Hdr.Height+12+i)
		r.cmd.resize <- struct{}{}
		r.send(t, "\x1bx")
		r.wait(t)
		if got := r.out.String(); !strings.HasSuffix(got, "\x1b[r\x1b[0m\x1b[2J") {
			t.Fatalf("wrote after Run returned: %q", got[max(0, len(got)-60):])
		}
	}
}

// vtScreen is a minimal terminal: text, CR, LF, CUP, DECSTBM, ED and EL.
type vtScreen struct {
	w, h     int
	cells    [][]rune
	row, col int // 0-based
	top, bot int
	scrolls  int
}

func newVTScreen(w, h int) *vtScreen {
	s := &vtScreen{w: w, h: h, top: 0, bot: h - 1}
	for i := 0; i < h; i++ {
		s.cells = append(s.cells, s.blank())
	}
	return s
}

func (s *vtScreen) blank() []rune { return []rune(strings.Repeat(" ", s.w)) }

func (s *vtScreen) lf() {
	if s.row == s.bot {
		copy(s.cells[s.top:s.bot], s.cells[s.top+1:s.bot+1])
		s.cells[s.bot] = s.blank()
		s.scrolls++
	} else if s.row < s.h-1 {
		s.row++
	}
}

func (s *vtScreen) feed(in string) {
	b := []rune(in)
	for i := 0; i < len(b); i++ {
		switch r := b[i]; {
		case r == 0x1b && i+1 < len(b) && b[i+1] == '[':
			j := i + 2
			for j < len(b) && (b[j] < 0x40 || b[j] > 0x7e) {
				j++
			}
			if j >= len(b) {
				return
			}
			s.csi(b[j], string(b[i+2:j]))
			i = j
		case r == 0x1b:
			i++
		case r == '\r':
			s.col = 0
		case r == '\n':
			s.lf()
		case r >= 0x20:
			if s.col >= s.w {
				s.col = s.w - 1
			}
			s.cells[s.row][s.col] = r
			s.col++
		}
	}
}

func (s *vtScreen) csi(final rune, params string) {
	var n []int
	for _, p := range strings.Split(params, ";") {
		v := 0
		fmt.Sscanf(p, "%d", &v)
		n = append(n, v)
	}
	arg := func(i, def int) int {
		if i < len(n) && n[i] > 0 {
			return n[i]
		}
		return def
	}
	switch final {
	case 'H':
		s.row, s.col = arg(0, 1)-1, arg(1, 1)-1
	case 'r':
		s.top, s.bot = arg(0, 1)-1, arg(1, s.h)-1
		s.row, s.col = 0, 0
	case 'J':
		if arg(0, 0) == 2 {
			for i := range s.cells {
				s.cells[i] = s.blank()
			}
		}
	case 'K':
		if arg(0, 0) == 2 {
			s.cells[s.row] = s.blank()
		}
	}
}

func (s *vtScreen) line(i int) string { return strings.TrimRight(string(s.cells[i]), " ") }

func TestSnoopMirroredScreenScrollsUnderBar(t *testing.T) {
	r := newRig(t, utf8Hdr, 100, 40)
	r.out.waitFor(t, "NODE 3")
	var sb strings.Builder
	for i := 1; i <= 40; i++ {
		fmt.Fprintf(&sb, "line%02d\r\n", i)
	}
	_, _ = r.server.Write([]byte(sb.String()))
	r.out.waitFor(t, "line40")
	r.send(t, "\x1bx")
	r.wait(t)
	out := strings.TrimSuffix(r.out.String(), "\x1b[r\x1b[0m\x1b[2J")
	sc := newVTScreen(100, 40)
	sc.feed(out)
	if sc.scrolls == 0 {
		t.Fatal("the mirrored vtScreen never scrolled")
	}
	if got := sc.line(0); got != "line17" {
		t.Fatalf("row 1 = %q, want line17", got)
	}
	if got := sc.line(23); got != "line40" {
		t.Fatalf("row 24 = %q, want line40", got)
	}
	if got := sc.line(24); got != "" {
		t.Fatalf("row 25 = %q, want empty", got)
	}
	if !strings.Contains(sc.line(39), "NODE 3") {
		t.Fatalf("bar missing from the last row: %q", sc.line(39))
	}
	if !strings.HasSuffix(r.out.String(), "\x1b[r\x1b[0m\x1b[2J") {
		t.Fatal("scroll region not reset at exit")
	}
}

func TestSnoopCallerRegionChangeReassertsOurs(t *testing.T) {
	r := newRig(t, utf8Hdr, 100, 40)
	r.out.waitFor(t, "\x1b[1;25r")
	_, _ = r.server.Write([]byte("\x1b[3;10r"))
	r.out.waitFor(t, "\x1b[3;10r")
	r.out.waitFor(t, "\x1b[3;10r\x1b[1;25r\x1b[1;1H")
	_, _ = r.server.Write([]byte("\x1bc"))
	r.out.waitFor(t, "\x1bc\x1b[1;25r")
	r.send(t, "\x1bx")
	r.wait(t)
}

func TestSnoopNoRegionWhenTerminalIsNotTaller(t *testing.T) {
	r := newRig(t, utf8Hdr, 80, 25)
	_, _ = r.server.Write([]byte("hi"))
	r.out.waitFor(t, "hi")
	r.send(t, "\x1bx")
	r.wait(t)
	if strings.Contains(r.out.String(), "r\x1b[") && strings.Contains(r.out.String(), "\x1b[1;25r") {
		t.Fatal("scroll region set on a terminal with no spare row")
	}
}
