package telnetserver

import (
	"testing"
	"time"
)

func TestAdapter_PtyReflectsTermAndSize(t *testing.T) {
	// Client reports terminal type "xterm" before the adapter is built.
	in := []byte{IAC, SB, OptTermType, TermTypeIs}
	in = append(in, []byte("xterm")...)
	in = append(in, IAC, SE)
	tc := NewTelnetConn(newFakeConn(in))
	drainRead(t, tc)

	a := NewTelnetSessionAdapter(tc)
	t.Cleanup(func() { _ = a.Close() }) // stop the winCh forwarding goroutine
	pty, winCh, ok := a.Pty()
	if !ok {
		t.Fatal("Pty() ok = false, want true")
	}
	if pty.Term != "xterm" {
		t.Errorf("pty.Term = %q, want xterm", pty.Term)
	}
	if pty.Window.Width != 80 || pty.Window.Height != 25 {
		t.Errorf("pty.Window = %dx%d, want 80x25", pty.Window.Width, pty.Window.Height)
	}
	select {
	case w := <-winCh:
		if w.Width != 80 || w.Height != 25 {
			t.Errorf("initial window = %dx%d, want 80x25", w.Width, w.Height)
		}
	case <-time.After(time.Second):
		t.Fatal("no initial window delivered on channel")
	}
}

func TestAdapter_ReadWriteDelegate(t *testing.T) {
	fc := newFakeConn([]byte("input"))
	tc := NewTelnetConn(fc)
	a := NewTelnetSessionAdapter(tc)
	t.Cleanup(func() { _ = a.Close() }) // stop the winCh forwarding goroutine

	b := make([]byte, 16)
	n, _ := a.Read(b)
	if string(b[:n]) != "input" {
		t.Errorf("adapter Read = %q, want input", b[:n])
	}

	if _, err := a.Write([]byte("out")); err != nil {
		t.Fatalf("adapter Write: %v", err)
	}
	if fc.w.String() != "out" {
		t.Errorf("written = %q, want out", fc.w.String())
	}
}

func TestAdapter_NAWSForwardedToWindowChannel(t *testing.T) {
	in := []byte{IAC, SB, OptNAWS, 0, 70, 0, 24, IAC, SE}
	tc := NewTelnetConn(newFakeConn(in))
	a := NewTelnetSessionAdapter(tc)
	t.Cleanup(func() { _ = a.Close() }) // stop the winCh forwarding goroutine

	_, winCh, _ := a.Pty()
	<-winCh // drain the initial 80x25 window

	// Reading the NAWS subnegotiation triggers the tc.winCh -> adapter forward.
	drainRead(t, tc)

	select {
	case w := <-winCh:
		if w.Width != 70 || w.Height != 24 {
			t.Errorf("forwarded window = %dx%d, want 70x24", w.Width, w.Height)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("NAWS update was not forwarded to the window channel")
	}

	if pty, _, _ := a.Pty(); pty.Window.Width != 70 || pty.Window.Height != 24 {
		t.Errorf("pty.Window after NAWS = %dx%d, want 70x24", pty.Window.Width, pty.Window.Height)
	}
}

func TestSessionContext_SetValueAndFallback(t *testing.T) {
	tc := NewTelnetConn(newFakeConn(nil))
	a := NewTelnetSessionAdapter(tc)
	t.Cleanup(func() { _ = a.Close() }) // stop the winCh forwarding goroutine
	ctx := a.Context()

	ctx.SetValue("key", "value")
	if got := ctx.Value("key"); got != "value" {
		t.Errorf("Value(key) = %v, want value", got)
	}
	if got := ctx.Value("absent"); got != nil {
		t.Errorf("Value(absent) = %v, want nil", got)
	}
	if a.SessionID() == "" {
		t.Error("SessionID should be non-empty")
	}
	if a.User() != "" {
		t.Errorf("telnet User() = %q, want empty (forces manual login)", a.User())
	}
}
