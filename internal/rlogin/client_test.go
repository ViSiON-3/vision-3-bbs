package rlogin

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"testing"
	"time"
)

func TestHandshakeEncodeWireFormat(t *testing.T) {
	h := Handshake{ClientUser: "robbie", ServerUser: "[V3]robbie", TermType: "xtrn=LORD"}
	got, err := h.Encode()
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	want := []byte("\x00robbie\x00[V3]robbie\x00xtrn=LORD\x00")
	if !bytes.Equal(got, want) {
		t.Errorf("handshake = %q, want %q", got, want)
	}
}

// Empty fields still occupy their slot: a server counting NUL separators must
// find four of them or it will read the fields shifted by one.
func TestHandshakeEncodeKeepsEmptyFieldSlots(t *testing.T) {
	got, err := Handshake{}.Encode()
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if want := []byte("\x00\x00\x00\x00"); !bytes.Equal(got, want) {
		t.Errorf("handshake = %q, want %q", got, want)
	}
}

// A NUL in a field would silently shift every later field, so it must fail
// rather than reach the door server as a different user.
func TestHandshakeEncodeRejectsEmbeddedNUL(t *testing.T) {
	for name, h := range map[string]Handshake{
		"client": {ClientUser: "bad\x00user"},
		"server": {ServerUser: "bad\x00user"},
		"term":   {TermType: "bad\x00term"},
	} {
		if _, err := h.Encode(); err == nil {
			t.Errorf("%s field with embedded NUL: expected an error", name)
		}
	}
}

func TestJoinHostPort(t *testing.T) {
	for _, tc := range []struct {
		host string
		port int
		want string
	}{
		{"door.example.com", 0, "door.example.com:513"},
		{"door.example.com", 5000, "door.example.com:5000"},
		{"::1", 0, "[::1]:513"},
	} {
		if got := JoinHostPort(tc.host, tc.port); got != tc.want {
			t.Errorf("JoinHostPort(%q, %d) = %q, want %q", tc.host, tc.port, got, tc.want)
		}
	}
}

// Dial must deliver the handshake before the caller relays anything, since the
// server will not respond until it has been read.
func TestDialSendsHandshake(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	got := make(chan []byte, 1)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		buf := make([]byte, 128)
		n, _ := c.Read(buf)
		got <- append([]byte(nil), buf[:n]...)
	}()

	conn, err := Dial(context.Background(), ln.Addr().String(),
		Handshake{ClientUser: "pw", ServerUser: "[V3]robbie", TermType: "ANSI/38400"}, time.Second)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer conn.Close()

	select {
	case b := <-got:
		if want := "\x00pw\x00[V3]robbie\x00ANSI/38400\x00"; string(b) != want {
			t.Errorf("server received %q, want %q", b, want)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for the handshake")
	}
}

// A bad field must fail before a socket is opened, so a misconfigured door
// never reaches the door server at all.
func TestDialRejectsBadHandshakeWithoutConnecting(t *testing.T) {
	// Port 0 on a valid host would fail to dial; if Encode is checked first
	// the error is about the NUL, not about the connection.
	_, err := Dial(context.Background(), "127.0.0.1:1",
		Handshake{ClientUser: "bad\x00user"}, time.Second)
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "NUL") {
		t.Errorf("error = %v, want it to name the NUL byte", err)
	}
}

func TestDialTimesOutOnUnreachableServer(t *testing.T) {
	// A listener that never accepts still completes the TCP handshake via the
	// backlog, so use an address with nothing listening at all.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	ln.Close() // nothing is listening on addr any more

	_, err = Dial(context.Background(), addr, Handshake{}, 500*time.Millisecond)
	if err == nil {
		t.Fatal("expected a connection error")
	}
}

func TestAckReaderStripsLeadingNUL(t *testing.T) {
	r := NewAckReader(strings.NewReader("\x00Welcome to the door\x00server"))
	got, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	// Only the first byte is the acknowledgement; a later NUL is door output.
	if want := "Welcome to the door\x00server"; string(got) != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestAckReaderPassesThroughWhenNoAckSent(t *testing.T) {
	r := NewAckReader(strings.NewReader("no ack here"))
	got, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if string(got) != "no ack here" {
		t.Errorf("got %q, want %q", got, "no ack here")
	}
}

// A server that sends the ack on its own, before any door output, must not
// surface as a zero-length read to the relay.
func TestAckReaderHandlesAckArrivingAlone(t *testing.T) {
	pr, pw := io.Pipe()
	go func() {
		pw.Write([]byte{0})
		pw.Write([]byte("door output"))
		pw.Close()
	}()

	r := NewAckReader(pr)
	buf := make([]byte, 64)
	n, err := r.Read(buf)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if n == 0 {
		t.Fatal("first read returned 0 bytes; the relay would see a spin")
	}
	if string(buf[:n]) != "door output" {
		t.Errorf("got %q, want %q", buf[:n], "door output")
	}
}

func TestAckReaderPropagatesError(t *testing.T) {
	want := errors.New("connection reset")
	r := NewAckReader(&errReader{err: want})
	if _, err := r.Read(make([]byte, 8)); !errors.Is(err, want) {
		t.Errorf("err = %v, want %v", err, want)
	}
}

type errReader struct{ err error }

func (e *errReader) Read([]byte) (int, error) { return 0, e.err }
