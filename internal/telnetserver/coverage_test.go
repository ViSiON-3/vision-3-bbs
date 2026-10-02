package telnetserver

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/gliderlabs/ssh"
)

// negotiationBytes is the option burst Negotiate sends first.
var negotiationBytes = []byte{
	IAC, WILL, OptEcho,
	IAC, WILL, OptSGA,
	IAC, DO, OptSGA,
	IAC, DONT, OptLinemode,
	IAC, DO, OptNAWS,
	IAC, DO, OptTermType,
}

var (
	termTypeRequest = []byte{IAC, SB, OptTermType, TermTypeSend, IAC, SE}
	cprQuery        = "\033[s\033[999;999H\033[6n"
	cprRestore      = "\033[u"
)

var errBrokenConn = errors.New("connection broken")

// failingConn is a fakeConn whose writes start failing after okWrites
// successful ones, and whose reads fail with readErr once the input is used up.
type failingConn struct {
	*fakeConn
	okWrites int
	readErr  error
}

func (f *failingConn) Write(p []byte) (int, error) {
	if f.okWrites <= 0 {
		return 0, errBrokenConn
	}
	f.okWrites--
	return f.fakeConn.Write(p)
}

func (f *failingConn) Read(p []byte) (int, error) {
	n, err := f.fakeConn.Read(p)
	if err == io.EOF && f.readErr != nil {
		return n, f.readErr
	}
	return n, err
}

// --- Negotiate ---

func TestNegotiate_RequestsTermTypeAfterWill(t *testing.T) {
	// The client agrees to TERM_TYPE, so the server must follow up with
	// SB TERM_TYPE SEND and record the IS reply.
	in := []byte{IAC, WILL, OptTermType, IAC, SB, OptTermType, TermTypeIs}
	in = append(in, "ANSI-BBS"...)
	in = append(in, IAC, SE)
	fc := newFakeConn(in)
	tc := NewTelnetConn(fc)

	if err := tc.Negotiate(); err != nil {
		t.Fatalf("Negotiate: %v", err)
	}
	want := append(append([]byte{}, negotiationBytes...), termTypeRequest...)
	if !bytes.Equal(fc.w.Bytes(), want) {
		t.Errorf("bytes sent = %v, want %v", fc.w.Bytes(), want)
	}
	if got := tc.TermType(); got != "ansi-bbs" {
		t.Errorf("TermType() = %q, want ansi-bbs", got)
	}
}

func TestNegotiate_NoTermTypeRequestWithoutWill(t *testing.T) {
	// WONT TERM_TYPE (and DO/DONT for other options) must not trigger phase 2.
	in := []byte{IAC, WONT, OptTermType, IAC, DO, OptEcho, IAC, DONT, OptSGA}
	fc := newFakeConn(in)
	tc := NewTelnetConn(fc)

	if err := tc.Negotiate(); err != nil {
		t.Fatalf("Negotiate: %v", err)
	}
	if !bytes.Equal(fc.w.Bytes(), negotiationBytes) {
		t.Errorf("bytes sent = %v, want only the option burst %v", fc.w.Bytes(), negotiationBytes)
	}
	if got := tc.TermType(); got != "ansi" {
		t.Errorf("TermType() = %q, want ansi", got)
	}
}

func TestNegotiate_WaitsForRepliesSplitAcrossPackets(t *testing.T) {
	// On a real network the client's replies can arrive in several packets
	// with pauses between them. Negotiate must keep reading until it has the
	// answer to DO TERM_TYPE and then the type itself, rather than stop at the
	// first pause — stopping there left a UTF-8 terminal typed as "ansi" and
	// sent it raw CP437 art.
	server, client := net.Pipe()
	tc := NewTelnetConn(server)
	t.Cleanup(func() { _ = tc.Close(); _ = client.Close() })
	// net.Pipe writes block until read, so a server that stops reading early
	// would otherwise hang the test instead of failing it.
	_ = client.SetDeadline(time.Now().Add(2 * negotiationTimeout))

	done := make(chan error, 1)
	go func() { done <- tc.Negotiate() }()

	send := func(b ...byte) {
		t.Helper()
		time.Sleep(50 * time.Millisecond)
		if _, err := client.Write(b); err != nil {
			t.Fatal(err)
		}
	}

	expectBytes(t, client, "option negotiation", negotiationBytes)
	send(IAC, WILL, OptSGA)
	send(IAC, SB, OptNAWS, 0, 80, 0, 25, IAC, SE)
	send(IAC, WILL, OptTermType)

	expectBytes(t, client, "TERM_TYPE request", termTypeRequest)
	send(IAC, SB, OptTermType, TermTypeIs, 'x', 't', 'e')
	send('r', 'm', IAC, SE)

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Negotiate: %v", err)
		}
	case <-time.After(2 * negotiationTimeout):
		t.Fatal("Negotiate did not return")
	}
	if got := tc.TermType(); got != "xterm" {
		t.Errorf("TermType() = %q, want xterm", got)
	}
}

