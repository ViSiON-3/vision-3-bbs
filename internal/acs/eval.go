package acs

import (
	"log/slog"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/perm"
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
	// Session is the caller's connection, nil when there is none: a string
	// with L or A in it then fails as a whole, and T counts the call as just
	// started, with no chat credit.
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
	// in config.json). SysOpLevel is also the level that holds every
	// permission, for PERM.
	SysOpLevel, CoSysOpLevel int
	// Roles are the roles ROLE and PERM read; nil is the BBS's current set
	// (perm.Current). A ROLE term naming a role the set doesn't define fails
	// the whole string.
	Roles *perm.Roles
	// Scope finds the message area PERM asks a scoped permission
	// (messages.moderate) about, called only for such a term. nil is the
	// user's current message area by the tags on their record, with no
	// sponsor; the BBS looks the area up, sponsor included (see
	// menu.acsContext).
	Scope func() perm.Scope
}

// scope is the area PERM asks about.
func (c Context) scope() perm.Scope {
	if c.Scope != nil {
		return c.Scope()
	}
	return perm.Scope{Area: c.User.CurrentMessageAreaTag, Conference: c.User.CurrentMsgConferenceTag}
}

// permission decides PERM's permission p for the context's user, in their
// current message area when p is scoped.
func (c Context) permission(p perm.Permission) perm.Decision {
	var at perm.Scope
	if perm.IsScoped(p) {
		at = c.scope()
	}
	return perm.Check(c.User, p, at, perm.Levels{SysOp: c.SysOpLevel, CoSysOp: c.CoSysOpLevel}, c.roles())
}

// withRoles is ctx with its roles fixed for one evaluation: a reload of
// roles.json part way through can't make one term see the old roles and
// another the new.
func (c Context) withRoles() Context {
	c.Roles = c.roles()
	return c
}

func (c Context) roles() *perm.Roles {
	if c.Roles == nil {
		return perm.Current()
	}
	return c.Roles
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
// else fails for a nil User. A string Parse reports any problem for fails for
// every caller, and is logged once.
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
	if !e.Valid() {
		logInvalid(e)
		return false
	}
	result := e.Eval(ctx)
	slog.Debug("ACS check result", "acs", s, "result", result)
	return result
}

// maxLogged bounds how many invalid strings logInvalid remembers. Past it
// the set starts over, so a board fed endless distinct bad strings logs
// some again rather than growing without limit.
const maxLogged = 1024

// logged holds the invalid strings Check has logged since the last
// ResetInvalidLog.
var logged = struct {
	sync.Mutex
	seen map[string]bool
}{seen: map[string]bool{}}

// ResetInvalidLog forgets which invalid strings Check has logged, so each is
// logged again the next time a caller's check reads it. The BBS calls it
// when a config file is reloaded, so a string that is fixed and later comes
// back is reported again.
func ResetInvalidLog() {
	logged.Lock()
	logged.seen = map[string]bool{}
	logged.Unlock()
}

// logInvalid logs an invalid string's problems the first time Check sees
// it, so a string from a source the startup scan doesn't read still leaves a
// trace, without a line per evaluation.
func logInvalid(e *Expr) {
	logged.Lock()
	seen := logged.seen[e.Source]
	if !seen {
		if len(logged.seen) >= maxLogged {
			logged.seen = map[string]bool{}
		}
		logged.seen[e.Source] = true
	}
	logged.Unlock()
	if seen {
		return
	}
	msgs := make([]string, len(e.Diagnostics))
	for i, d := range e.Diagnostics {
		msgs[i] = d.String()
	}
	slog.Error("invalid ACS string denies every caller", "acs", e.Source, "error", strings.Join(msgs, "; "))
}

// Eval evaluates a valid expression against ctx; it is false for an
// expression that isn't Valid. ctx.User must not be nil. Every term is
// evaluated, as the BBS always has, and each logs its result at Debug. A term that
// needs context ctx lacks (see lacking) makes the whole expression false,
// whatever the other parts say: '!' can't turn it into a pass, and another
// branch of '|' can't pass in its place.
func (e *Expr) Eval(ctx Context) bool {
	if e.Root == nil {
		return false
	}
	result, lacks := evalNode(e.Root, ctx.withRoles())
	return result && !lacks
}

// evalNode evaluates n, and reports whether any term under it lacked its
// context.
func evalNode(n *Node, ctx Context) (result, lacks bool) {
	switch n.Kind {
	case TermNode:
		if _, missing := lacking(n.term, ctx); missing {
			slog.Debug("ACS term lacks its context, so the string fails", "condition", n.Text)
			return false, true
		}
		result = evalTerm(n.term, ctx)
		slog.Debug("ACS term", "condition", n.Text, "result", result)
		return result, false
	case NotNode:
		x, lx := evalNode(n.X, ctx)
		return !x, lx
	case AndNode:
		x, lx := evalNode(n.X, ctx)
		y, ly := evalNode(n.Y, ctx)
		return x && y, lx || ly
	default:
		x, lx := evalNode(n.X, ctx)
		y, ly := evalNode(n.Y, ctx)
		return x || y, lx || ly
	}
}

// lacking reports whether t needs context ctx doesn't have: a session for
// L and A (NoSession), a sign-up date on the user's record for AGE
// (Unavailable), or for ROLE a role the board defines (Undefined). The last
// is what keeps a misspelt role from passing through '!' or another branch
// of '|'.
func lacking(t parsedTerm, ctx Context) (Reason, bool) {
	switch {
	case (t.code == "L" || t.code == "A") && ctx.Session == nil:
		return NoSession, true
	case t.code == "AGE" && ctx.User.CreatedAt.IsZero():
		return Unavailable, true
	case t.code == "ROLE" && !ctx.roles().Defined(t.value):
		return Undefined, true
	}
	return Match, false
}

// EvalTerm evaluates a single term, such as S50, FA or LEVEL<20, against ctx,
// whose User must not be nil. A term with an unknown letter or a bad
// argument, or one that lacks its context, is false; the first is logged as
// a warning.
func EvalTerm(condition string, ctx Context) bool {
	if len(condition) == 0 {
		slog.Warn("empty condition encountered in ACS string after tokenization")
		return false
	}
	t, problem := parseTerm(condition)
	if problem != "" {
		slog.Warn("invalid ACS term", "condition", condition, "problem", problem)
		return false
	}
	ctx = ctx.withRoles()
	if _, missing := lacking(t, ctx); missing {
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
	case "L": // Local connection; lacking has checked there is a session
		network := s.RemoteAddr().Network()
		addr := s.RemoteAddr().String()
		isLoopback := strings.HasPrefix(addr, "127.") || strings.HasPrefix(addr, "[::1]")
		return network == "pipe" || network == "unix" || isLoopback
	case "A": // Session has a terminal: every telnet session, SSH with a PTY. Not an ANSI or encoding check.
		return s.HasPTY()
	case "F": // Flag is set
		return strings.Contains(strings.ToUpper(u.Flags), strings.ToUpper(t.value))
	case "I": // User has flag I: the same test as FI; any argument is ignored
		return strings.Contains(strings.ToUpper(u.Flags), "I")
	case "V": // User is validated
		return u.Validated
	case "ROLE": // The record lists the role; lacking has checked it is defined
		return perm.Member(u, t.value, ctx.roles())
	case "PERM": // The level holds it, or a sponsorship or a role grants it here
		return ctx.permission(perm.Permission(t.value)).Allowed
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
