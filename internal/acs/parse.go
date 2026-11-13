// Package acs parses and evaluates Access Control Strings, the expressions
// such as s20&fC that decide who may use a menu, command, area or
// conference. docs/sysop/reference/acs.md describes the language.
//
// Parse turns a string into a tree with each node's position and a list of
// diagnostics; Check and Expr.Eval evaluate it against a Context. Explain
// evaluates it the same way part by part, with the reason for each result,
// and Render lays that out as text for the ACS tester in both user editors.
// The BBS, vision3 doctor and the editors all go through this package, so a
// string means the same thing everywhere.
//
// One policy covers every string. Anything Parse reports (a character it
// doesn't know, a term with an unknown letter or a bad argument, a string
// that can't be read as a whole) makes the string fail for every caller, and
// '!' can't make it pass. A term that needs a session the context doesn't
// have, or a value the user's record lacks, fails the whole evaluation in the
// same way, whatever the other parts say.
package acs

import (
	"fmt"
	"unicode"
)

// Diagnostic is one problem Parse found. Any diagnostic makes the string
// fail for every caller. Pos and End are rune offsets into the string, End
// exclusive; Column is Pos+1, for messages.
type Diagnostic struct {
	Pos, End int
	Message  string
}

// Column is the diagnostic's 1-based position in the string, in runes.
func (d Diagnostic) Column() int { return d.Pos + 1 }

// String formats d as "column N: message".
func (d Diagnostic) String() string {
	return fmt.Sprintf("column %d: %s", d.Column(), d.Message)
}

// Kind is the type of a Node.
type Kind int

const (
	// TermNode is a single term such as S20 or FA; Text holds it.
	TermNode Kind = iota
	// NotNode negates X.
	NotNode
	// AndNode is X & Y.
	AndNode
	// OrNode is X | Y.
	OrNode
)

// Node is one part of a parsed expression. Pos and End are rune offsets of
// the source text it covers, End exclusive; for an operator node Op is the
// offset of its operator character.
type Node struct {
	Kind     Kind
	Pos, End int
	Op       int
	Text     string // the term, for a TermNode
	X, Y     *Node  // operands: X for NotNode, X and Y for AndNode and OrNode
	// term is Text as parseTerm read it, kept so a term is parsed once
	// rather than on every evaluation.
	term parsedTerm
}

// UsesSession reports whether any term in the expression reads the
// caller's session (L or A), so it fails as a whole where there is none.
func (e *Expr) UsesSession() bool {
	return e.uses(func(code string) bool { return code == "L" || code == "A" })
}

// UsesAge reports whether any term in the expression is AGE, so it fails as
// a whole for a user with no sign-up date.
func (e *Expr) UsesAge() bool {
	return e.uses(func(code string) bool { return code == "AGE" })
}

// RoleNames lists the roles the expression's ROLE terms name, lower case,
// each once, in order. A role configs/roles.json doesn't define makes the
// whole string fail, so the startup scan and doctor check these.
func (e *Expr) RoleNames() []string { return e.names("ROLE") }

// GroupNames is RoleNames for GROUP terms.
func (e *Expr) GroupNames() []string { return e.names("GROUP") }

// names lists the values of the terms with code, each once, in order.
func (e *Expr) names(code string) []string {
	var names []string
	seen := map[string]bool{}
	var walk func(n *Node)
	walk = func(n *Node) {
		if n == nil {
			return
		}
		if n.Kind == TermNode && n.term.code == code && !seen[n.term.value] {
			seen[n.term.value] = true
			names = append(names, n.term.value)
		}
		walk(n.X)
		walk(n.Y)
	}
	walk(e.Root)
	return names
}

// uses reports whether any term's code satisfies match.
func (e *Expr) uses(match func(code string) bool) bool {
	var walk func(n *Node) bool
	walk = func(n *Node) bool {
		if n == nil {
			return false
		}
		if n.Kind == TermNode {
			return match(n.term.code)
		}
		return walk(n.X) || walk(n.Y)
	}
	return walk(e.Root)
}

// Expr is a parsed ACS string.
type Expr struct {
	// Source is the string as given.
	Source string
	// Root is the expression tree, nil when Parse found any problem.
	Root *Node
	// Diagnostics lists every problem found, in order of position.
	Diagnostics []Diagnostic
}