func TestNegotiate_WriteErrors(t *testing.T) {
	// The very first write fails.
	tc := NewTelnetConn(&failingConn{fakeConn: newFakeConn(nil)})
	err := tc.Negotiate()
	if !errors.Is(err, errBrokenConn) || !strings.Contains(err.Error(), "failed to send telnet negotiations") {
		t.Errorf("first write: err = %v", err)
	}

	// The option burst goes out but the TERM_TYPE request fails.
	tc = NewTelnetConn(&failingConn{fakeConn: newFakeConn([]byte{IAC, WILL, OptTermType}), okWrites: 1})
	err = tc.Negotiate()
	if !errors.Is(err, errBrokenConn) || !strings.Contains(err.Error(), "failed to send TERM_TYPE request") {
		t.Errorf("second write: err = %v", err)
	}
}

// --- negotiation state machine ---

func TestProcessNegotiationBytes_Subnegotiations(t *testing.T) {
	longType := bytes.Repeat([]byte{'x'}, 300)

	tests := []struct {
		name     string
		in       []byte
		wantW    int
		wantH    int
		wantTerm string
	}{
		{
			name:  "NAWS surrounded by data and an escaped 0xFF",
			in:    []byte{'a', IAC, IAC, 'b', IAC, SB, OptNAWS, 0, 132, 0, 24, IAC, SE, 'c'},
			wantW: 132, wantH: 24, wantTerm: "ansi",
		},
		{
			name:  "NAWS width 255 arrives with its 0xFF doubled",
			in:    []byte{IAC, SB, OptNAWS, 0, IAC, IAC, 0, 20, IAC, SE},
			wantW: 255, wantH: 20, wantTerm: "ansi",
		},
		{
			name:  "NAWS shorter than four bytes is ignored",
			in:    []byte{IAC, SB, OptNAWS, 0, 132, 0, IAC, SE},
			wantW: 80, wantH: 25, wantTerm: "ansi",
		},
		{
			name:  "NAWS zero width falls back to 80x25",
			in:    []byte{IAC, SB, OptNAWS, 0, 0, 0, 24, IAC, SE},
			wantW: 80, wantH: 25, wantTerm: "ansi",
		},
		{
			name:  "NAWS width over 255 falls back to 80x25",
			in:    []byte{IAC, SB, OptNAWS, 1, 44, 0, 24, IAC, SE},
			wantW: 80, wantH: 25, wantTerm: "ansi",
		},
		{
			name:  "subnegotiation aborted by IAC <other> is dropped",
			in:    []byte{IAC, SB, OptNAWS, 0, 132, 0, 24, IAC, 241},
			wantW: 80, wantH: 25, wantTerm: "ansi",
		},
		{
			name:  "unknown IAC command is consumed",
			in:    []byte{IAC, 246, IAC, SB, OptNAWS, 0, 90, 0, 24, IAC, SE},
			wantW: 90, wantH: 24, wantTerm: "ansi",
		},
		{
			name:  "terminal type is trimmed and lowercased",
			in:    append(append([]byte{IAC, SB, OptTermType, TermTypeIs}, " SyncTERM "...), IAC, SE),
			wantW: 80, wantH: 25, wantTerm: "syncterm",
		},
		{
			name:  "terminal type SEND from the client is not a type",
			in:    append(append([]byte{IAC, SB, OptTermType, TermTypeSend}, "VT100"...), IAC, SE),
			wantW: 80, wantH: 25, wantTerm: "ansi",
		},
		{
			name:  "blank terminal type keeps the default",
			in:    []byte{IAC, SB, OptTermType, TermTypeIs, ' ', IAC, SE},
			wantW: 80, wantH: 25, wantTerm: "ansi",
		},
		{
			name:  "oversize subnegotiation is truncated to 256 bytes",
			in:    append(append([]byte{IAC, SB, OptTermType, TermTypeIs}, longType...), IAC, SE),
			wantW: 80, wantH: 25, wantTerm: string(longType[:255]),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tc := NewTelnetConn(newFakeConn(nil))
			tc.processNegotiationBytes(tt.in)

			if w, h := tc.WindowSize(); w != tt.wantW || h != tt.wantH {
				t.Errorf("WindowSize() = %dx%d, want %dx%d", w, h, tt.wantW, tt.wantH)
			}
			if got := tc.TermType(); got != tt.wantTerm {
				t.Errorf("TermType() = %q, want %q", got, tt.wantTerm)
			}
			if tc.state != stateData {
				t.Errorf("state machine left in state %d, want stateData", tc.state)
			}
		})
	}
}

