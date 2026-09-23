// Package rlogin implements the client side of the BSD RLogin protocol
// (RFC 1282) as BBS door servers actually speak it.
//
// Only connection establishment is implemented. The protocol's later features
// -- cooked mode, out-of-band window-size control sequences -- are not, which
// matches what Synchronet and ENiGMA do: once the handshake is sent the socket
// is a raw byte pipe in both directions. Door servers rely on nothing else, and
// a control sequence we never send cannot be misinterpreted by a door.
package rlogin

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"
)

// DefaultPort is the IANA-assigned port for rlogin.
const DefaultPort = 513

// DefaultTimeout bounds connection establishment when a door configures none.
// It matches Synchronet's rlogin module default.
const DefaultTimeout = 10 * time.Second

// Handshake carries the three NUL-separated strings an rlogin client sends
// immediately after connecting.
//
// Door servers overload all three: ClientUser and ServerUser identify (and
// often authenticate) the caller, and TermType doubles as the door selector --
// Synchronet door servers read "xtrn=CODE" from it, while DoorParty expects a
// bare door code. None of that is interpreted here; the fields are sent as
// given so a sysop can match whatever convention their server documents.
type Handshake struct {
	ClientUser string
	ServerUser string
	TermType   string
}

// Encode renders the handshake as it goes on the wire: a leading empty string
// followed by the three fields, each NUL-terminated.
//
// A NUL inside a field would frame the handshake wrongly and silently shift
// every later field, so an embedded NUL is an error rather than something to
// strip: a handle that cannot be sent faithfully should fail loudly at connect
// time, not arrive at the door server as somebody else.
func (h Handshake) Encode() ([]byte, error) {
	for _, f := range []struct {
		name  string
		value string
	}{
		{"client user name", h.ClientUser},
		{"server user name", h.ServerUser},
		{"terminal type", h.TermType},
	} {
		if strings.ContainsRune(f.value, 0) {
			return nil, fmt.Errorf("rlogin %s contains a NUL byte", f.name)
		}
	}

	var b bytes.Buffer
	b.WriteByte(0) // the protocol opens with an empty string
	for _, f := range []string{h.ClientUser, h.ServerUser, h.TermType} {
		b.WriteString(f)
		b.WriteByte(0)
	}
	return b.Bytes(), nil
}

// JoinHostPort renders host and port as a dial address, supplying DefaultPort
// when port is zero. It is a helper for callers holding the two separately, and
// gets IPv6 literals right, which naive concatenation does not.
func JoinHostPort(host string, port int) string {
	if port <= 0 {
		port = DefaultPort
	}
	return net.JoinHostPort(host, strconv.Itoa(port))
}

// Dial connects to an rlogin server and sends the handshake. The returned
// connection is positioned at the start of the session byte stream and is the
// caller's to close.
//
// The server's protocol acknowledgement is not consumed here -- see AckReader
// for why that is the reader's job rather than an extra round trip.
func Dial(ctx context.Context, addr string, h Handshake, timeout time.Duration) (net.Conn, error) {
	payload, err := h.Encode()
	if err != nil {
		return nil, err
	}
	if timeout <= 0 {
		timeout = DefaultTimeout
	}

	dialCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	conn, err := (&net.Dialer{}).DialContext(dialCtx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("rlogin dial %s: %w", addr, err)
	}

	// The handshake must land before the server will say anything, so a stalled
	// write has to fail rather than hang the caller's session forever.
	if err := conn.SetWriteDeadline(time.Now().Add(timeout)); err != nil {
		_ = conn.Close() // cleanup on error path
		return nil, fmt.Errorf("rlogin %s: set write deadline: %w", addr, err)
	}
	if _, err := conn.Write(payload); err != nil {
		_ = conn.Close() // cleanup on error path
		return nil, fmt.Errorf("rlogin %s: send handshake: %w", addr, err)
	}
	// Clear the deadline: the session that follows has no write timeout, since
	// a user idling in a door is not an error.
	if err := conn.SetWriteDeadline(time.Time{}); err != nil {
		_ = conn.Close() // cleanup on error path
		return nil, fmt.Errorf("rlogin %s: clear write deadline: %w", addr, err)
	}
	return conn, nil
}