// Valid reports whether Parse found no problem, so the string can be
// evaluated. A string that isn't valid fails for every caller.
func (e *Expr) Valid() bool { return e.Root != nil }

type tokenKind int

const (
	tokTerm tokenKind = iota
	tokOp
	tokLParen
	tokRParen
)

type token struct {
	kind     tokenKind
	text     string
	pos, end int
}

// isTermRune reports whether r can be part of a term: letters, digits, '/',
// ':', and the '.' and '_' of permission, role and group names
// (PERM:users.validate, ROLE:msg_ops). The comparison characters (isCompareRune) join a term only
// between term characters, which is how a role name takes a '-'.
func isTermRune(r rune) bool {
	return (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '/' || r == ':' || r == '.' || r == '_'
}

// tokenize splits s into terms, operators and parentheses. Whitespace (a
// space, a tab, a non-breaking space) separates tokens. The comparison and range characters join a term when a term
// character follows them (S<20, H8-17, LEVEL>=20). Any other character it
// doesn't know is reported, and ends the term it is in.
func tokenize(s string) ([]token, []Diagnostic) {
	var toks []token
	var diags []Diagnostic
	runes := []rune(s)
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		switch {
		case r == '(':
			toks = append(toks, token{kind: tokLParen, text: "(", pos: i, end: i + 1})
		case r == ')':
			toks = append(toks, token{kind: tokRParen, text: ")", pos: i, end: i + 1})
		case r == '&' || r == '|' || r == '!':
			toks = append(toks, token{kind: tokOp, text: string(r), pos: i, end: i + 1})
		case unicode.IsSpace(r):
		case isTermRune(r):
			start := i
			for i+1 < len(runes) {
				if isTermRune(runes[i+1]) {
					i++
					continue
				}
				// Comparison characters followed by a term character join
				// the term; elsewhere they are unexpected (S20-).
				j := i + 1
				for j < len(runes) && isCompareRune(runes[j]) {
					j++
				}
				if j == i+1 || j == len(runes) || !isTermRune(runes[j]) {
					break
				}
				i = j
			}
			toks = append(toks, token{kind: tokTerm, text: string(runes[start : i+1]), pos: start, end: i + 1})
		default:
			diags = append(diags, Diagnostic{Pos: i, End: i + 1,
				Message: fmt.Sprintf("unexpected character %q", r)})
		}
	}
	return toks, diags
}

// precedence orders the operators: ! before & before |.
var precedence = map[string]int{"!": 3, "&": 2, "|": 1}

