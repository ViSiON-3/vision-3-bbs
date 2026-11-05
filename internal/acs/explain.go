package acs

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Reason is why a part of an expression passed or failed.
type Reason int

const (
	// Match is a term the user's values meet, or an operator whose result
	// was decided by ordinary passing and failing terms.
	Match Reason = iota
	// NoMatch is a term the user's values don't meet: an ordinary failure.
	NoMatch
	// Invalid is a term with an unknown letter or a bad argument, which is
	// false for everyone; for an operator, a result that rests on such a
	// term; for a whole explanation with no tree, a string that can't be read
	// as a whole.
	Invalid
	// NoSession is a term that reads the caller's session when the context
	// has none; for an operator, a result that rests on such a term.
	NoSession
)

func (r Reason) String() string {
	switch r {
	case Match:
		return "match"
	case NoMatch:
		return "no match"
	case Invalid:
		return "invalid"
	default:
		return "no session"
	}
}

// problem reports whether r is a reason other than an ordinary pass or fail.
func (r Reason) problem() bool { return r == Invalid || r == NoSession }

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
	// Diagnostics are Parse's, and Ignored the characters among them that
	// were dropped: they are not in the tree, but they change what it says.
	Diagnostics []Diagnostic
	Ignored     []Diagnostic
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
	x.Context = ctx
	if s == "" || s == "*" {
		x.Result, x.Reason = true, Match
		x.Detail = "an empty string or a lone * passes for everyone"
		return x
	}
	e := Parse(s)
	x.Diagnostics = e.Diagnostics
	_, x.Ignored = tokenize(s, e.compare)
	switch {
	case ctx.User == nil:
		x.Reason = NoMatch
		x.Detail = "there is no user: only an empty string or a lone * passes"
	case !e.Valid():
		x.Reason = Invalid
		x.Detail = "the string can't be read as a whole, so it fails for everyone and '!' can't make it pass"
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
		st.Result = !st.X.Result
		st.Reason = outcome(st.Result)
		if st.X.Reason.problem() {
			st.Reason = st.X.Reason
		}
		st.Detail = fmt.Sprintf("%s %s, so this %s", st.X.Text, passed(st.X.Result), passes(st.Result))
		switch st.X.Reason {
		case Invalid:
			st.Detail += "; that rests on an invalid term, and a stricter ACS check will deny it"
		case NoSession:
			st.Detail += "; that rests on a session the context doesn't have"
		}
	case AndNode, OrNode:
		st.X, st.Y = explainNode(s, n.X, ctx), explainNode(s, n.Y, ctx)
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

// combine works out an operator's reason and detail. A result rests on an
// invalid term or a missing session only when every part that decided it
// does: S10|Q passes on S10 alone, but S10&!Q passes only because of !Q.
func combine(kind Kind, result bool, x, y *Step) (Reason, string) {
	var deciding []*Step
	for _, c := range []*Step{x, y} {
		// For & a failing part decides a failure and both decide a pass; for
		// | a passing part decides a pass and both decide a failure.
		if c.Result == result || (kind == AndNode) == result {
			deciding = append(deciding, c)
		}
	}
	reason := outcome(result)
	clean := false
	for _, c := range deciding {
		if !c.Reason.problem() {
			clean = true
		}
	}
	// A pass on & and a failure on | need every part, so any problem
	// counts; otherwise one clean deciding part is enough.
	if !clean || (kind == AndNode) == result {
		for _, c := range deciding {
			reason = worse(reason, c.Reason)
		}
	}

	var detail string
	if kind == AndNode {
		detail = "both parts must pass: "
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
	} else {
		detail = "either part may pass: "
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
	}
	switch reason {
	case Invalid:
		detail += "; the result rests on an invalid term"
	case NoSession:
		detail += "; the result rests on a session the context doesn't have"
	}
	return reason, detail
}

// worse returns the more serious of a and b: Invalid, then NoSession, then
// a; ordinary reasons don't replace one another.
func worse(a, b Reason) Reason {
	switch {
	case a == Invalid || b == Invalid:
		return Invalid
	case a == NoSession || b == NoSession:
		return NoSession
	}
	return a
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

// explainTerm evaluates a term with EvalTerm and describes the values it
// compared, read with the same parseTerm and measure, so the description
// can't drift from the result. A term Parse found a problem with is false
// without being evaluated, as EvalTerm would make it (see termProblem).
func explainTerm(n *Node, ctx Context) (bool, Reason, string) {
	if n.termDiag != "" {
		return false, Invalid, n.termDiag + ", so it is always false"
	}
	result := EvalTerm(n.Text, ctx)
	reason := outcome(result)
	u := ctx.User
	t, _, _ := parseTerm(n.Text)
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
				reason = NoSession
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
		case ctx.Session == nil:
			reason = NoSession
			detail = "needs a session and there is none, so it is false"
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
