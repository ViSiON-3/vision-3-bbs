// Package telnetclient implements the client side of Telnet (RFC 854) as far
// as reaching a door server needs it.
//
// It is the mirror image of internal/telnetserver. The server offers to echo
// and asks for the window size and terminal type; the client here agrees to
// the first and answers the other two. Negotiation is reactive throughout:
// nothing is requested of the server, only answered, so the option state never
// has a request of its own outstanding and cannot fall into a loop with a
// server that repeats itself.
//
// Apart from negotiation the connection is a byte pipe, as the rlogin one is.
// The only data the client rewrites is what the protocol reserves: 0xFF, and
// a carriage return outside binary mode.
package telnetclient

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"time"
)

// DefaultPort is the IANA-assigned port for telnet.
const DefaultPort = 23

// DefaultTimeout bounds connection establishment when a door configures none.
// It matches Synchronet's telnet gateway default, and rlogin's here.
const DefaultTimeout = 10 * time.Second

// DefaultTermType is reported when a door configures no terminal type. It is
// what BBS software expects of a caller who can draw its menus.
const DefaultTermType = "ANSI"

// The window size reported when the caller's is unknown.
const (
	defaultWidth  = 80
	defaultHeight = 24
)

// Options describes the terminal the client reports to the server, and what
// it does once connected.
type Options struct {
	// TermType answers the server's TERMINAL-TYPE request. Empty means
	// DefaultTermType.
	TermType string

	// Width and Height answer the server's window-size request. A value that
	// is not positive means 80 or 24.
	Width, Height int

	// Send is written to the server as soon as the connection is up, before
	// anything the caller types. Door servers that ask for a login take it
	// from here.
	Send string

	// Raw turns the protocol off: no negotiation is answered and no byte is
	// rewritten in either direction. It is for servers that speak plain TCP on
	// a port a sysop would expect telnet on.
	Raw bool
}

// JoinHostPort renders host and port as a dial address, supplying DefaultPort
// when port is zero.
func JoinHostPort(host string, port int) string {
	if port <= 0 {
		port = DefaultPort
	}
	return net.JoinHostPort(host, strconv.Itoa(port))
}

// Dial connects to a telnet server and sends opts.Send. The returned
// connection answers the server's negotiation as it is read from, and is the
// caller's to close.
//
// Nothing is read here. A server's opening negotiation arrives with its first
// output, and waiting for it would stall against one that sends none.
func Dial(ctx context.Context, addr string, opts Options, timeout time.Duration) (net.Conn, error) {
	if timeout <= 0 {
		timeout = DefaultTimeout
	}

	dialCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	raw, err := (&net.Dialer{}).DialContext(dialCtx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("telnet dial %s: %w", addr, err)
	}

	conn := raw
	if !opts.Raw {
		conn = NewConn(raw, opts)
	}
	if opts.Send == "" {
		return conn, nil
	}

	// A stalled write has to fail rather than hang the caller's session.
	if err := conn.SetWriteDeadline(time.Now().Add(timeout)); err != nil {
		_ = conn.Close() // cleanup on error path
		return nil, fmt.Errorf("telnet %s: set write deadline: %w", addr, err)
	}
	if _, err := conn.Write([]byte(opts.Send)); err != nil {
		_ = conn.Close() // cleanup on error path
		return nil, fmt.Errorf("telnet %s: send after connect: %w", addr, err)
	}
	// Clear the deadline: the session that follows has no write timeout, since
	// a user idling in a door is not an error.
	if err := conn.SetWriteDeadline(time.Time{}); err != nil {
		_ = conn.Close() // cleanup on error path
		return nil, fmt.Errorf("telnet %s: clear write deadline: %w", addr, err)
	}
	return conn, nil
}