func TestRead_SubnegotiationEdgeCases(t *testing.T) {
	longType := bytes.Repeat([]byte{'y'}, 300)

	// An aborted subnegotiation is dropped and the data after it still flows.
	in := []byte{'a', IAC, SB, OptNAWS, 0, 132, 0, 24, IAC, 241, 'b'}
	// WONT / DO / DONT option bytes are consumed.
	in = append(in, IAC, WONT, OptEcho, IAC, DO, OptSGA, IAC, DONT, OptLinemode, 'c')
	// Oversize terminal type, with a doubled 0xFF past the 256-byte limit.
	in = append(in, IAC, SB, OptTermType, TermTypeIs)
	in = append(in, longType...)
	in = append(in, IAC, IAC, IAC, SE, 'd')

	tc := NewTelnetConn(newFakeConn(in))
	if got := drainRead(t, tc); string(got) != "abcd" {
		t.Errorf("decoded payload = %q, want abcd", got)
	}
	if w, h := tc.WindowSize(); w != 80 || h != 25 {
		t.Errorf("WindowSize() = %dx%d, want 80x25 (aborted NAWS ignored)", w, h)
	}
	if got := tc.TermType(); got != string(longType[:255]) {
		t.Errorf("TermType() length = %d, want 255 bytes of %q", len(got), "y")
	}
}

func TestRead_EmptyBufferAndErrors(t *testing.T) {
	fc := &failingConn{fakeConn: newFakeConn([]byte("xy")), readErr: errBrokenConn}
	tc := NewTelnetConn(fc)

	// A zero-length read must not consume anything.
	if n, err := tc.Read(nil); n != 0 || err != nil {
		t.Errorf("Read(nil) = (%d, %v), want (0, nil)", n, err)
	}
	buf := make([]byte, 8)
	n, err := tc.Read(buf)
	if string(buf[:n]) != "xy" {
		t.Errorf("Read = %q, want xy", buf[:n])
	}
	// The connection error surfaces once the data has been delivered.
	if err == nil {
		_, err = tc.Read(buf)
	}
	if !errors.Is(err, errBrokenConn) {
		t.Errorf("Read err = %v, want %v", err, errBrokenConn)
	}
}

func TestRead_InterruptReportsEOFForConnError(t *testing.T) {
	// With a fired interrupt pending, a read error is reported as io.EOF so
	// door I/O loops stop cleanly instead of logging a connection failure.
	tc := NewTelnetConn(&failingConn{fakeConn: newFakeConn(nil), readErr: errBrokenConn})
	ch := make(chan struct{})
	close(ch)
	tc.SetReadInterrupt(ch)

	if n, err := tc.Read(make([]byte, 8)); n != 0 || err != io.EOF {
		t.Errorf("interrupted Read = (%d, %v), want (0, io.EOF)", n, err)
	}

	// Clearing the interrupt restores the real error.
	tc.SetReadInterrupt(nil)
	if _, err := tc.Read(make([]byte, 8)); !errors.Is(err, errBrokenConn) {
		t.Errorf("Read after clearing interrupt: err = %v, want %v", err, errBrokenConn)
	}
}

func TestWrite_EscapedWriteError(t *testing.T) {
	tc := NewTelnetConn(&failingConn{fakeConn: newFakeConn(nil)})
	if n, err := tc.Write([]byte{'a', 0xFF}); n != 0 || !errors.Is(err, errBrokenConn) {
		t.Errorf("Write = (%d, %v), want (0, %v)", n, err, errBrokenConn)
	}
}

