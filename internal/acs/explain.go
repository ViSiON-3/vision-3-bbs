package acs

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/perm"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// Reason is why a part of an expression passed or failed.
type Reason int

const (
	// Match is a term the user's values meet, or an operator whose result
	// was decided by ordinary passing and failing terms.
	Match Reason = iota
	// NoMatch is a term the user's values don't meet: an ordinary failure.
	NoMatch
	// Invalid is a string Parse found a problem with, which fails for
	// everyone. Only a whole explanation, which then has no tree, has it.
	Invalid
	// NoSession is a term that reads the caller's session when the context
	// has none; for an operator, one with such a term under it. Either fails,
	// and so does the whole string.
	NoSession
	// Unavailable is a term that needs a value the user's record doesn't
	// have (AGE without a sign-up date); for an operator, one with such a
	// term under it. Either fails, and so does the whole string.
	Unavailable
	// Undefined is a ROLE term naming a role configs/roles.json doesn't
	// define; for an operator, one with such a term under it. Either fails,
	// and so does the whole string.
	Undefined
)

func (r Reason) String() string {
	switch r {
	case Match:
		return "match"
	case NoMatch:
		return "no match"
	case Invalid:
		return "invalid"
	case NoSession:
		return "no session"
	case Undefined:
		return "undefined"
	default:
		return "unavailable"
	}
}

// problem reports whether r is a reason other than an ordinary pass or fail.
func (r Reason) problem() bool {
	return r == Invalid || r == NoSession || r == Unavailable || r == Undefined
}

// Step is one part of an explained expression: a node of the parsed tree
// with what it evaluated to and why.
type Step struct {
	Kind     Kind
	Pos, End int    // rune offsets of the source text the part covers
	Op       int    // offset of the operator character, for an operator
	Text     string // the source text the part covers
	Result   bool
	Reason   Reason
	// Detail says how the result came about: the user's value against the
	// term's ("level 20 is below 50"), or which parts decided an operator.
	Detail string
	X, Y   *Step // operands, as in Node
}

// Explanation is an expression evaluated for a user, part by part. Its
// Result is always what Check returns for the same string and context.
type Explanation struct {
	Source string
	Result bool
	// Reason is the root step's, or Invalid when the string can't be read.
	Reason Reason
	// Detail explains a result that has no tree: an empty string or "*", no
	// user, or a string that can't be read as a whole.
	Detail string
	// Root is the evaluated tree, nil when Detail explains the result.
	Root *Step
	// Diagnostics are Parse's: any makes the string fail, with no tree.
	Diagnostics []Diagnostic
	// Context is the context evaluated against, with Now filled in.
	Context Context
	// CurrentTime is set when Now was the current time rather than a time
	// given in the context.
	CurrentTime bool
}

// Explain evaluates s against ctx exactly as Check does and returns each
// part's result with the reason for it, for the ACS tester. Unlike Check it
// logs no warnings or errors, since a sysop trying strings out isn't a
// caller hitting one, and it reads the clock once: a zero ctx.Now becomes
// the current time for every term.
//
// A passing Explanation means this one expression passes for this user in
// this context. It says nothing about the other checks a resource has
// (a conference's ACS around an area's, private mail rules and so on).
func Explain(s string, ctx Context) Explanation {
	x := Explanation{Source: s, CurrentTime: ctx.Now.IsZero()}
	if x.CurrentTime {
		ctx.Now = time.Now()
	}
	ctx = ctx.withRoles()
	x.Context = ctx
	if s == "" || s == "*" {
		x.Result, x.Reason = true, Match
		x.Detail = "an empty string or a lone * passes for everyone"
		return x
	}
	e := Parse(s)
	x.Diagnostics = e.Diagnostics
	switch {
	case ctx.User == nil:
		x.Reason = NoMatch
		x.Detail = "there is no user: only an empty string or a lone * passes"
	case !e.Valid():
		x.Reason = Invalid
		x.Detail = "the string has a problem, so it fails for everyone and '!' can't make it pass"
	default:
		x.Root = explainNode(s, e.Root, ctx)
		x.Result, x.Reason = x.Root.Result, x.Root.Reason
	}
	return x
}

