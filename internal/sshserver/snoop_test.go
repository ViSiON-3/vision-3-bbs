package sshserver

import (
	"bytes"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/gliderlabs/ssh"

	"github.com/ViSiON-3/vision-3-bbs/internal/snoop"
)

// pipeSession is a minimal ssh.Session: reads come from r, writes go to w.
type pipeSession struct {
	ssh.Session
	r   io.Reader
	mu  sync.Mutex
	w   bytes.Buffer
	pty bool
}

func (p *pipeSession) Read(b []byte) (int, error) { return p.r.Read(b) }
func (p *pipeSession) Write(b []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.w.Write(b)
}
func (p *pipeSession) Pty() (ssh.Pty, <-chan ssh.Window, bool) { return ssh.Pty{}, nil, p.pty }

func TestWriteReachesTap(t *testing.T) {
	pr, _ := io.Pipe()
	bs := WrapSession(&pipeSession{r: pr, pty: true})
	tp := snoop.NewTap()
	bs.SetTap(tp)
	w := tp.Attach()
	defer w.Close()
	if _, err := bs.Write([]byte("a\nb")); err != nil {
		t.Fatal(err)
	}
	if got := <-w.C(); string(got) != "a\r\nb" {
		t.Fatalf("tap got %q; want CRLF-normalised", got)
	}
}

func TestTransferSuppressesTap(t *testing.T) {
	pr, _ := io.Pipe()
	bs := WrapSession(&pipeSession{r: pr})
	tp := snoop.NewTap()
	bs.SetTap(tp)
	w := tp.Attach()
	defer w.Close()
	bs.SetTransferActive(true)
	_, _ = bs.RawWrite([]byte{0x2a, 0x18, 0x42})
	if got := <-w.C(); string(got) != snoop.TransferMarker {
		t.Fatalf("got %q; want marker", got)
	}
	bs.SetTransferActive(false)
	if tp.Mode() != snoop.ModeBBS {
		t.Fatalf("mode = %v after transfer", tp.Mode())
	}
}

func TestInjectedBytesComeOutOfRead(t *testing.T) {
	pr, pw := io.Pipe()
	bs := WrapSession(&pipeSession{r: pr})
	tp := snoop.NewTap()
	bs.SetTap(tp)
	if err := tp.TakeKeyboard("sysop"); err != nil {
		t.Fatal(err)
	}
	got := make(chan string, 4)
	go func() {
		buf := make([]byte, 16)
		for i := 0; i < 2; i++ {
			n, _ := bs.Read(buf)
			got <- string(buf[:n])
		}
	}()
	tp.Inject("sysop", []byte("S"))
	go func() { _, _ = pw.Write([]byte("C")) }()
	seen := map[string]bool{}
	for i := 0; i < 2; i++ {
		select {
		case s := <-got:
			seen[s] = true
		case <-time.After(time.Second):
			t.Fatalf("only saw %v", seen)
		}
	}
	if !seen["S"] || !seen["C"] {
		t.Fatalf("saw %v; want both sysop and caller bytes", seen)
	}
}

func TestReadInterruptStillWorksWithTap(t *testing.T) {
	pr, _ := io.Pipe()
	bs := WrapSession(&pipeSession{r: pr})
	bs.SetTap(snoop.NewTap())
	ch := make(chan struct{})
	bs.SetReadInterrupt(ch)
	errc := make(chan error, 1)
	go func() {
		_, err := bs.Read(make([]byte, 8))
		errc <- err
	}()
	close(ch)
	select {
	case err := <-errc:
		if err != ErrReadInterrupted {
			t.Fatalf("err = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("interrupt ignored")
	}
}

func TestDoorModeSurvivesTransfer(t *testing.T) {
	pr, _ := io.Pipe()
	bs := WrapSession(&pipeSession{r: pr})
	tp := snoop.NewTap()
	bs.SetTap(tp)
	w := tp.Attach()
	defer w.Close()
	tp.SetMode(snoop.ModeDoor)
	bs.SetTransferActive(true)
	if tp.Mode() != snoop.ModeDoor {
		t.Fatalf("mode = %v during in-door transfer", tp.Mode())
	}
	_, _ = bs.RawWrite([]byte{0x2a, 0x18, 0x42})
	if got := <-w.C(); string(got) != snoop.TransferMarker {
		t.Fatalf("got %q; want marker", got)
	}
	bs.SetTransferActive(false)
	if tp.Mode() != snoop.ModeDoor {
		t.Fatalf("mode = %v after in-door transfer", tp.Mode())
	}
}

func TestLargeTapChunkSpansReads(t *testing.T) {
	pr, _ := io.Pipe()
	bs := WrapSession(&pipeSession{r: pr})
	tp := snoop.NewTap()
	bs.SetTap(tp)
	if err := tp.TakeKeyboard("sysop"); err != nil {
		t.Fatal(err)
	}
	tp.Inject("sysop", []byte("abcdefgh"))
	buf := make([]byte, 3)
	var got []string
	for i := 0; i < 3; i++ {
		n, err := bs.Read(buf)
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, string(buf[:n]))
	}
	if got[0] != "abc" || got[1] != "def" || got[2] != "gh" {
		t.Fatalf("reads = %q", got)
	}
}
