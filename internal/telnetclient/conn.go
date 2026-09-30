package telnetclient

import (
	"net"
	"sync"
	"sync/atomic"

	"github.com/ViSiON-3/vision-3-bbs/internal/telnetserver"
)

// The protocol bytes are the server's, so the two sides cannot drift apart.
const (
	iac  = telnetserver.IAC
	dont = telnetserver.DONT
	do   = telnetserver.DO
	wont = telnetserver.WONT
	will = telnetserver.WILL
	sb   = telnetserver.SB
	se   = telnetserver.SE

	optBinary   byte = 0 // Binary Transmission (RFC 856), which the server never negotiates
	optEcho          = telnetserver.OptEcho
	optSGA           = telnetserver.OptSGA
	optTermType      = telnetserver.OptTermType
	optNAWS          = telnetserver.OptNAWS

	termTypeIs   = telnetserver.TermTypeIs
	termTypeSend = telnetserver.TermTypeSend
)

// maxSubnegotiation caps how much of a subnegotiation is kept. The only one
// read is TERMINAL-TYPE SEND, a single byte, so anything longer is a server
// sending something the client would ignore anyway.
const maxSubnegotiation = 64

type readState int

const (
	stateData readState = iota
	stateIAC
	stateWill
	stateWont
	stateDo
	stateDont
	stateSB
	stateSBData
	stateSBIAC
)

// Conn is a connection to a telnet server. Read strips the protocol from the
// server's output and answers its negotiation; Write escapes what the caller
// sends.
//
// One goroutine may read while another writes, which is how the door relay
// uses it. Read is not safe for concurrent use with itself.
type Conn struct {
	net.Conn
	opts Options

	writeMu sync.Mutex // serialises caller data with negotiation replies

	// Read-side state, touched only by Read.
	state   readState
	sbOpt   byte
	sbData  []byte
	lastCR  bool      // the previous data byte was a CR
	local   [256]bool // options this end has agreed to perform
	remote  [256]bool // options the server has been told to perform
	replies []byte    // negotiation answers waiting to be written

	// binaryTX mirrors local[optBinary] for Write, which runs on another
	// goroutine.
	binaryTX atomic.Bool
}

// NewConn wraps a connected socket with the client side of the protocol.
func NewConn(conn net.Conn, opts Options) *Conn {
	if opts.TermType == "" {
		opts.TermType = DefaultTermType
	}
	if opts.Width <= 0 {
		opts.Width = defaultWidth
	}
	if opts.Height <= 0 {
		opts.Height = defaultHeight
	}
	return &Conn{Conn: conn, opts: opts}
}

// Read returns the server's output with the protocol removed.
func (c *Conn) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	// Loop so a read consisting of nothing but negotiation does not surface
	// as a zero-length read, which a caller may treat as a spin.
	for {
		n, err := c.Conn.Read(p)
		out := c.filter(p[:n])
		if werr := c.flushReplies(); werr != nil && err == nil {
			err = werr
		}
		if out > 0 || err != nil {
			return out, err
		}
	}
}

// filter removes the protocol from buf in place and returns how many data
// bytes are left at the front of it. Output never outruns input, so writing
// into the buffer being read is safe.
func (c *Conn) filter(buf []byte) int {
	out := 0
	for _, b := range buf {
		switch c.state {
		case stateData:
			if b == iac {
				c.state = stateIAC
				continue
			}
			// Outside binary mode a CR is always followed by LF or NUL, and
			// the NUL is padding rather than data (RFC 854).
			if c.lastCR && b == 0 && !c.remote[optBinary] {
				c.lastCR = false
				continue
			}
			c.lastCR = b == '\r'
			buf[out] = b
			out++

		case stateIAC:
			c.state = stateData
			switch b {
			case iac: // an escaped 0xFF is data
				c.lastCR = false
				buf[out] = b
				out++
			case will:
				c.state = stateWill
			case wont:
				c.state = stateWont
			case do:
				c.state = stateDo
			case dont:
				c.state = stateDont
			case sb:
				c.state = stateSB
			}
			// Every other command (NOP, GA, AYT and the rest) takes no option
			// and asks nothing of a door relay.

		case stateWill:
			c.state = stateData
			c.handleWill(b)
		case stateWont:
			c.state = stateData
			c.handleWont(b)
		case stateDo:
			c.state = stateData
			c.handleDo(b)
		case stateDont:
			c.state = stateData
			c.handleDont(b)

		case stateSB:
			c.sbOpt = b
			c.sbData = c.sbData[:0]
			c.state = stateSBData
		case stateSBData:
			if b == iac {
				c.state = stateSBIAC
				continue
			}
			c.appendSubnegotiation(b)
		case stateSBIAC:
			switch b {
			case se:
				c.state = stateData
				c.handleSubnegotiation()
			case iac:
				c.state = stateSBData
				c.appendSubnegotiation(b)
			default:
				// Not a sequence the protocol defines. Staying inside the
				// subnegotiation until its real end keeps the bytes that
				// follow from being read as door output.
				c.state = stateSBData
			}
		}
	}
	return out
}