// explainNode evaluates n as evalNode does, every term included.
func explainNode(s string, n *Node, ctx Context) *Step {
	st := &Step{Kind: n.Kind, Pos: n.Pos, End: n.End, Op: n.Op, Text: partText(s, n)}
	switch n.Kind {
	case TermNode:
		st.Result, st.Reason, st.Detail = explainTerm(n, ctx)
	case NotNode:
		st.X = explainNode(s, n.X, ctx)
		if st.X.Reason.problem() {
			st.Reason = st.X.Reason
			st.Detail = st.X.Text + " can't be checked here, so this fails too"
			break
		}
		st.Result = !st.X.Result
		st.Reason = outcome(st.Result)
		st.Detail = fmt.Sprintf("%s %s, so this %s", st.X.Text, passed(st.X.Result), passes(st.Result))
	case AndNode, OrNode:
		st.X, st.Y = explainNode(s, n.X, ctx), explainNode(s, n.Y, ctx)
		if c := lackingPart(st.X, st.Y); c != nil {
			st.Reason = c.Reason
			st.Detail = c.Text + " can't be checked here, so this fails whatever the other part says"
			break
		}
		if n.Kind == AndNode {
			st.Result = st.X.Result && st.Y.Result
		} else {
			st.Result = st.X.Result || st.Y.Result
		}
		st.Reason, st.Detail = combine(n.Kind, st.Result, st.X, st.Y)
	}
	return st
}

// partText is the source text n covers, widened to take in the parentheses
// of the groups it opens or closes: a span runs from a node's first term to
// its last, so in (FA|FB)&V the & node's span reads "FA|FB)&V".
func partText(s string, n *Node) string {
	r := []rune(s)
	pos, end := n.Pos, n.End
	open, closed := 0, 0
	for _, c := range r[pos:end] {
		switch {
		case c == '(':
			open++
		case c == ')' && open > 0:
			open--
		case c == ')':
			closed++
		}
	}
	for ; open > 0 && end < len(r) && (r[end] == ')' || r[end] == ' '); end++ {
		if r[end] == ')' {
			open--
		}
	}
	for ; closed > 0 && pos > 0 && (r[pos-1] == '(' || r[pos-1] == ' '); pos-- {
		if r[pos-1] == '(' {
			closed--
		}
	}
	return strings.TrimSpace(string(r[pos:end]))
}

// lackingPart is the first of x and y that lacks its context, nil if
// neither does.
func lackingPart(x, y *Step) *Step {
	for _, c := range []*Step{x, y} {
		if c.Reason.problem() {
			return c
		}
	}
	return nil
}

// combine works out an operator's reason and detail when neither part lacks
// its context.
func combine(kind Kind, result bool, x, y *Step) (Reason, string) {
	if kind == AndNode {
		detail := "both parts must pass: "
		switch {
		case result:
			detail += "both passed"
		case !x.Result && !y.Result:
			detail += "both failed"
		case !x.Result:
			detail += x.Text + " failed"
		default:
			detail += y.Text + " failed"
		}
		return outcome(result), detail
	}
	detail := "either part may pass: "
	switch {
	case !result:
		detail += "neither passed"
	case x.Result && y.Result:
		detail += "both passed"
	case x.Result:
		detail += x.Text + " passed"
	default:
		detail += y.Text + " passed"
	}
	return outcome(result), detail
}

func outcome(result bool) Reason {
	if result {
		return Match
	}
	return NoMatch
}

func passed(ok bool) string {
	if ok {
		return "passed"
	}
	return "failed"
}

func passes(ok bool) string {
	if ok {
		return "passes"
	}
	return "fails"
}

