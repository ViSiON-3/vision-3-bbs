package telnetclient

import (
	"bytes"
	"context"
	"io"
	"net"
	"testing"
	"time"
)

// pair returns a client wrapped around one end of a loopback connection and
// the server's end of it. net.Pipe will not do: its writes block until they
// are read, and the client answers negotiation from inside Read.
func pair(t *testing.T, opts Options) (*Conn, net.Conn) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	accepted := make(chan net.Conn, 1)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			close(accepted)
			return
		}
		accepted <- c
	}()

	client, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	server, ok := <-accepted
	if !ok {
		t.Fatal("accept failed")
	}
	t.Cleanup(func() {
		_ = client.Close()
		_ = server.Close()
	})
	return NewConn(client, opts), server
}

// readN reads exactly n bytes from the server's end, or fails.
func readN(t *testing.T, c net.Conn, n int) []byte {
	t.Helper()
	_ = c.SetReadDeadline(time.Now().Add(5 * time.Second))
	buf := make([]byte, n)
	if _, err := io.ReadFull(c, buf); err != nil {
		t.Fatalf("server read of %d bytes: %v (got %q)", n, err, buf)
	}
	return buf
}

// expectQuiet fails if the client sent the server anything.
func expectQuiet(t *testing.T, c net.Conn) {
	t.Helper()
	_ = c.SetReadDeadline(time.Now().Add(150 * time.Millisecond))
	buf := make([]byte, 64)
	if n, _ := c.Read(buf); n > 0 {
		t.Errorf("client sent % x, want nothing", buf[:n])
	}
}

// clientRead reads the client's view of what the server sent.
func clientRead(t *testing.T, c *Conn, want string) {
	t.Helper()
	_ = c.SetReadDeadline(time.Now().Add(5 * time.Second))
	got := make([]byte, 0, len(want))
	buf := make([]byte, 256)
	for len(got) < len(want) {
		n, err := c.Read(buf)
		got = append(got, buf[:n]...)
		if err != nil {
			t.Fatalf("client read: %v (got %q)", err, got)
		}
	}
	if string(got) != want {
		t.Errorf("client read %q, want %q", got, want)
	}
}

func serverSend(t *testing.T, c net.Conn, b ...byte) {
	t.Helper()
	if _, err := c.Write(b); err != nil {
		t.Fatalf("server write: %v", err)
	}
}

// What a BBS sends a caller on connect, and what a client has to say back for
// the caller to see their own typing and a screen drawn to their size.
func TestAnswersAServersOpeningNegotiation(t *testing.T) {
	client, server := pair(t, Options{TermType: "ANSI-BBS", Width: 132, Height: 50})

	serverSend(t, server,
		iac, will, optEcho,
		iac, will, optSGA,
		iac, do, optSGA,
		iac, do, optNAWS,
		iac, do, optTermType,
		'h', 'i')
	clientRead(t, client, "hi")

	want := []byte{
		iac, do, optEcho,
		iac, do, optSGA,
		iac, will, optSGA,
		iac, will, optNAWS,
		iac, sb, optNAWS, 0, 132, 0, 50, iac, se,
		iac, will, optTermType,
	}
	if got := readN(t, server, len(want)); !bytes.Equal(got, want) {
		t.Errorf("client answered % x\nwant            % x", got, want)
	}

	serverSend(t, server, iac, sb, optTermType, termTypeSend, iac, se, '!')
	clientRead(t, client, "!")

	want = append([]byte{iac, sb, optTermType, termTypeIs}, "ANSI-BBS"...)
	want = append(want, iac, se)
	if got := readN(t, server, len(want)); !bytes.Equal(got, want) {
		t.Errorf("terminal type answer % x\nwant                 % x", got, want)
	}
}

func TestDefaultsTerminalTypeAndWindowSize(t *testing.T) {
	client, server := pair(t, Options{})

	serverSend(t, server,
		iac, do, optNAWS,
		iac, do, optTermType,
		iac, sb, optTermType, termTypeSend, iac, se,
		'x')
	clientRead(t, client, "x")

	want := []byte{
		iac, will, optNAWS,
		iac, sb, optNAWS, 0, 80, 0, 24, iac, se,
		iac, will, optTermType,
		iac, sb, optTermType, termTypeIs, 'A', 'N', 'S', 'I', iac, se,
	}
	if got := readN(t, server, len(want)); !bytes.Equal(got, want) {
		t.Errorf("client answered % x\nwant            % x", got, want)
	}
}