// Parse reads an ACS string into an expression tree and reports every
// problem it finds. It never fails: a string with any problem has a nil Root
// and its Diagnostics say what and where. Parse doesn't treat "" and "*"
// specially; Check does (they pass), and Validate reports nothing for them.
//
// The tree is built as the evaluator has always read ACS: operators by
// precedence with a shunting-yard pass, then the postfix form folded into
// nodes. Besides an unknown character and a term that can't be evaluated,
// the problems are the ones that made the old evaluator give up: unbalanced
// parentheses, an operator without enough operands (a trailing &, or !!),
// and terms with no operator between them.
func Parse(s string) *Expr {
	e := &Expr{Source: s}
	toks, diags := tokenize(s)
	for _, t := range toks {
		if t.kind == tokTerm {
			if _, p := parseTerm(t.text); p != "" {
				diags = append(diags, Diagnostic{Pos: t.pos, End: t.end, Message: p})
			}
		}
	}
	e.Diagnostics = diags
	fail := func(pos, end int, msg string) *Expr {
		e.Root = nil
		e.Diagnostics = append(e.Diagnostics, Diagnostic{Pos: pos, End: end, Message: msg})
		sortDiagnostics(e.Diagnostics)
		return e
	}

	// Shunting-yard to postfix. ! is treated like the binary operators, as it
	// always has been: an earlier ! on the stack is popped before a later one,
	// which is why !!x leaves the first ! without an operand.
	var out, ops []token
	for _, t := range toks {
		switch t.kind {
		case tokTerm:
			out = append(out, t)
		case tokOp:
			for len(ops) > 0 && ops[len(ops)-1].kind == tokOp && precedence[ops[len(ops)-1].text] >= precedence[t.text] {
				out = append(out, ops[len(ops)-1])
				ops = ops[:len(ops)-1]
			}
			ops = append(ops, t)
		case tokLParen:
			ops = append(ops, t)
		case tokRParen:
			matched := false
			for len(ops) > 0 {
				op := ops[len(ops)-1]
				ops = ops[:len(ops)-1]
				if op.kind == tokLParen {
					matched = true
					break
				}
				out = append(out, op)
			}
			if !matched {
				return fail(t.pos, t.end, "')' has no matching '('")
			}
		}
	}
	for len(ops) > 0 {
		op := ops[len(ops)-1]
		ops = ops[:len(ops)-1]
		if op.kind == tokLParen {
			return fail(op.pos, op.end, "'(' is never closed")
		}
		out = append(out, op)
	}

	// Fold the postfix form into a tree.
	var stack []*Node
	for _, t := range out {
		switch {
		case t.kind == tokTerm:
			term, _ := parseTerm(t.text) // a term with a problem has failed Parse already
			stack = append(stack, &Node{Kind: TermNode, Pos: t.pos, End: t.end, Op: t.pos, Text: t.text, term: term})
		case t.text == "!":
			if len(stack) < 1 {
				return fail(t.pos, t.end, "'!' has nothing after it to negate (two '!' in a row are an error)")
			}
			x := stack[len(stack)-1]
			stack[len(stack)-1] = &Node{Kind: NotNode, Pos: min(t.pos, x.Pos), End: max(t.end, x.End), Op: t.pos, X: x}
		default:
			if len(stack) < 2 {
				return fail(t.pos, t.end, fmt.Sprintf("'%s' needs a term on each side", t.text))
			}
			x, y := stack[len(stack)-2], stack[len(stack)-1]
			kind := AndNode
			if t.text == "|" {
				kind = OrNode
			}
			stack = stack[:len(stack)-2]
			stack = append(stack, &Node{Kind: kind, Pos: min(x.Pos, t.pos), End: max(y.End, t.end), Op: t.pos, X: x, Y: y})
		}
	}
	switch len(stack) {
	case 0:
		return fail(0, len([]rune(s)), "there is no term to check")
	case 1:
		e.Root = stack[0]
	default:
		// Point at the first term that directly follows another term or a
		// closing parenthesis. Failing that (F(A)), the roots are in source
		// order and the second one starts where an operator is missing.
		for i := 1; i < len(toks); i++ {
			if toks[i].kind == tokTerm && (toks[i-1].kind == tokTerm || toks[i-1].kind == tokRParen) {
				t := toks[i]
				return fail(t.pos, t.end, fmt.Sprintf("no operator before %q: put & or | between terms", t.text))
			}
		}
		extra := stack[1]
		return fail(extra.Pos, extra.End, fmt.Sprintf("no operator before %q: put & or | between terms", textOf(s, extra)))
	}

	if len(e.Diagnostics) > 0 {
		e.Root = nil
		sortDiagnostics(e.Diagnostics)
	}
	return e
}

// textOf is the source text n covers.
func textOf(s string, n *Node) string {
	r := []rune(s)
	if n.Pos < 0 || n.End > len(r) || n.Pos >= n.End {
		return ""
	}
	return string(r[n.Pos:n.End])
}

func sortDiagnostics(d []Diagnostic) {
	// Insertion sort: diagnostics are few and mostly in order already.
	for i := 1; i < len(d); i++ {
		for j := i; j > 0 && d[j].Pos < d[j-1].Pos; j-- {
			d[j], d[j-1] = d[j-1], d[j]
		}
	}
}

// FieldError is what an editor shows for an ACS string a sysop typed: nil
// when it reads cleanly, or is "" or "*", otherwise its first problem, such
// as `ACS column 2: "Q": Q is not an ACS term`. Editors check every ACS
// field with it, since a string with any problem denies every caller.
func FieldError(s string) error {
	if d := Validate(s); len(d) > 0 {
		return fmt.Errorf("ACS %s", d[0])
	}
	return nil
}

// Validate returns the diagnostics for an ACS string as configured: nothing
// for "" or "*", which pass every caller, otherwise Parse's.
func Validate(s string) []Diagnostic {
	if s == "" || s == "*" {
		return nil
	}
	return Parse(s).Diagnostics
}