// --- DetectTerminalSize ---

func TestDetectTerminalSize_CPR(t *testing.T) {
	tests := []struct {
		name         string
		response     string
		wantW, wantH int
	}{
		{"plain report", "\033[24;132R", 132, 24},
		{"report after type-ahead", "x\033[25;80R", 80, 25},
		{"rows capped at 25", "\033[50;100R", 100, 25},
		{"columns capped at 255", "\033[24;300R", 255, 24},
		{"implausibly narrow uses 80", "\033[24;10R", 80, 24},
		{"implausibly short uses 25", "\033[5;80R", 80, 25},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fc := newFakeConn([]byte(tt.response))
			tc := NewTelnetConn(fc)

			w, h, method := tc.DetectTerminalSize()
			if w != tt.wantW || h != tt.wantH || method != "ANSI" {
				t.Errorf("DetectTerminalSize() = %d, %d, %q; want %d, %d, ANSI", w, h, method, tt.wantW, tt.wantH)
			}
			// Cursor saved, parked bottom-right, queried, then restored.
			if got := fc.w.String(); got != cprQuery+cprRestore {
				t.Errorf("bytes sent = %q, want %q", got, cprQuery+cprRestore)
			}
			if gw, gh := tc.WindowSize(); gw != tt.wantW || gh != tt.wantH {
				t.Errorf("WindowSize() = %dx%d, want %dx%d", gw, gh, tt.wantW, tt.wantH)
			}
			select {
			case win := <-tc.winCh:
				if win.Width != tt.wantW || win.Height != tt.wantH {
					t.Errorf("window change = %dx%d, want %dx%d", win.Width, win.Height, tt.wantW, tt.wantH)
				}
			default:
				t.Error("no window change queued after CPR detection")
			}
		})
	}
}

func TestDetectTerminalSize_FallsBackToNAWS(t *testing.T) {
	naws := []byte{IAC, SB, OptNAWS, 0, 132, 0, 24, IAC, SE}

	tests := []struct {
		name string
		conn net.Conn
	}{
		// A late NAWS reply mixed into the (absent) CPR response still counts.
		{"no CPR report", newFakeConn(naws)},
		{"garbage instead of a report", newFakeConn(append([]byte("\033[24;R junk"), naws...))},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tc := NewTelnetConn(tt.conn)
			w, h, method := tc.DetectTerminalSize()
			if w != 132 || h != 24 || method != "NAWS" {
				t.Errorf("DetectTerminalSize() = %d, %d, %q; want 132, 24, NAWS", w, h, method)
			}
		})
	}

	t.Run("silent client keeps negotiated size", func(t *testing.T) {
		fc := newFakeConn(nil)
		tc := NewTelnetConn(fc)
		tc.processNegotiationBytes(naws)
		w, h, method := tc.DetectTerminalSize()
		if w != 132 || h != 24 || method != "NAWS" {
			t.Errorf("DetectTerminalSize() = %d, %d, %q; want 132, 24, NAWS", w, h, method)
		}
		if got := fc.w.String(); got != cprQuery+cprRestore {
			t.Errorf("bytes sent = %q, want %q", got, cprQuery+cprRestore)
		}
	})

	t.Run("query cannot be sent", func(t *testing.T) {
		tc := NewTelnetConn(&failingConn{fakeConn: newFakeConn([]byte("\033[24;132R"))})
		w, h, method := tc.DetectTerminalSize()
		if w != 80 || h != 25 || method != "NAWS" {
			t.Errorf("DetectTerminalSize() = %d, %d, %q; want 80, 25, NAWS", w, h, method)
		}
	})
}

func TestDetectTerminalSize_DefaultsWhenSizeUnusable(t *testing.T) {
	// NAWS handling never stores such a size; this guards the last-resort
	// branch against a stored size that is unusable for a 25-line BBS.
	tc := NewTelnetConn(newFakeConn(nil))
	tc.width, tc.height = 132, 50

	w, h, method := tc.DetectTerminalSize()
	if w != 80 || h != 25 || method != "DEFAULT" {
		t.Errorf("DetectTerminalSize() = %d, %d, %q; want 80, 25, DEFAULT", w, h, method)
	}
	if gw, gh := tc.WindowSize(); gw != 80 || gh != 25 {
		t.Errorf("WindowSize() = %dx%d, want 80x25", gw, gh)
	}
}