// A window 255 columns wide puts 0xFF in the size, which has to be escaped or
// the server reads it as the start of a command.
func TestEscapesIACInWindowSize(t *testing.T) {
	client, server := pair(t, Options{Width: 255, Height: 0x1FF})

	serverSend(t, server, iac, do, optNAWS, 'x')
	clientRead(t, client, "x")

	want := []byte{
		iac, will, optNAWS,
		iac, sb, optNAWS, 0, iac, iac, 1, iac, iac, iac, se,
	}
	if got := readN(t, server, len(want)); !bytes.Equal(got, want) {
		t.Errorf("client answered % x\nwant            % x", got, want)
	}
}

func TestRefusesOptionsItDoesNotImplement(t *testing.T) {
	const optLinemode, optStatus = 34, 5
	client, server := pair(t, Options{})

	serverSend(t, server, iac, do, optLinemode, iac, will, optStatus, iac, do, optEcho, 'x')
	clientRead(t, client, "x")

	// Echoing is the server's job; a client asked to do it says no.
	want := []byte{iac, wont, optLinemode, iac, dont, optStatus, iac, wont, optEcho}
	if got := readN(t, server, len(want)); !bytes.Equal(got, want) {
		t.Errorf("client answered % x\nwant            % x", got, want)
	}
}

// Answering every repeat of a request is how two ends get stuck answering
// each other. An option that is already on, or already off, needs no reply.
func TestDoesNotAnswerARepeatedRequest(t *testing.T) {
	client, server := pair(t, Options{})

	serverSend(t, server, iac, will, optEcho, iac, do, optSGA, 'a')
	clientRead(t, client, "a")
	readN(t, server, 6)

	serverSend(t, server,
		iac, will, optEcho, iac, do, optSGA, // already on
		iac, wont, optBinary, iac, dont, optNAWS, // never on
		'b')
	clientRead(t, client, "b")
	expectQuiet(t, server)

	// Turning one off is a change, and is acknowledged once.
	serverSend(t, server, iac, wont, optEcho, iac, wont, optEcho, 'c')
	clientRead(t, client, "c")
	want := []byte{iac, dont, optEcho}
	if got := readN(t, server, len(want)); !bytes.Equal(got, want) {
		t.Errorf("client answered % x, want % x", got, want)
	}
	expectQuiet(t, server)
}

func TestReadStripsTheProtocolFromOutput(t *testing.T) {
	for _, tc := range []struct {
		name string
		sent []byte
		want string
	}{
		{"escaped 0xFF is data", []byte{'a', iac, iac, 'b'}, "a\xffb"},
		{"commands without an option", []byte{'a', iac, 241, 'b', iac, 249, 'c'}, "abc"},
		{"CR NUL is a bare CR", []byte{'a', '\r', 0, 'b'}, "a\rb"},
		{"CR LF is kept", []byte{'a', '\r', '\n', 'b'}, "a\r\nb"},
		{"a NUL elsewhere is data", []byte{'a', 0, 'b'}, "a\x00b"},
		{"unknown subnegotiation", []byte{'a', iac, sb, 99, 1, 2, iac, iac, 3, iac, se, 'b'}, "ab"},
		{"stray byte after IAC in subnegotiation", []byte{'a', iac, sb, 99, iac, 7, 'z', iac, se, 'b'}, "ab"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client, server := pair(t, Options{})
			serverSend(t, server, tc.sent...)
			clientRead(t, client, tc.want)
		})
	}
}

// A command split across two reads has to be put back together, or the tail
// of it arrives on the caller's screen.
func TestReadHandlesACommandSplitAcrossReads(t *testing.T) {
	client, server := pair(t, Options{})

	serverSend(t, server, 'a', iac)
	clientRead(t, client, "a")
	serverSend(t, server, will)
	time.Sleep(20 * time.Millisecond)
	serverSend(t, server, optEcho, 'b')
	clientRead(t, client, "b")

	want := []byte{iac, do, optEcho}
	if got := readN(t, server, len(want)); !bytes.Equal(got, want) {
		t.Errorf("client answered % x, want % x", got, want)
	}
}

