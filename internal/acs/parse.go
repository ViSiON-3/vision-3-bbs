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
// Evaluation keeps the behaviour ACS strings have always had, including the
// lenient parts the diagnostics report: a character the parser doesn't know
// is dropped and ends the term it is in, and a term with an unknown letter or
// a bad argument is false, so negating it passes.
package acs

import (
	"fmt"
	"strings"
)

// Severity is how much a Diagnostic matters.
type Severity int

const (
	// Warning marks something the string does today that a stricter ACS
	// policy would change: a dropped character, or a term that is always
	// false because its letter is unknown or its argument is bad.
	Warning Severity = iota
	// Error marks a string that can't be read as a whole. It fails for every
	// caller, and ! can't make it pass.
	Error
)

func (s Severity) String() string {
	if s == Error {
		return "error"
	}
	return "warning"
}

// Diagnostic is one problem Parse found. Pos and End are rune offsets into
// the string, End exclusive; Column is Pos+1, for messages.
type Diagnostic struct {
	Severity Severity
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
	termDiag string // why the term is always false, "" for a valid term
}

// Expr is a parsed ACS string.
type Expr struct {
	// Source is the string as given.
	Source string
	// Root is the expression tree, nil when the string can't be read as a
	// whole (Diagnostics then holds an Error).
	Root *Node
	// Diagnostics lists every problem found, in order of position.
	Diagnostics []Diagnostic
}

// Valid reports whether the string can be read as a whole. A valid string
// may still have warnings.
func (e *Expr) Valid() bool { return e.Root != nil }

// HasErrors reports whether any diagnostic is an Error.
func (e *Expr) HasErrors() bool {
	for _, d := range e.Diagnostics {
		if d.Severity == Error {
			return true
		}
	}
	return false
}

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

// isTermRune reports whether r can be part of a term: letters, digits, '/'
// and ':'.
func isTermRune(r rune) bool {
	return (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '/' || r == ':'
}

// tokenize splits s into terms, operators and parentheses. Spaces separate
// tokens; any other character it doesn't know is dropped, with a warning, and
// ends the term it is in.
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
		case r == ' ':
		case isTermRune(r):
			start := i
			for i+1 < len(runes) && isTermRune(runes[i+1]) {
				i++
			}
			toks = append(toks, token{kind: tokTerm, text: string(runes[start : i+1]), pos: start, end: i + 1})
		default:
			diags = append(diags, Diagnostic{Severity: Warning, Pos: i, End: i + 1,
				Message: fmt.Sprintf("unexpected character %q is ignored and ends the term before it", r)})
		}
	}
	return toks, diags
}

// precedence orders the operators: ! before & before |.
var precedence = map[string]int{"!": 3, "&": 2, "|": 1}

// Parse reads an ACS string into an expression tree and reports what it
// finds. It never fails: a string that can't be read as a whole has a nil
// Root and an Error diagnostic. Parse doesn't treat "" and "*" specially;
// Check does (they pass), and Validate reports nothing for them.
//
// The tree is built exactly as the evaluator has always read ACS: operators
// by precedence with a shunting-yard pass, then the postfix form folded into
// nodes. The checks that fail a string as a whole are the ones that made the
// old evaluator give up: unbalanced parentheses, an operator without enough
// operands (a trailing &, or !!), and terms with no operator between them.
func Parse(s string) *Expr {
	e := &Expr{Source: s}
	toks, diags := tokenize(s)
	e.Diagnostics = diags

	// Shunting-yard to postfix. ! is treated like the binary operators, as it
	// always has been: an earlier ! on the stack is popped before a later one,
	// which is why !!x leaves the first ! without an operand.
	var out, ops []token
	fail := func(pos, end int, msg string) *Expr {
		e.Root = nil
		e.Diagnostics = append(e.Diagnostics, Diagnostic{Severity: Error, Pos: pos, End: end, Message: msg})
		// Terms that would be false on their own are worth fixing too.
		for _, t := range toks {
			if t.kind == tokTerm {
				if p := termProblem(t.text); p != "" {
					e.Diagnostics = append(e.Diagnostics, Diagnostic{Severity: Warning, Pos: t.pos, End: t.end, Message: p})
				}
			}
		}
		sortDiagnostics(e.Diagnostics)
		return e
	}
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
			n := &Node{Kind: TermNode, Pos: t.pos, End: t.end, Op: t.pos, Text: t.text}
			n.termDiag = termProblem(t.text)
			stack = append(stack, n)
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

	e.Diagnostics = append(e.Diagnostics, termDiagnostics(e.Root, false)...)
	sortDiagnostics(e.Diagnostics)
	return e
}

// termDiagnostics reports each term under n that is always false, noting
// when it is negated (and so true for everyone).
func termDiagnostics(n *Node, negated bool) []Diagnostic {
	if n == nil {
		return nil
	}
	switch n.Kind {
	case TermNode:
		if n.termDiag == "" {
			return nil
		}
		msg := n.termDiag + ", so it is always false"
		if negated {
			msg = n.termDiag + "; it is false, and the '!' before it makes that part pass for everyone"
		}
		return []Diagnostic{{Severity: Warning, Pos: n.Pos, End: n.End, Message: msg}}
	case NotNode:
		return termDiagnostics(n.X, !negated)
	default:
		return append(termDiagnostics(n.X, negated), termDiagnostics(n.Y, negated)...)
	}
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

// Validate returns the diagnostics for an ACS string as configured: nothing
// for "" or "*", which pass every caller, otherwise Parse's.
func Validate(s string) []Diagnostic {
	if s == "" || s == "*" {
		return nil
	}
	return Parse(s).Diagnostics
}

// splitTerm returns a term's letter (upper case) and its argument, or the
// whole term upper-cased as the keyword for SYSOP and COSYSOP.
func splitTerm(term string) (code, value string, keyword bool) {
	upper := strings.ToUpper(term)
	if upper == "SYSOP" || upper == "COSYSOP" {
		return upper, "", true
	}
	return strings.ToUpper(term[0:1]), term[1:], false
}