// explainTerm evaluates a term as Eval does and describes the values it
// compared, read with the same parsed term and measure, so the description
// can't drift from the result. A term that lacks its context fails without
// being evaluated, as in Eval.
func explainTerm(n *Node, ctx Context) (bool, Reason, string) {
	t := n.term
	if reason, missing := lacking(t, ctx); missing {
		switch reason {
		case NoSession:
			return false, reason, "needs a session and there is none, so the whole string fails"
		case Undefined:
			return false, reason, undefinedRole(t.value, ctx.roles())
		}
		return false, reason, "the record has no sign-up date, so the account has no age and the whole string fails"
	}
	result := evalTerm(t, ctx)
	reason := outcome(result)
	u := ctx.User
	bare := t.op == "" && !t.ranged
	hasFlag := func(flag string) string {
		flags := u.Flags
		if flags == "" {
			flags = "none"
		}
		if result {
			return fmt.Sprintf("has flag %s (flags: %s)", strings.ToUpper(flag), flags)
		}
		return fmt.Sprintf("doesn't have flag %s (flags: %s)", strings.ToUpper(flag), flags)
	}
	now := ctx.now()
	var detail string
	switch t.code {
	case "SYSOP":
		detail = compared("level", u.AccessLevel, "", parsedTerm{code: "S", op: ">=", lo: ctx.SysOpLevel}) + " (sysOpLevel)"
	case "COSYSOP":
		detail = compared("level", u.AccessLevel, "", parsedTerm{code: "S", op: ">=", lo: ctx.CoSysOpLevel}) + " (coSysOpLevel)"
	case "S", "D":
		detail = compared("level", u.AccessLevel, "", t)
	case "F":
		detail = hasFlag(t.value)
	case "I":
		detail = hasFlag("I")
	case "V":
		if result {
			detail = "validated"
		} else {
			detail = "not validated"
		}
	case "ROLE":
		detail = roleDetail(u, t.value, result)
	case "PERM":
		detail = permDetail(perm.Check(u, perm.Permission(t.value), ctx.SysOpLevel, ctx.roles()), u, ctx.SysOpLevel)
	case "U":
		detail = compared("user ID", u.ID, "", t)
	case "P":
		detail = compared("file points", u.FilePoints, "", t)
	case "E":
		m := measure(t, ctx)
		if m.none {
			detail = "no calls yet, so it fails"
			if bare {
				detail = "no calls yet, so E fails"
			}
		} else {
			detail = fmt.Sprintf("%d uploads in %d calls: ", u.NumUploads, u.TimesCalled) + compared("", m.have, "%", t)
		}
	case "B":
		m := measure(t, ctx)
		switch {
		case m.unlimited && bare:
			detail = "no downloads yet, so B passes"
		case m.unlimited:
			detail = "no downloads yet, which is above every number, so it " + passes(result)
		default:
			detail = fmt.Sprintf("%d uploads per %d downloads: ", u.NumUploads, u.NumDownloads) + compared("", m.have, "%", t)
		}
	case "T":
		m := measure(t, ctx)
		var why string
		switch {
		case u.TimeLimit <= 0:
			why = "no time limit"
		case u.AccessLevel >= ctx.CoSysOpLevel:
			why = "co-sysop level and up have no time limit"
		}
		switch {
		case why != "" && bare:
			detail = why + ", so T passes"
		case why != "":
			detail = why + ", which is above every number, so it " + passes(result)
		default:
			detail = compared("minutes left", m.have, "", t) + fmt.Sprintf(" (limit %d)", u.TimeLimit)
			if ctx.Session == nil {
				detail = "no session, so the call is taken to have just started: " + detail
			}
		}
	case "Z":
		if t.value == "" {
			detail = "Z with no text passes for everyone"
		} else if result {
			detail = fmt.Sprintf("the private note contains %q", t.value)
		} else {
			detail = fmt.Sprintf("the private note doesn't contain %q", t.value)
		}
	case "CALLS":
		detail = compared("calls", u.TimesCalled, "", t)
	case "POSTS":
		detail = compared("posts", u.MessagesPosted, "", t)
	case "UPLOADS":
		detail = compared("uploads", u.NumUploads, "", t)
	case "DOWNLOADS":
		detail = compared("downloads", u.NumDownloads, "", t)
	case "REALNAME":
		if result {
			detail = "the real name is set"
		} else {
			detail = "no real name is set"
		}
	case "AGE":
		detail = compared("account age", measure(t, ctx).have, "", t) +
			fmt.Sprintf(" (days since sign-up on %s, %s)", u.CreatedAt.In(ctx.location()).Format("2006-01-02"), ctx.location())
	case "C":
		detail = compared("the message conference", u.CurrentMsgConferenceID, "", t)
	case "X":
		detail = compared("the file conference", u.CurrentFileConferenceID, "", t)
	case "H":
		detail = compared("the hour", now.Hour(), "", t)
	case "W":
		day := now.Weekday()
		switch {
		case (bare || (t.op == "=" && !t.ranged)) && int(day) == t.lo:
			detail = fmt.Sprintf("today is %d (%s)", t.lo, day)
		case bare || (t.op == "=" && !t.ranged):
			detail = fmt.Sprintf("today is %d (%s), not %d (%s)", int(day), day, t.lo, time.Weekday(t.lo))
		default:
			detail = fmt.Sprintf("today is %d (%s): ", int(day), day) + compared("", int(day), "", t)
		}
	case "Y":
		from, to, _ := strings.Cut(t.value, "/")
		in := "outside"
		if result {
			in = "inside"
		}
		detail = fmt.Sprintf("%s is %s %s-%s", now.Format("15:04"), in, from, to)
	case "L", "A":
		switch {
		case t.code == "L" && result:
			detail = fmt.Sprintf("the connection is local (%s)", ctx.Session.RemoteAddr())
		case t.code == "L":
			detail = fmt.Sprintf("the connection is not local (%s)", ctx.Session.RemoteAddr())
		case result:
			detail = "the connection has a terminal"
		default:
			detail = "the connection has no terminal"
		}
	}
	return result, reason, detail
}