// --- session adapter ---

func TestAdapter_SessionMetadata(t *testing.T) {
	a := NewTelnetSessionAdapter(NewTelnetConn(newFakeConn(nil)))
	t.Cleanup(func() { _ = a.Close() })

	if got := a.RemoteAddr().String(); got != "127.0.0.1:0" {
		t.Errorf("RemoteAddr() = %q", got)
	}
	if got := a.LocalAddr().Network(); got != "tcp" {
		t.Errorf("LocalAddr().Network() = %q", got)
	}
	if len(a.Environ()) != 0 || len(a.Command()) != 0 || a.RawCommand() != "" || a.Subsystem() != "" {
		t.Errorf("shell session reported env=%v command=%v raw=%q subsystem=%q",
			a.Environ(), a.Command(), a.RawCommand(), a.Subsystem())
	}
	if a.PublicKey() != nil {
		t.Errorf("PublicKey() = %v, want nil", a.PublicKey())
	}
	if p := a.Permissions(); p.Permissions != nil {
		t.Errorf("Permissions() = %+v, want empty", p)
	}
	if ok, err := a.SendRequest("keepalive", true, nil); ok || err == nil {
		t.Errorf("SendRequest = (%v, %v), want (false, error)", ok, err)
	}
	if err := a.CloseWrite(); err != nil {
		t.Errorf("CloseWrite() = %v, want nil", err)
	}
	// No-ops that must not block or touch the channels they are given.
	a.Signals(make(chan ssh.Signal))
	a.Break(make(chan bool))

	ctx := a.Context()
	if ctx.User() != "" || ctx.SessionID() != a.SessionID() || !strings.HasPrefix(ctx.SessionID(), "telnet-") {
		t.Errorf("context user=%q sessionID=%q, adapter sessionID=%q", ctx.User(), ctx.SessionID(), a.SessionID())
	}
	if ctx.ClientVersion() != "telnet" || ctx.ServerVersion() != "vision3-telnet" {
		t.Errorf("versions = %q / %q", ctx.ClientVersion(), ctx.ServerVersion())
	}
	if ctx.RemoteAddr().String() != "127.0.0.1:0" || ctx.LocalAddr().String() != "127.0.0.1:0" {
		t.Errorf("context addrs = %v / %v", ctx.RemoteAddr(), ctx.LocalAddr())
	}
	if ctx.Permissions() != nil {
		t.Errorf("context Permissions() = %v, want nil", ctx.Permissions())
	}
	if _, ok := ctx.Deadline(); ok {
		t.Error("telnet session context has a deadline")
	}

	// Lock/Unlock guard the same mutex as SetValue: a SetValue made while
	// the lock is held cannot complete until it is released.
	ctx.Lock()
	set := make(chan struct{})
	go func() {
		ctx.SetValue("node", 3)
		close(set)
	}()
	select {
	case <-set:
		ctx.Unlock()
		t.Fatal("SetValue completed while the context lock was held")
	default:
	}
	ctx.Unlock()
	<-set
	if got := ctx.Value("node"); got != 3 {
		t.Errorf("Value(node) = %v, want 3", got)
	}
}

func TestAdapter_SessionIDsAreUnique(t *testing.T) {
	a := NewTelnetSessionAdapter(NewTelnetConn(newFakeConn(nil)))
	b := NewTelnetSessionAdapter(NewTelnetConn(newFakeConn(nil)))
	t.Cleanup(func() { _ = a.Close(); _ = b.Close() })
	if a.SessionID() == b.SessionID() {
		t.Errorf("two sessions share ID %q", a.SessionID())
	}
}

func TestAdapter_CloseCancelsContext(t *testing.T) {
	a := NewTelnetSessionAdapter(NewTelnetConn(newFakeConn(nil)))
	ctx := a.Context()

	select {
	case <-ctx.Done():
		t.Fatal("context done before the session closed")
	default:
	}
	if err := ctx.Err(); err != nil {
		t.Errorf("Err() before close = %v, want nil", err)
	}

	// Exit is how the BBS ends a session; it must behave like Close.
	if err := a.Exit(0); err != nil {
		t.Fatalf("Exit: %v", err)
	}
	select {
	case <-ctx.Done():
	default:
		t.Error("context not done after the session closed")
	}
	if err := ctx.Err(); !errors.Is(err, context.Canceled) {
		t.Errorf("Err() after close = %v, want context.Canceled", err)
	}
}

