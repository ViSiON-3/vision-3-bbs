package telnetserver

import (
	"io"
	"net"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/snoop"
)

func pipeAdapter(t *testing.T) (*TelnetSessionAdapter, net.Conn) {
	t.Helper()
	server, client := net.Pipe()
	t.Cleanup(func() { server.Close(); client.Close() })
	return NewTelnetSessionAdapter(NewTelnetConn(server)), client
}

func TestTelnetInjectWakesBlockedRead(t *testing.T) {
	a, _ := pipeAdapter(t)
	tp := snoop.NewTap()
	a.SetTap(tp)
	if err := tp.TakeKeyboard("sysop"); err != nil {
		t.Fatal(err)
	}
	got := make(chan string, 1)
	go func() {
		buf := make([]byte, 8)
		n, _ := a.Read(buf)
		got <- string(buf[:n])
	}()
	time.Sleep(20 * time.Millisecond) // let Read block on the socket
	tp.Inject("sysop", []byte("x"))
	select {
	case s := <-got:
		if s != "x" {
			t.Fatalf("Read = %q", s)
		}
	case <-time.After(time.Second):
		t.Fatal("blocked read not woken by inject")
	}
}

func TestTelnetCallerBytesStillArriveAfterWake(t *testing.T) {
	a, client := pipeAdapter(t)
	tp := snoop.NewTap()
	a.SetTap(tp)
	_ = tp.TakeKeyboard("sysop")
	tp.Inject("sysop", []byte("s"))
	buf := make([]byte, 8)
	n, _ := a.Read(buf)
	if string(buf[:n]) != "s" {
		t.Fatalf("first read %q", buf[:n])
	}
	go func() { _, _ = client.Write([]byte("c")) }()
	n, err := a.Read(buf)
	if err != nil || string(buf[:n]) != "c" {
		t.Fatalf("second read %q, %v", buf[:n], err)
	}
}

// A wake that lands after the socket read already returned data must not
// leave a past deadline behind for the next read.
func TestTelnetWakeRacingCallerReadLeavesNoStaleDeadline(t *testing.T) {
	for i := 0; i < 200; i++ {
		a, client := pipeAdapter(t)
		tp := snoop.NewTap()
		a.SetTap(tp)
		_ = tp.TakeKeyboard("sysop")
		go func() {
			time.Sleep(time.Duration(i%5) * 100 * time.Microsecond)
			tp.Inject("sysop", []byte("s"))
		}()
		go func() { _, _ = client.Write([]byte("c")) }()

		var got []byte
		buf := make([]byte, 8)
		for len(got) < 2 {
			n, err := a.Read(buf)
			if err != nil {
				t.Fatalf("iter %d: read after %q: %v", i, got, err)
			}
			got = append(got, buf[:n]...)
		}
		if len(got) != 2 {
			t.Fatalf("iter %d: got %q", i, got)
		}
		seen := map[byte]bool{got[0]: true, got[1]: true}
		if !seen['s'] || !seen['c'] {
			t.Fatalf("iter %d: lost a byte, got %q", i, got)
		}

		go func() { _, _ = client.Write([]byte("z")) }()
		n, err := a.Read(buf)
		if err != nil || string(buf[:n]) != "z" {
			t.Fatalf("iter %d: next read %q, %v", i, buf[:n], err)
		}
	}
}

func TestTelnetSysopBytesKeepOrderAcrossSmallReads(t *testing.T) {
	a, _ := pipeAdapter(t)
	tp := snoop.NewTap()
	a.SetTap(tp)
	_ = tp.TakeKeyboard("sysop")
	tp.Inject("sysop", []byte("abc"))
	tp.Inject("sysop", []byte("de"))
	var got []byte
	buf := make([]byte, 2)
	for len(got) < 5 {
		n, err := a.Read(buf)
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, buf[:n]...)
	}
	if string(got) != "abcde" {
		t.Fatalf("got %q", got)
	}
}

func TestTelnetWriteReachesTap(t *testing.T) {
	a, client := pipeAdapter(t)
	go func() { _, _ = client.Read(make([]byte, 64)) }()
	tp := snoop.NewTap()
	a.SetTap(tp)
	w := tp.Attach()
	defer w.Close()
	if _, err := a.Write([]byte("hello")); err != nil {
		t.Fatal(err)
	}
	if got := <-w.C(); string(got) != "hello" {
		t.Fatalf("tap got %q", got)
	}
}

func TestTelnetTransferActiveMovesTapMode(t *testing.T) {
	a, _ := pipeAdapter(t)
	tp := snoop.NewTap()
	a.SetTap(tp)
	a.SetTransferActive(true)
	if tp.Mode() != snoop.ModeTransfer {
		t.Fatalf("mode = %v, want transfer", tp.Mode())
	}
	a.SetTransferActive(false)
	if tp.Mode() != snoop.ModeBBS {
		t.Fatalf("mode = %v, want bbs", tp.Mode())
	}
	tp.SetMode(snoop.ModeDoor)
	a.SetTransferActive(true)
	a.SetTransferActive(false)
	if tp.Mode() != snoop.ModeDoor {
		t.Fatalf("mode = %v, want door", tp.Mode())
	}
}

func TestTelnetTransferWriteSendsMarkerNotData(t *testing.T) {
	a, client := pipeAdapter(t)
	go func() { _, _ = client.Read(make([]byte, 64)) }()
	tp := snoop.NewTap()
	a.SetTap(tp)
	w := tp.Attach()
	defer w.Close()
	a.SetTransferActive(true)
	if _, err := a.Write([]byte("BINARY")); err != nil {
		t.Fatal(err)
	}
	if got := <-w.C(); string(got) != snoop.TransferMarker {
		t.Fatalf("tap got %q", got)
	}
}

// A wake racing a read interrupt must not erase the interrupt's deadline.
func TestTelnetWakeRacingReadInterruptStillEnds(t *testing.T) {
	for i := 0; i < 200; i++ {
		server, client := net.Pipe()
		tc := NewTelnetConn(server)
		intr := make(chan struct{})
		tc.SetReadInterrupt(intr)

		res := make(chan error, 1)
		go func() {
			_, err := tc.Read(make([]byte, 8))
			res <- err
		}()
		time.Sleep(time.Duration(i%4) * 50 * time.Microsecond)
		go tc.Wake()
		go close(intr)

		deadline := time.After(time.Second)
		for {
			select {
			case err := <-res:
				if err == ErrWoken {
					go func() {
						_, err := tc.Read(make([]byte, 8))
						res <- err
					}()
					continue
				}
				if err != io.EOF {
					t.Fatalf("iter %d: read = %v, want EOF", i, err)
				}
			case <-deadline:
				t.Fatalf("iter %d: interrupted read hung", i)
			}
			break
		}
		server.Close()
		client.Close()
	}
}
