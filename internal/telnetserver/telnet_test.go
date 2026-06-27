package telnetserver

import (
	"bytes"
	"io"
	"net"
	"testing"
	"time"
)

// fakeAddr is a stub net.Addr for the in-memory connection.
type fakeAddr struct{}

func (fakeAddr) Network() string { return "tcp" }
func (fakeAddr) String() string  { return "127.0.0.1:0" }

// fakeConn is a deterministic net.Conn backed by a fixed read buffer and a
// capture buffer for writes. Deadline setters are no-ops.
type fakeConn struct {
	r *bytes.Reader
	w bytes.Buffer
}

func newFakeConn(in []byte) *fakeConn { return &fakeConn{r: bytes.NewReader(in)} }

func (f *fakeConn) Read(p []byte) (int, error)         { return f.r.Read(p) }
func (f *fakeConn) Write(p []byte) (int, error)        { return f.w.Write(p) }
func (f *fakeConn) Close() error                       { return nil }
func (f *fakeConn) LocalAddr() net.Addr                { return fakeAddr{} }
func (f *fakeConn) RemoteAddr() net.Addr               { return fakeAddr{} }
func (f *fakeConn) SetDeadline(t time.Time) error      { return nil }
func (f *fakeConn) SetReadDeadline(t time.Time) error  { return nil }
func (f *fakeConn) SetWriteDeadline(t time.Time) error { return nil }

// drainRead reads the telnet connection to EOF, returning the decoded payload.
func drainRead(t *testing.T, tc *TelnetConn) []byte {
	t.Helper()
	var out []byte
	buf := make([]byte, 64)
	for {
		n, err := tc.Read(buf)
		out = append(out, buf[:n]...)
		if err != nil {
			if err != io.EOF {
				t.Fatalf("Read returned unexpected error: %v", err)
			}
			return out
		}
	}
}

func TestRead_StripsIACNegotiation(t *testing.T) {
	// IAC DO NAWS, then "hello", then IAC WILL ECHO — only "hello" should surface.
	in := []byte{IAC, DO, OptNAWS}
	in = append(in, []byte("hello")...)
	in = append(in, IAC, WILL, OptEcho)

	tc := NewTelnetConn(newFakeConn(in))
	got := drainRead(t, tc)
	if string(got) != "hello" {
		t.Errorf("decoded payload = %q, want %q", got, "hello")
	}
}

func TestRead_UnescapesDoubledIAC(t *testing.T) {
	// 0xFF in the data stream arrives doubled (IAC IAC) and must decode to one 0xFF.
	in := []byte{'a', IAC, IAC, 'b'}
	tc := NewTelnetConn(newFakeConn(in))
	got := drainRead(t, tc)
	want := []byte{'a', 0xFF, 'b'}
	if !bytes.Equal(got, want) {
		t.Errorf("decoded payload = %v, want %v", got, want)
	}
}

func TestRead_ConsumesUnknownIACCommand(t *testing.T) {
	// IAC followed by a non-option command byte (e.g. AYT=246) is consumed silently.
	in := []byte{'x', IAC, 246, 'y'}
	tc := NewTelnetConn(newFakeConn(in))
	got := drainRead(t, tc)
	if string(got) != "xy" {
		t.Errorf("decoded payload = %q, want %q", got, "xy")
	}
}

func TestRead_NAWSSetsWindowSize(t *testing.T) {
	// IAC SB NAWS <w hi><w lo><h hi><h lo> IAC SE; 70x24 is within BBS limits.
	in := []byte{IAC, SB, OptNAWS, 0, 70, 0, 24, IAC, SE}
	in = append(in, []byte("data")...)
	tc := NewTelnetConn(newFakeConn(in))

	got := drainRead(t, tc)
	if string(got) != "data" {
		t.Errorf("decoded payload = %q, want %q", got, "data")
	}
	w, h := tc.WindowSize()
	if w != 70 || h != 24 {
		t.Errorf("WindowSize() = %dx%d, want 70x24", w, h)
	}
}

func TestRead_NAWSCapsOversizeDimensions(t *testing.T) {
	// Width 0x00FF (255) carries a 0xFF that must be doubled inside the SB; the
	// result is capped to the BBS maximum of 80x25.
	in := []byte{IAC, SB, OptNAWS, 0x00, IAC, IAC, 0x00, 100, IAC, SE}
	tc := NewTelnetConn(newFakeConn(in))
	drainRead(t, tc)
	w, h := tc.WindowSize()
	if w != 80 || h != 25 {
		t.Errorf("WindowSize() = %dx%d, want 80x25 (capped)", w, h)
	}
}

func TestRead_TermTypeSubnegotiation(t *testing.T) {
	// IAC SB TERMTYPE IS "VT100" IAC SE -> TermType() lowercased.
	in := []byte{IAC, SB, OptTermType, TermTypeIs}
	in = append(in, []byte("VT100")...)
	in = append(in, IAC, SE)
	tc := NewTelnetConn(newFakeConn(in))
	drainRead(t, tc)
	if got := tc.TermType(); got != "vt100" {
		t.Errorf("TermType() = %q, want %q", got, "vt100")
	}
}