func TestAdapter_BinaryAndStderrWrites(t *testing.T) {
	fc := newFakeConn(nil)
	a := NewTelnetSessionAdapter(NewTelnetConn(fc))
	t.Cleanup(func() { _ = a.Close() })

	// RawWrite is binary-safe for the caller but still escapes IAC on the wire.
	if n, err := a.RawWrite([]byte{0x0A, 0xFF, 0x0D}); n != 3 || err != nil {
		t.Fatalf("RawWrite = (%d, %v), want (3, nil)", n, err)
	}
	if _, err := a.Stderr().Write([]byte("err")); err != nil {
		t.Fatalf("Stderr write: %v", err)
	}
	want := append([]byte{0x0A, IAC, IAC, 0x0D}, "err"...)
	if !bytes.Equal(fc.w.Bytes(), want) {
		t.Errorf("bytes on the wire = %v, want %v", fc.w.Bytes(), want)
	}
}

func TestAdapter_TransferActiveFlag(t *testing.T) {
	a := NewTelnetSessionAdapter(NewTelnetConn(newFakeConn(nil)))
	t.Cleanup(func() { _ = a.Close() })

	if a.IsTransferActive() {
		t.Error("new session reports an active transfer")
	}
	a.SetTransferActive(true)
	if !a.IsTransferActive() {
		t.Error("IsTransferActive() = false after SetTransferActive(true)")
	}
	a.SetTransferActive(false)
	if a.IsTransferActive() {
		t.Error("IsTransferActive() = true after SetTransferActive(false)")
	}
}

func TestAdapter_SetReadInterrupt(t *testing.T) {
	// The adapter hands the interrupt to the telnet layer: once it has fired,
	// a failed read is reported as a clean io.EOF.
	tc := NewTelnetConn(&failingConn{fakeConn: newFakeConn(nil), readErr: errBrokenConn})
	a := NewTelnetSessionAdapter(tc)
	t.Cleanup(func() { _ = a.Close() })

	ch := make(chan struct{})
	close(ch)
	a.SetReadInterrupt(ch)
	if _, err := a.Read(make([]byte, 4)); err != io.EOF {
		t.Errorf("Read err = %v, want io.EOF", err)
	}
}

// --- Server ---

func TestNewServer_Validation(t *testing.T) {
	handler := func(*TelnetSessionAdapter) {}

	if _, err := NewServer(Config{Port: 2323}); err == nil || !strings.Contains(err.Error(), "session handler is required") {
		t.Errorf("missing handler: err = %v", err)
	}
	for _, port := range []int{0, -1} {
		if _, err := NewServer(Config{Port: port, SessionHandler: handler}); err == nil || !strings.Contains(err.Error(), "invalid port") {
			t.Errorf("port %d: err = %v", port, err)
		}
	}
	s, err := NewServer(Config{Port: 2323, SessionHandler: handler})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	if s.config.Host != "0.0.0.0" {
		t.Errorf("default host = %q, want 0.0.0.0", s.config.Host)
	}
	// Closing a server that never listened is a no-op.
	if err := s.Close(); err != nil {
		t.Errorf("Close before listen = %v, want nil", err)
	}
}

func TestListenAndServe_PortInUse(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	s, err := NewServer(Config{
		Host:           "127.0.0.1",
		Port:           ln.Addr().(*net.TCPAddr).Port,
		SessionHandler: func(*TelnetSessionAdapter) {},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ListenAndServe(); err == nil || !strings.Contains(err.Error(), "failed to listen") {
		t.Errorf("ListenAndServe on a busy port: err = %v", err)
	}
}

// startServer runs a telnet server on a free loopback port and returns its
// address. The server is closed, and its clean shutdown checked, on cleanup.
func startServer(t *testing.T, handler SessionHandler) string {
	t.Helper()
	// Reserve a free port, then release it for the server to bind.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	addr := ln.Addr().String()
	ln.Close()

	s, err := NewServer(Config{Host: "127.0.0.1", Port: port, SessionHandler: handler})
	if err != nil {
		t.Fatal(err)
	}
	served := make(chan error, 1)
	go func() { served <- s.ListenAndServe() }()
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
		select {
		case err := <-served:
			if err != nil {
				t.Errorf("ListenAndServe after Close = %v, want nil", err)
			}
		case <-time.After(5 * time.Second):
			t.Error("ListenAndServe did not return after Close")
		}
	})
	return addr
}