// undefinedRole explains a ROLE term whose role isn't defined.
func undefinedRole(name string, roles *perm.Roles) string {
	defined := "it defines none"
	if roles.Len() > 0 {
		defined = "defined: " + strings.Join(roles.Names(), ", ")
	}
	return fmt.Sprintf("configs/roles.json has no role named %s (%s), so the whole string fails", name, defined)
}

// userRoles is a record's roles for an explanation: "validator, msgops" or
// "none".
func userRoles(u *user.User) string {
	if r := perm.FormatRoleList(u.Roles); r != "" {
		return r
	}
	return "none"
}

// roleDetail explains a ROLE term for a defined role.
func roleDetail(u *user.User, name string, member bool) string {
	if member {
		return fmt.Sprintf("has role %s (roles: %s)", name, userRoles(u))
	}
	return fmt.Sprintf("doesn't have role %s (roles: %s); a sysop's level doesn't make them a member", name, userRoles(u))
}

// permDetail explains a PERM term: where the permission came from, or why
// nothing grants it.
func permDetail(d perm.Decision, u *user.User, sysOpLevel int) string {
	switch {
	case d.Sysop:
		return fmt.Sprintf("has %s: level %d is at least %d (sysOpLevel), which holds every permission", d.Permission, u.AccessLevel, sysOpLevel)
	case d.Allowed:
		return fmt.Sprintf("has %s through %s", d.Permission, d.Source())
	case u.DeletedUser:
		return fmt.Sprintf("doesn't have %s: the account is deleted", d.Permission)
	}
	return fmt.Sprintf("doesn't have %s: level %d is below %d (sysOpLevel) and no role grants it (roles: %s)", d.Permission, u.AccessLevel, sysOpLevel, userRoles(u))
}

// compared describes a numeric term's comparison of have, the caller's value
// (with unit after it, such as "%"): "level 20 is below 50", "user ID is 5,
// not 3", "the hour is 23, inside 22-6, which runs past the top". what is
// the value's name, or "" when the caller has said it already.
func compared(what string, have int, unit string, t parsedTerm) string {
	v := func(n int) string { return strconv.Itoa(n) + unit }
	subject := v(have)
	if what != "" {
		subject = what + " " + subject
	}
	ok := t.compare(measured{have: have})
	if t.ranged {
		if t.lo > t.hi {
			in := "outside"
			if ok {
				in = "inside"
			}
			return fmt.Sprintf("%s is %s %s-%s, which runs past the top", subject, in, v(t.lo), v(t.hi))
		}
		if ok {
			return fmt.Sprintf("%s is between %s and %s", subject, v(t.lo), v(t.hi))
		}
		return fmt.Sprintf("%s is not between %s and %s", subject, v(t.lo), v(t.hi))
	}
	op := t.op
	if op == "" {
		op = numericCodes[t.code]
	}
	want := v(t.lo)
	switch op {
	case "=":
		if what == "" {
			if ok {
				return fmt.Sprintf("%s is %s", subject, want)
			}
			return fmt.Sprintf("%s is not %s", subject, want)
		}
		if ok {
			return fmt.Sprintf("%s is %s", what, v(have))
		}
		return fmt.Sprintf("%s is %s, not %s", what, v(have), want)
	case ">=":
		if ok {
			return fmt.Sprintf("%s is at least %s", subject, want)
		}
		return fmt.Sprintf("%s is below %s", subject, want)
	case ">":
		if ok {
			return fmt.Sprintf("%s is above %s", subject, want)
		}
		return fmt.Sprintf("%s is not above %s", subject, want)
	case "<":
		if ok {
			return fmt.Sprintf("%s is below %s", subject, want)
		}
		return fmt.Sprintf("%s is not below %s", subject, want)
	}
	if ok { // <=
		return fmt.Sprintf("%s is at most %s", subject, want)
	}
	return fmt.Sprintf("%s is above %s", subject, want)
}
