package acs

import (
	"log/slog"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// Session is what the session terms read from a caller's connection. The BBS
// adapts its telnet and SSH sessions to it; a check made outside a call
// (default areas at login, QWK API uploads, menu art) has none.
type Session interface {
	// RemoteAddr is the connection's remote address, for L.
	RemoteAddr() net.Addr
	// HasPTY reports whether the connection has a terminal, for A.
	HasPTY() bool
	// ChatCredit is the sysop chat time credited back to the call, for T.
	ChatCredit() time.Duration
}

// Context is what an expression is evaluated against.
type Context struct {
	// User is the caller. Check fails every non-empty string other than "*"
	// for a nil User; Eval needs a non-nil one.
	User *user.User
	// Session is the caller's connection, nil when there is none: L and A
	// are then false and T counts no chat credit.
	Session Session
	// Start is when the call began, for T.
	Start time.Time
	// Now, when set, is the time H, W, Y and T use; zero means the current
	// time, read when each term is evaluated.
	Now time.Time
	// Location is the board's timezone (its timezone setting), which AGE counts
	// calendar days in; nil is the server's local zone.
	Location *time.Location
	// SysOpLevel and CoSysOpLevel are the thresholds for SYSOP and COSYSOP
	// and the level from which T always passes (sysOpLevel and coSysOpLevel
	// in config.json).
	SysOpLevel, CoSysOpLevel int
}

func (c Context) location() *time.Location {
	if c.Location == nil {
		return time.Local
	}
	return c.Location
}

func (c Context) now() time.Time {
	if c.Now.IsZero() {
		return time.Now()
	}
	return c.Now
}

// Check evaluates the ACS string s against ctx: true when it passes. An empty
// string and a lone "*" pass for everyone, including a nil User; anything
// else fails for a nil User. A string that can't be read as a whole fails.
func Check(s string, ctx Context) bool {
	if s == "" || s == "*" {
		return true
	}
	if ctx.User == nil {
		return false
	}
	u := ctx.User
	slog.Debug("checking ACS", "acs", s, "handle", u.Handle, "level", u.AccessLevel, "flags", u.Flags, "id", u.ID, "validated", u.Validated)
	e := Parse(s)
	warnOnce(e)
	if !e.Valid() {
		for _, d := range e.Diagnostics {
			if d.Severity == Error {
				slog.Error("invalid ACS string", "acs", s, "error", d.String())
			}
		}
		return false
	}
	result := e.Eval(ctx)
	slog.Debug("ACS check result", "acs", s, "result", result)
	return result
}

// warned holds the strings whose warnings Check has logged.
var warned sync.Map

// warnOnce logs e's warnings the first time Check sees its string, so a
// string from a source the startup scan doesn't read still leaves a trace of
// what the parser dropped, without a line per evaluation.
func warnOnce(e *Expr) {
	var msgs []string
	for _, d := range e.Diagnostics {
		if d.Severity == Warning {
			msgs = append(msgs, d.String())
		}
	}
	if len(msgs) == 0 {
		return
	}
	if _, seen := warned.LoadOrStore(e.Source, true); seen {
		return
	}
	slog.Warn("ACS string warning", "acs", e.Source, "warning", strings.Join(msgs, "; "))
}

// Eval evaluates a valid expression against ctx; it is false for an
// expression that isn't Valid. ctx.User must not be nil. Every term is
// evaluated, as the BBS always has, so each logs its own check.
func (e *Expr) Eval(ctx Context) bool {
	if e.Root == nil {
		return false
	}
	return evalNode(e.Root, ctx)
}

func evalNode(n *Node, ctx Context) bool {
	switch n.Kind {
	case TermNode:
		return EvalTerm(n.Text, ctx)
	case NotNode:
		return !evalNode(n.X, ctx)
	case AndNode:
		x, y := evalNode(n.X, ctx), evalNode(n.Y, ctx)
		return x && y
	default:
		x, y := evalNode(n.X, ctx), evalNode(n.Y, ctx)
		return x || y
	}
}

// termProblem says why a term is always false (an unknown letter or a bad
// argument), or "" for a term that is evaluated. It follows EvalTerm: every
// term it accepts, EvalTerm evaluates, and every term it rejects, EvalTerm
// treats as false.
func termProblem(text string) string {
	_, p, _ := parseTerm(text)
	return p
}

// EvalTerm evaluates a single term, such as S50, FA or LEVEL<20, against ctx,
// whose User must not be nil. A term with an unknown letter or a bad argument
// is false and logged as a warning.
func EvalTerm(condition string, ctx Context) bool {
	if len(condition) == 0 {
		slog.Warn("empty condition encountered in ACS string after tokenization")
		return false
	}
	t, problem, _ := parseTerm(condition)
	if problem != "" {
		slog.Warn("invalid ACS term", "condition", condition, "problem", problem)
		return false
	}
	result := evalTerm(t, ctx)
	slog.Debug("ACS term", "condition", condition, "result", result)
	return result
}

// evalTerm evaluates a term parseTerm read without a problem.
func evalTerm(t parsedTerm, ctx Context) bool {
	u, s := ctx.User, ctx.Session
	switch t.code {
	case "SYSOP":
		return u.AccessLevel >= ctx.SysOpLevel
	case "COSYSOP":
		return u.AccessLevel >= ctx.CoSysOpLevel
	case "L": // Local connection
		if s == nil {
			return false
		}
		network := s.RemoteAddr().Network()
		addr := s.RemoteAddr().String()
		isLoopback := strings.HasPrefix(addr, "127.") || strings.HasPrefix(addr, "[::1]")
		return network == "pipe" || network == "unix" || isLoopback
	case "A": // Session has a terminal: every telnet session, SSH with a PTY. Not an ANSI or encoding check.
		return s != nil && s.HasPTY()
	case "F": // Flag is set
		return strings.Contains(strings.ToUpper(u.Flags), strings.ToUpper(t.value))
	case "I": // User has flag I: the same test as FI; any argument is ignored
		return strings.Contains(strings.ToUpper(u.Flags), "I")
	case "V": // User is validated
		return u.Validated
	case "REALNAME": // The user's real name is set
		return strings.TrimSpace(u.RealName) != ""
	case "Z": // String exists in PrivateNote
		return strings.Contains(strings.ToUpper(u.PrivateNote), strings.ToUpper(t.value))
	case "Y": // Within Time Range hh:mm/hh:mm
		from, to, _ := strings.Cut(t.value, "/")
		start, _ := time.Parse("15:04", from) // parseTerm checked both
		end, _ := time.Parse("15:04", to)
		return InTimeRange(ctx.now(), start, end)
	}
	return t.compare(measure(t, ctx))
}

// InTimeRange reports whether now's clock time falls within start-end, both
// minutes included: 00:00/23:59 is the whole day, through 23:59:59. A range
// whose end is before its start runs past midnight. Only the hours and
// minutes of each time are used.
func InTimeRange(now, start, end time.Time) bool {
	m := now.Hour()*60 + now.Minute()
	from := start.Hour()*60 + start.Minute()
	to := end.Hour()*60 + end.Minute()
	if to < from {
		return m >= from || m <= to
	}
	return m >= from && m <= to
}