// dialTelnet connects to addr, retrying until the server has bound its port.
func dialTelnet(t *testing.T, addr string) net.Conn {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		conn, err := net.Dial("tcp", addr)
		if err == nil {
			_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
			t.Cleanup(func() { _ = conn.Close() })
			return conn
		}
		if time.Now().After(deadline) {
			t.Fatalf("dial %s: %v", addr, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// expectBytes reads exactly len(want) bytes from conn and compares them.
func expectBytes(t *testing.T, conn net.Conn, what string, want []byte) {
	t.Helper()
	got := make([]byte, len(want))
	if _, err := io.ReadFull(conn, got); err != nil {
		t.Fatalf("reading %s: %v (got %q)", what, err, got)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("%s = %q, want %q", what, got, want)
	}
}

// clientHandshake plays the client side of connection setup: it answers the
// option burst with WILL TERM_TYPE and a NAWS report, names its terminal when
// asked, and answers the cursor-position query.
func clientHandshake(t *testing.T, conn net.Conn, termType string, naws []byte, cpr string) {
	t.Helper()
	expectBytes(t, conn, "option negotiation", negotiationBytes)
	reply := append([]byte{IAC, WILL, OptTermType, IAC, SB, OptNAWS}, naws...)
	reply = append(reply, IAC, SE)
	if _, err := conn.Write(reply); err != nil {
		t.Fatal(err)
	}

	expectBytes(t, conn, "TERM_TYPE request", termTypeRequest)
	is := append([]byte{IAC, SB, OptTermType, TermTypeIs}, termType...)
	is = append(is, IAC, SE)
	if _, err := conn.Write(is); err != nil {
		t.Fatal(err)
	}

	expectBytes(t, conn, "CPR query", []byte(cprQuery))
	if _, err := conn.Write([]byte(cpr)); err != nil {
		t.Fatal(err)
	}
	expectBytes(t, conn, "cursor restore", []byte(cprRestore))
}

func TestServer_SessionLifecycle(t *testing.T) {
	type sessionInfo struct {
		term   string
		window ssh.Window
		input  string
	}
	sessions := make(chan sessionInfo, 1)
	addr := startServer(t, func(a *TelnetSessionAdapter) {
		pty, _, _ := a.Pty()
		_, _ = a.Write([]byte("Welcome\xff"))
		buf := make([]byte, 16)
		n, _ := a.Read(buf)
		sessions <- sessionInfo{pty.Term, pty.Window, string(buf[:n])}
	})
	conn := dialTelnet(t, addr)

	// NAWS says 100x25 but the cursor report shows only 24 usable rows, as
	// with a terminal that has a status line; the cursor report wins.
	clientHandshake(t, conn, "XTERM", []byte{0, 100, 0, 25}, "\033[24;100R")

	// Output is IAC-escaped on the wire; input has its IAC commands stripped.
	expectBytes(t, conn, "handler output", []byte{'W', 'e', 'l', 'c', 'o', 'm', 'e', IAC, IAC})
	if _, err := conn.Write([]byte{'h', 'i', IAC, 241, '!'}); err != nil {
		t.Fatal(err)
	}

	select {
	case got := <-sessions:
		want := sessionInfo{"xterm", ssh.Window{Width: 100, Height: 24}, "hi!"}
		if got != want {
			t.Errorf("session saw %+v, want %+v", got, want)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("session handler never completed")
	}

	// When the handler returns the server hangs up.
	if n, err := conn.Read(make([]byte, 1)); err != io.EOF {
		t.Errorf("read after handler returned = (%d, %v), want EOF", n, err)
	}
}

func TestServer_HandlerPanicClosesConnection(t *testing.T) {
	addr := startServer(t, func(*TelnetSessionAdapter) { panic("handler blew up") })

	// A panicking session must be dropped without taking the server down.
	for i := 0; i < 2; i++ {
		conn := dialTelnet(t, addr)
		clientHandshake(t, conn, "ansi", []byte{0, 80, 0, 25}, "\033[25;80R")
		if n, err := conn.Read(make([]byte, 1)); err != io.EOF {
			t.Errorf("connection %d: read after panic = (%d, %v), want EOF", i, n, err)
		}
	}
}