// In binary mode a NUL after a CR is the door's, not padding.
func TestBinaryModeLeavesCarriageReturnsAlone(t *testing.T) {
	client, server := pair(t, Options{})

	serverSend(t, server, iac, will, optBinary, iac, do, optBinary, 'a', '\r', 0, 'b')
	clientRead(t, client, "a\r\x00b")
	want := []byte{iac, do, optBinary, iac, will, optBinary}
	if got := readN(t, server, len(want)); !bytes.Equal(got, want) {
		t.Fatalf("client answered % x, want % x", got, want)
	}

	if _, err := client.Write([]byte("x\ry")); err != nil {
		t.Fatalf("client write: %v", err)
	}
	if got := readN(t, server, 3); string(got) != "x\ry" {
		t.Errorf("server received %q, want %q", got, "x\ry")
	}

	// Leaving binary mode brings the padding back.
	serverSend(t, server, iac, dont, optBinary, 'c')
	clientRead(t, client, "c")
	readN(t, server, 3)
	if _, err := client.Write([]byte("\r")); err != nil {
		t.Fatalf("client write: %v", err)
	}
	if got := readN(t, server, 2); string(got) != "\r\x00" {
		t.Errorf("server received %q, want %q", got, "\r\x00")
	}
}

func TestWriteEscapesCallerInput(t *testing.T) {
	for _, tc := range []struct {
		name  string
		input string
		want  string
	}{
		{"plain", "hello", "hello"},
		{"0xFF is doubled", "a\xffb", "a\xff\xffb"},
		{"bare CR is padded", "yes\r", "yes\r\x00"},
		{"CR mid-write is padded", "a\rb", "a\r\x00b"},
		{"CR LF is kept", "a\r\nb", "a\r\nb"},
		{"CR NUL is not padded twice", "a\r\x00b", "a\r\x00b"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client, server := pair(t, Options{})
			n, err := client.Write([]byte(tc.input))
			if err != nil {
				t.Fatalf("client write: %v", err)
			}
			// The count is of the caller's bytes, not of what went on the
			// wire: io.Copy treats any other number as a short write.
			if n != len(tc.input) {
				t.Errorf("Write returned %d, want %d", n, len(tc.input))
			}
			if got := readN(t, server, len(tc.want)); string(got) != tc.want {
				t.Errorf("server received %q, want %q", got, tc.want)
			}
			expectQuiet(t, server)
		})
	}
}

func listen(t *testing.T) (net.Listener, <-chan net.Conn) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	conns := make(chan net.Conn, 1)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		conns <- c
	}()
	return ln, conns
}

func TestDialSendsAfterConnect(t *testing.T) {
	ln, conns := listen(t)

	conn, err := Dial(context.Background(), ln.Addr().String(),
		Options{Send: "neo\rpass\xff\r\n"}, time.Second)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer conn.Close()
	server := <-conns
	defer server.Close()

	want := "neo\r\x00pass\xff\xff\r\n"
	if got := readN(t, server, len(want)); string(got) != want {
		t.Errorf("server received %q, want %q", got, want)
	}
}

// Raw mode is plain TCP: what looks like negotiation is door output, and what
// the caller types is sent as typed.
func TestDialRawLeavesEveryByteAlone(t *testing.T) {
	ln, conns := listen(t)

	conn, err := Dial(context.Background(), ln.Addr().String(),
		Options{Raw: true, Send: "go\r"}, time.Second)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer conn.Close()
	server := <-conns
	defer server.Close()

	if got := readN(t, server, 3); string(got) != "go\r" {
		t.Errorf("server received %q, want %q", got, "go\r")
	}

	sent := []byte{iac, do, optNAWS, '\r', 0, 'x'}
	serverSend(t, server, sent...)
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	got := make([]byte, len(sent))
	if _, err := io.ReadFull(conn, got); err != nil {
		t.Fatalf("client read: %v", err)
	}
	if !bytes.Equal(got, sent) {
		t.Errorf("client read % x, want % x", got, sent)
	}
	expectQuiet(t, server)
}

func TestDialReportsAnUnreachableServer(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	_ = ln.Close() // nothing is listening there now

	if conn, err := Dial(context.Background(), addr, Options{}, time.Second); err == nil {
		_ = conn.Close()
		t.Error("Dial succeeded against a closed port")
	}
}

func TestJoinHostPort(t *testing.T) {
	for _, tc := range []struct {
		host string
		port int
		want string
	}{
		{"doors.example.com", 0, "doors.example.com:23"},
		{"doors.example.com", 2323, "doors.example.com:2323"},
		{"::1", 0, "[::1]:23"},
	} {
		if got := JoinHostPort(tc.host, tc.port); got != tc.want {
			t.Errorf("JoinHostPort(%q, %d) = %q, want %q", tc.host, tc.port, got, tc.want)
		}
	}
}