func TestTermType_DefaultsToAnsi(t *testing.T) {
	tc := NewTelnetConn(newFakeConn(nil))
	if got := tc.TermType(); got != "ansi" {
		t.Errorf("TermType() = %q, want %q", got, "ansi")
	}
}

func TestWindowSize_Defaults(t *testing.T) {
	tc := NewTelnetConn(newFakeConn(nil))
	if w, h := tc.WindowSize(); w != 80 || h != 25 {
		t.Errorf("default WindowSize() = %dx%d, want 80x25", w, h)
	}
}

func TestWrite_EscapesIAC(t *testing.T) {
	fc := newFakeConn(nil)
	tc := NewTelnetConn(fc)

	n, err := tc.Write([]byte{'a', 0xFF, 'b'})
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if n != 3 {
		t.Errorf("Write returned n=%d, want 3 (original byte count)", n)
	}
	want := []byte{'a', IAC, IAC, 'b'}
	if !bytes.Equal(fc.w.Bytes(), want) {
		t.Errorf("written bytes = %v, want %v", fc.w.Bytes(), want)
	}
}

func TestWrite_FastPathNoIAC(t *testing.T) {
	fc := newFakeConn(nil)
	tc := NewTelnetConn(fc)
	n, err := tc.Write([]byte("plain"))
	if err != nil || n != 5 {
		t.Fatalf("Write = (%d, %v), want (5, nil)", n, err)
	}
	if fc.w.String() != "plain" {
		t.Errorf("written = %q, want %q", fc.w.String(), "plain")
	}
}

func TestWrite_Empty(t *testing.T) {
	fc := newFakeConn(nil)
	tc := NewTelnetConn(fc)
	n, err := tc.Write(nil)
	if n != 0 || err != nil {
		t.Errorf("Write(nil) = (%d, %v), want (0, nil)", n, err)
	}
	if fc.w.Len() != 0 {
		t.Errorf("expected no bytes written, got %d", fc.w.Len())
	}
}

func TestProcessNegotiationBytes_DetectsWillTermType(t *testing.T) {
	tc := NewTelnetConn(newFakeConn(nil))
	tc.processNegotiationBytes([]byte{IAC, WILL, OptTermType})
	if !tc.willTermType {
		t.Error("expected willTermType=true after IAC WILL TERMTYPE")
	}
}

func TestNegotiate_SendsExpectedOptions(t *testing.T) {
	server, client := net.Pipe()
	tc := NewTelnetConn(server)
	t.Cleanup(func() { _ = tc.Close(); _ = client.Close() })

	done := make(chan error, 1)
	go func() { done <- tc.Negotiate() }()

	expected := []byte{
		IAC, WILL, OptEcho,
		IAC, WILL, OptSGA,
		IAC, DO, OptSGA,
		IAC, DONT, OptLinemode,
		IAC, DO, OptNAWS,
		IAC, DO, OptTermType,
	}
	got := make([]byte, len(expected))
	if _, err := io.ReadFull(client, got); err != nil {
		t.Fatalf("reading negotiation bytes: %v", err)
	}
	if !bytes.Equal(got, expected) {
		t.Errorf("negotiation bytes = %v, want %v", got, expected)
	}

	// Close the client so Negotiate's drain returns promptly instead of waiting
	// out the 500ms deadline.
	client.Close()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Negotiate returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Negotiate did not return after client close")
	}
}

func TestClose_Idempotent(t *testing.T) {
	tc := NewTelnetConn(newFakeConn(nil))
	if err := tc.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	// Second close must not panic (would double-close winCh) and returns nil.
	if err := tc.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}
}

func TestAddrs_Delegate(t *testing.T) {
	tc := NewTelnetConn(newFakeConn(nil))
	if got := tc.RemoteAddr().String(); got != "127.0.0.1:0" {
		t.Errorf("RemoteAddr = %q, want 127.0.0.1:0", got)
	}
	if got := tc.LocalAddr().Network(); got != "tcp" {
		t.Errorf("LocalAddr network = %q, want tcp", got)
	}
}

func TestSetReadInterrupt_UnblocksRead(t *testing.T) {
	// net.Pipe honors read deadlines, so it can exercise the interrupt path that
	// fakeConn (non-blocking) cannot.
	server, client := net.Pipe()
	tc := NewTelnetConn(server)
	t.Cleanup(func() { _ = tc.Close(); _ = client.Close() })

	type result struct {
		n   int
		err error
	}
	res := make(chan result, 1)
	go func() {
		b := make([]byte, 8)
		n, err := tc.Read(b)
		res <- result{n, err}
	}()

	time.Sleep(100 * time.Millisecond) // let the Read block on the pipe

	ch := make(chan struct{})
	tc.SetReadInterrupt(ch)
	close(ch)

	select {
	case r := <-res:
		if r.err != io.EOF {
			t.Errorf("Read err = %v, want io.EOF", r.err)
		}
		if r.n != 0 {
			t.Errorf("Read n = %d, want 0", r.n)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Read was not interrupted")
	}
}
