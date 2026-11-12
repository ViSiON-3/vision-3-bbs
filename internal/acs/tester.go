package acs

import (
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/perm"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// TesterSession is a session made up of values the sysop enters in the ACS
// tester: what L, A and T read from a caller's connection. It stands in for
// a call the selected user might make; it is never anyone's real session.
type TesterSession struct {
	Local    bool          // L: the call is local
	Terminal bool          // A: the connection has a terminal
	Elapsed  time.Duration // how long the call has run, for T
	Credit   time.Duration // chat time credited back to the call, for T
}

// RemoteAddr is loopback for a local call and an address from the
// documentation range (192.0.2.0/24) for a remote one.
func (s *TesterSession) RemoteAddr() net.Addr {
	if s.Local {
		return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 23}
	}
	return &net.TCPAddr{IP: net.IPv4(192, 0, 2, 1), Port: 23}
}

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

// ParseTesterTime reads a time typed into the tester, in the server's local
// zone: blank for the current time (the zero Time), hh:mm for that time
// today, or yyyy-mm-dd hh:mm.
func ParseTesterTime(s string, today time.Time) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" || strings.EqualFold(s, "now") {
		return time.Time{}, nil
	}
	for _, layout := range testerTimeLayouts {
		t, err := time.ParseInLocation(layout, s, time.Local)
		if err != nil {
			continue
		}
		if layout == "15:04" {
			y, m, d := today.In(time.Local).Date()
			t = time.Date(y, m, d, t.Hour(), t.Minute(), 0, 0, time.Local)
		}
		return t, nil
	}
	return time.Time{}, fmt.Errorf("%q: enter hh:mm or yyyy-mm-dd hh:mm, or leave it blank for now", s)
}