func (c *Conn) appendSubnegotiation(b byte) {
	if len(c.sbData) < maxSubnegotiation {
		c.sbData = append(c.sbData, b)
	}
}

// handleDo answers a request that this end perform an option.
//
// An option already on is not acknowledged again: a server that repeats its
// request would otherwise be answered every time, and two ends doing that to
// each other never stop.
func (c *Conn) handleDo(opt byte) {
	switch opt {
	case optBinary, optSGA, optTermType, optNAWS:
		if c.local[opt] {
			return
		}
		c.local[opt] = true
		c.reply(will, opt)
		switch opt {
		case optBinary:
			c.binaryTX.Store(true)
		case optNAWS:
			c.replyWindowSize()
		}
	default:
		c.reply(wont, opt)
	}
}

func (c *Conn) handleDont(opt byte) {
	if !c.local[opt] {
		return
	}
	c.local[opt] = false
	if opt == optBinary {
		c.binaryTX.Store(false)
	}
	c.reply(wont, opt)
}

// handleWill answers the server's offer to perform an option. Echo is the one
// that matters: a BBS echoes what the caller types, and refusing the offer
// would leave them typing blind.
func (c *Conn) handleWill(opt byte) {
	switch opt {
	case optBinary, optEcho, optSGA:
		if c.remote[opt] {
			return
		}
		c.remote[opt] = true
		c.reply(do, opt)
	default:
		c.reply(dont, opt)
	}
}

func (c *Conn) handleWont(opt byte) {
	if !c.remote[opt] {
		return
	}
	c.remote[opt] = false
	c.reply(dont, opt)
}

// handleSubnegotiation answers the one subnegotiation a server sends a
// client: the request for its terminal type.
func (c *Conn) handleSubnegotiation() {
	if c.sbOpt != optTermType || len(c.sbData) == 0 || c.sbData[0] != termTypeSend {
		return
	}
	c.replies = append(c.replies, iac, sb, optTermType, termTypeIs)
	for i := 0; i < len(c.opts.TermType); i++ {
		// RFC 1091 allows printable ASCII only. Anything else is dropped
		// rather than sent: 0xFF here would end the subnegotiation early.
		if b := c.opts.TermType[i]; b > ' ' && b < 0x7F {
			c.replies = append(c.replies, b)
		}
	}
	c.replies = append(c.replies, iac, se)
}

func (c *Conn) reply(cmd, opt byte) {
	c.replies = append(c.replies, iac, cmd, opt)
}

// replyWindowSize reports the caller's screen, as two 16-bit values.
func (c *Conn) replyWindowSize() {
	c.replies = append(c.replies, iac, sb, optNAWS)
	for _, v := range []int{c.opts.Width, c.opts.Height} {
		if v > 0xFFFF {
			v = 0xFFFF
		}
		for _, b := range []byte{byte(v >> 8), byte(v)} {
			c.replies = append(c.replies, b)
			if b == iac {
				c.replies = append(c.replies, iac) // 0xFF is escaped here too
			}
		}
	}
	c.replies = append(c.replies, iac, se)
}

// flushReplies writes the answers filter collected. They go out in one write,
// under the lock Write holds, so they cannot land in the middle of what the
// caller is sending.
func (c *Conn) flushReplies() error {
	if len(c.replies) == 0 {
		return nil
	}
	c.writeMu.Lock()
	_, err := c.Conn.Write(c.replies)
	c.writeMu.Unlock()
	c.replies = c.replies[:0]
	return err
}

// Write sends caller input to the server, escaping what the protocol
// reserves.
//
// Outside binary mode a bare CR is sent as CR NUL, as RFC 854 requires. A CR
// already followed by LF or NUL is left as it is. The pair is only recognised
// within one write: a terminal that sends CR and LF separately has the CR
// padded before the LF arrives, which a server reads as the line end it is
// followed by a stray LF.
func (c *Conn) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	binary := c.binaryTX.Load()

	out := make([]byte, 0, len(p)+8)
	for i, b := range p {
		out = append(out, b)
		switch {
		case b == iac:
			out = append(out, iac)
		case b == '\r' && !binary:
			if i+1 < len(p) && (p[i+1] == '\n' || p[i+1] == 0) {
				continue
			}
			out = append(out, 0)
		}
	}

	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if _, err := c.Conn.Write(out); err != nil {
		// How much of p the bytes that did go out amount to is not worth
		// working back from the escaped form; the connection is finished.
		return 0, err
	}
	return len(p), nil
}
