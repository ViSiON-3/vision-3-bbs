package acs

import (
	"net"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// sessionCtx is a context for a level 20 user on s, at a fixed time on a
// UTC board.
func sessionCtx(s *TesterSession) Context {
	now := time.Date(2026, 11, 4, 22, 0, 0, 0, time.UTC)
	ctx := Context{User: &user.User{AccessLevel: 20}, Now: now, Start: now, Location: time.UTC}
	if s != nil {
		ctx.Session = s
	}
	return ctx
}

// The session terms read the call: its node, transport, encoding, width
// and address.
func TestSessionTerms(t *testing.T) {
	s := &TesterSession{NodeNum: 3, Conn: TransportTelnet, Enc: EncodingUTF8, Cols: 132,
		Addr: netip.MustParseAddr("10.1.2.3")}
	ctx := sessionCtx(s)
	for acs, want := range map[string]bool{
		"NODE=3": true, "NODE=2": false, "NODE=1-4": true, "NODE>3": false, "node<=3": true,
		"CONN=telnet": true, "CONN=ssh": false, "conn=TELNET": true,
		"ENC=utf8": true, "ENC=cp437": false,
		"WIDTH>=80": true, "WIDTH=132": true, "WIDTH<100": false, "WIDTH=80-131": false,
		"IP:10.0.0.0/8": true, "IP:10.1.2.3": true, "IP:10.1.2.4": false, "IP:192.168.0.0/16": false,
		"IP:::ffff:10.0.0.0/104": true, "IP:fd00::/8": false,
		"NODE=3&CONN=telnet&ENC=utf8": true, "!CONN=ssh": true,
	} {
		if got := Check(acs, ctx); got != want {
			t.Errorf("Check(%q) = %v, want %v", acs, got, want)
		}
	}
}

// An IPv4 address mapped into IPv6, as a dual-stack listener reports it,
// matches its IPv4 network; IPv6 networks match IPv6 addresses.
func TestIPMatchesMappedAndV6(t *testing.T) {
	mapped := sessionCtx(&TesterSession{Addr: netip.MustParseAddr("::ffff:10.0.0.1")})
	if !Check("IP:10.0.0.0/8", mapped) {
		t.Error("::ffff:10.0.0.1 is not in 10.0.0.0/8")
	}
	v6 := sessionCtx(&TesterSession{Addr: netip.MustParseAddr("fd12::5")})
	if !Check("IP:fd00::/8", v6) || Check("IP:10.0.0.0/8", v6) {
		t.Error("fd12::5 against fd00::/8 and 10.0.0.0/8")
	}
}

// CONN=local is a local transport only. L also counts a loopback address,
// so an SSH call from 127.0.0.1 is L but not CONN=local.
func TestConnLocalDiffersFromL(t *testing.T) {
	loopSSH := sessionCtx(&TesterSession{Local: true, Conn: TransportSSH})
	if !Check("L", loopSSH) || Check("CONN=local", loopSSH) {
		t.Error("an SSH call over loopback: want L, not CONN=local")
	}
	pipe := sessionCtx(&TesterSession{Local: true, Conn: TransportLocal})
	if !Check("CONN=local", pipe) {
		t.Error("a local transport isn't CONN=local")
	}
}

// Without a session every session term fails the whole string, whatever
// '!' or another branch of '|' says.
func TestSessionTermsNeedASession(t *testing.T) {
	ctx := sessionCtx(nil)
	for _, term := range []string{"NODE=1", "CONN=ssh", "ENC=cp437", "WIDTH>=80", "IP:10.0.0.0/8"} {
		for _, acs := range []string{term, "!" + term, "S10|" + term, "S10|!" + term} {
			if Check(acs, ctx) {
				t.Errorf("Check(%q) passed with no session", acs)
			}
		}
		if !Parse(term).UsesSession() {
			t.Errorf("%s doesn't count as a session term", term)
		}
		x := Explain("S10|!"+term, ctx)
		if x.Result || !strings.Contains(strings.Join(Render(x, 79), "\n"), "no session") {
			t.Errorf("Explain(S10|!%s): result %v, no 'no session' in the tester", term, x.Result)
		}
	}
}

// MONTH and DATE read the calendar, and a range whose end comes first runs
// over the new year.
func TestCalendarTerms(t *testing.T) {
	at := func(m time.Month, d int) Context {
		ctx := sessionCtx(nil)
		ctx.Now = time.Date(2026, m, d, 12, 0, 0, 0, time.UTC)
		return ctx
	}
	for _, tc := range []struct {
		acs  string
		at   Context
		want bool
	}{
		{"MONTH=11", at(11, 4), true},
		{"MONTH=12", at(11, 4), false},
		{"MONTH=11-2", at(1, 15), true},
		{"MONTH=11-2", at(3, 1), false},
		{"MONTH>=6", at(11, 4), true},
		{"DATE:12-20/01-05", at(12, 31), true},
		{"DATE:12-20/01-05", at(1, 5), true},
		{"DATE:12-20/01-05", at(1, 6), false},
		{"DATE:11-01/11-30", at(11, 4), true},
		{"DATE:11-05/11-30", at(11, 4), false},
		{"DATE:02-29/02-29", Context{User: &user.User{}, Now: time.Date(2028, 2, 29, 9, 0, 0, 0, time.UTC), Location: time.UTC}, true},
	} {
		if got := Check(tc.acs, tc.at); got != tc.want {
			t.Errorf("Check(%q) on %s = %v, want %v", tc.acs, tc.at.Now.Format("01-02"), got, tc.want)
		}
	}
}

// Every clock term reads the board's zone. 22:00 UTC on 31 December is
// 11:00 on 1 January in Auckland.
func TestClockTermsUseBoardZone(t *testing.T) {
	auckland, err := time.LoadLocation("Pacific/Auckland")
	if err != nil {
		t.Skip("no zone data:", err)
	}
	ctx := Context{User: &user.User{}, Now: time.Date(2026, 12, 31, 22, 0, 0, 0, time.UTC), Location: auckland}
	for _, acs := range []string{"H11", "W5", "Y10:30/11:30", "MONTH=1", "DATE:01-01/01-01"} {
		if !Check(acs, ctx) {
			t.Errorf("Check(%q) failed on Auckland's clock (Fri 1 Jan 11:00)", acs)
		}
	}
	for _, acs := range []string{"H22", "W4", "MONTH=12", "DATE:12-31/12-31"} {
		if Check(acs, ctx) {
			t.Errorf("Check(%q) passed: it read the UTC clock", acs)
		}
	}
	if got := strings.Join(Render(Tester{At: ctx.Now, Location: auckland}.Explain("H11", &user.User{}), 79), "\n"); !strings.Contains(got, "Fri 2027-01-01 11:00 (Pacific/Auckland)") {
		t.Errorf("the tester doesn't show the board's clock and zone:\n%s", got)
	}
}

// A check reads the clock once, so two clock terms can't straddle a minute.
func TestCheckFixesTheTime(t *testing.T) {
	ctx := Context{User: &user.User{}}
	if f := ctx.fixed(); f.Now.IsZero() || f.Roles == nil {
		t.Errorf("fixed() left Now %v, Roles %v", f.Now, f.Roles)
	}
}

// Bad arguments are reported where the string is checked, and fail it.
func TestNewTermsReportBadArguments(t *testing.T) {
	for term, want := range map[string]string{
		"MONTH=13":         "a month from 1",
		"MONTH=0":          "a month from 1",
		"NODE=x":           "whole number",
		"WIDTH>=wide":      "whole number",
		"CONN=ftp":         "one of ssh, telnet, local",
		"CONN>ssh":         "one of ssh, telnet, local",
		"ENC=latin1":       "one of utf8, cp437",
		"IP:10.0.0.0/33":   "address/bits",
		"IP:host.example":  "address/bits",
		"IP:":              "address/bits",
		"DATE:13-01/01-01": "mm-dd/mm-dd",
		"DATE:02-30/03-01": "mm-dd/mm-dd",
		"DATE:1-1/2-2":     "mm-dd/mm-dd",
		"DATE:12-20":       "mm-dd/mm-dd",
	} {
		d := Validate(term)
		if len(d) == 0 || !strings.Contains(d[0].Message, want) {
			t.Errorf("Validate(%q) = %v, want a message with %q", term, d, want)
		}
	}
	// The legacy I, read with an argument, keeps its meaning: only IP: is
	// the address term.
	if _, p := parseTerm("IP"); p != "" {
		t.Errorf("IP (I with an argument) = %q", p)
	}
}

// The tester's session reads the defaults for what the sysop leaves unset,
// and its times are on the board's clock.
func TestTesterSessionDefaults(t *testing.T) {
	s := &TesterSession{}
	if s.Node() != 1 || s.Transport() != TransportSSH || s.Encoding() != EncodingCP437 || s.Width() != 80 {
		t.Errorf("defaults: node %d %s %s %d", s.Node(), s.Transport(), s.Encoding(), s.Width())
	}
	if got := s.RemoteAddr().String(); got != "192.0.2.1:23" {
		t.Errorf("remote address %s", got)
	}
	// A local call left at the default connection is CONN=local, so the
	// made-up call doesn't contradict itself; one set to SSH stays SSH.
	if got := Check("L&CONN=local", sessionCtx(&TesterSession{Local: true})); !got {
		t.Error("a local call with no connection set is not CONN=local")
	}
	if got := (&TesterSession{Local: true, Conn: TransportSSH}).Transport(); got != TransportSSH {
		t.Errorf("a local call set to ssh reads %s", got)
	}
	loc := time.FixedZone("X", 5*3600)
	at, err := ParseTesterTime("2026-11-04 10:00", time.Now(), loc)
	if err != nil || at.Location() != loc || at.Hour() != 10 {
		t.Errorf("ParseTesterTime in the board's zone = %v, %v", at, err)
	}
}

// addrSession is a tester session whose remote address is set outright,
// nil included.
type addrSession struct {
	*TesterSession
	addr net.Addr
}

func (s addrSession) RemoteAddr() net.Addr { return s.addr }

// L reads the address as IP does: a loopback address mapped into IPv6 is
// local, so L and IP:127.0.0.0/8 agree.
func TestLocalReadsMappedLoopback(t *testing.T) {
	mapped := &net.TCPAddr{IP: net.ParseIP("::ffff:127.0.0.1"), Port: 5000}
	ctx := sessionCtx(nil)
	ctx.Session = addrSession{&TesterSession{}, mapped}
	if !Check("L", ctx) || !Check("IP:127.0.0.0/8", ctx) {
		t.Error("[::ffff:127.0.0.1]:5000: want L and IP:127.0.0.0/8")
	}
	ctx.Session = addrSession{&TesterSession{}, &net.TCPAddr{IP: net.ParseIP("10.0.0.1"), Port: 5000}}
	if Check("L", ctx) {
		t.Error("10.0.0.1 counted as local")
	}
}

// A session with no remote address fails L and IP:, and the tester renders
// and explains it without a panic.
func TestNoRemoteAddress(t *testing.T) {
	for _, addr := range []net.Addr{nil, (*net.TCPAddr)(nil)} {
		ctx := sessionCtx(nil)
		ctx.Session = addrSession{&TesterSession{}, addr}
		if Check("L", ctx) || Check("IP:10.0.0.0/8", ctx) {
			t.Errorf("%#v: L or IP: passed", addr)
		}
		if x := Explain("IP:10.0.0.0/8|L", ctx); x.Result {
			t.Errorf("%#v: explained as a pass", addr)
		}
		if got := describeCall(ctx.Session); !strings.Contains(got, "no IP address") {
			t.Errorf("%#v: call reads %q", addr, got)
		}
	}
}

// An IPv4-mapped network shorter than /96 reaches past the IPv4 part, so
// no address could match it: it is refused, not left to deny everyone.
func TestShortMappedPrefixRefused(t *testing.T) {
	d := Validate("IP:::ffff:0.0.0.0/80")
	if len(d) == 0 || !strings.Contains(d[0].Message, "shorter than /96") {
		t.Errorf("Validate = %v", d)
	}
	if d := Validate("IP:::ffff:10.0.0.0/104"); len(d) != 0 {
		t.Errorf("a /104 mapped network: %v", d)
	}
}
