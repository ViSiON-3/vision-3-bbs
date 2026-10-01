package wfcui

import (
	"bytes"
	"errors"
	"io"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/admin"
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
	if !strings.Contains(out, "\x1b7\x1b[24;1H") || !strings.Contains(out, "\x1b8") {
		t.Fatalf("bar not on the last row with cursor save/restore: %q", out)
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

// feedReader hands out bytes from a channel and counts them.
type feedReader struct {
	ch chan byte
	n  atomic.Int32
}

func (f *feedReader) Read(p []byte) (int, error) {
	p[0] = <-f.ch
	f.n.Add(1)
	return 1, nil
}

func TestSnoopStdinReaderTakesAtMostOneByteAfterRun(t *testing.T) {
	a, server := net.Pipe()
	defer server.Close()
	in := &feedReader{ch: make(chan byte)}
	cmd := newSnoopCmd(admin.NewSnoopStream(utf8Hdr, a), &fakeCtl{}, 3, fakeRaw{w: 80, h: 25})
	cmd.SetStdin(in)
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
	if got := in.n.Load() - before; got > 1 {
		t.Fatalf("reader kept consuming stdin after Run: %d bytes", got)
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
