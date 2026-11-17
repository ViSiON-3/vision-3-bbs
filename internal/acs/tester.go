package acs

import (
	"cmp"
	"fmt"
	"net"
	"net/netip"
	"strings"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/perm"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// TesterSession is a session made up of values the sysop enters in the ACS
// tester: what the session terms and T read from a caller's connection. It
// stands in for a call the selected user might make; it is never anyone's
// real session. The zero values of NodeNum, Conn, Enc and Cols read as node
// 1, SSH (local for a local call), CP437 and 80 columns.
type TesterSession struct {
	Local    bool          // L: the call is local
	Terminal bool          // A: the connection has a terminal
	Elapsed  time.Duration // how long the call has run, for T
	Credit   time.Duration // chat time credited back to the call, for T
	NodeNum  int           // NODE
	Conn     string        // CONN: TransportSSH, TransportTelnet or TransportLocal
	Enc      string        // ENC: EncodingUTF8 or EncodingCP437
	Cols     int           // WIDTH
	// Addr is a remote call's address, for IP. Unset, it is
	// TesterDefaultAddr. A local call's address is always loopback, so L
	// and IP agree.
	Addr netip.Addr
}

// TesterDefaultAddr is a remote made-up call's address when the sysop
// gives none: one from the documentation range.
var TesterDefaultAddr = netip.MustParseAddr("192.0.2.1")

// RemoteAddr is loopback for a local call, else Addr, or TesterDefaultAddr
// when Addr is unset.
func (s *TesterSession) RemoteAddr() net.Addr {
	switch {
	case s.Local:
		return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 23}
	case s.Addr.IsValid():
		return &net.TCPAddr{IP: net.IP(s.Addr.AsSlice()), Port: 23}
	}
	return &net.TCPAddr{IP: net.IP(TesterDefaultAddr.AsSlice()), Port: 23}
}

// Node is NodeNum, 1 when unset.
func (s *TesterSession) Node() int { return cmp.Or(s.NodeNum, 1) }

// Transport is Conn. Unset, it is local for a local call, so L and CONN
// agree, else SSH.
func (s *TesterSession) Transport() string {
	if s.Conn == "" && s.Local {
		return TransportLocal
	}
	return cmp.Or(s.Conn, TransportSSH)
}

// Encoding is Enc, CP437 when unset.
func (s *TesterSession) Encoding() string { return cmp.Or(s.Enc, EncodingCP437) }

// Width is Cols, 80 when unset.
func (s *TesterSession) Width() int { return cmp.Or(s.Cols, 80) }

// HasPTY reports Terminal.
func (s *TesterSession) HasPTY() bool { return s.Terminal }

// ChatCredit reports Credit.
func (s *TesterSession) ChatCredit() time.Duration { return s.Credit }

// Tester is the context the ACS tester checks an expression in. Its zero
// value is the default: no session, the current time.
type Tester struct {
	// Session is the made-up call, nil for none.
	Session *TesterSession
	// At is the time to check at, zero for the current time.
	At time.Time
	// SysOpLevel and CoSysOpLevel are sysOpLevel and coSysOpLevel from
	// config.json, as the BBS uses them.
	SysOpLevel, CoSysOpLevel int
	// Location is the board's timezone, as for Context.Location.
	Location *time.Location
	// Roles are configs/roles.json's roles, as for Context.Roles: nil is the
	// running BBS's set, which ./ue, a separate process, must not rely on.
	Roles *perm.Roles
}

// Context is the Context t describes for u. Without a session the call is
// taken to have just started, as for the checks the BBS makes outside a
// call; with one, it started Session.Elapsed before the time checked at.
func (t Tester) Context(u *user.User) Context {
	now := t.At
	if now.IsZero() {
		now = time.Now()
	}
	ctx := Context{User: u, Start: now, Now: now, SysOpLevel: t.SysOpLevel, CoSysOpLevel: t.CoSysOpLevel, Location: t.Location, Roles: t.Roles}
	if t.Session != nil {
		ctx.Session = t.Session
		ctx.Start = now.Add(-t.Session.Elapsed)
	}
	return ctx
}

// Explain explains s for u in t's context.
func (t Tester) Explain(s string, u *user.User) Explanation {
	x := Explain(s, t.Context(u))
	x.CurrentTime = t.At.IsZero()
	return x
}

// testerTimeLayouts are the forms ParseTesterTime accepts.
var testerTimeLayouts = []string{"2006-01-02 15:04", "15:04"}

// ParseTesterTime reads a time typed into the tester, on the board's clock
// (loc, nil for the server's local zone), the one the clock terms read:
// blank for the current time (the zero Time), hh:mm for that time today, or
// yyyy-mm-dd hh:mm.
func ParseTesterTime(s string, today time.Time, loc *time.Location) (time.Time, error) {
	if loc == nil {
		loc = time.Local
	}
	s = strings.TrimSpace(s)
	if s == "" || strings.EqualFold(s, "now") {
		return time.Time{}, nil
	}
	for _, layout := range testerTimeLayouts {
		t, err := time.ParseInLocation(layout, s, loc)
		if err != nil {
			continue
		}
		if layout == "15:04" {
			y, m, d := today.In(loc).Date()
			t = time.Date(y, m, d, t.Hour(), t.Minute(), 0, 0, loc)
		}
		return t, nil
	}
	return time.Time{}, fmt.Errorf("%q: enter hh:mm or yyyy-mm-dd hh:mm, or leave it blank for now", s)
}
