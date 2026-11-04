// Package msgsearch parses and matches the text queries of the SEARCHMSGS
// message search.
//
// A query is a list of words and "quoted phrases" combined with AND, OR and
// NOT and grouped with parentheses:
//
//	phreak
//	blue box             (both words: adjacent terms are ANDed)
//	"blue box"           (the exact phrase)
//	bell AND (tsps OR "operator services") AND NOT 2600
//
// NOT binds tightest, then AND, then OR; operators of equal precedence group
// left to right, so "a OR b c" is "a OR (b AND c)". The operator words are
// recognised in any case; quote one ("and") to search for the word itself.
//
// Matching is case-insensitive and by substring: "box" matches "boxes" and
// "Blue Box". Runs of whitespace (spaces, tabs, line breaks) count as one
// space on both sides, so a phrase matches across a line break in a body.
// ANSI escape sequences in the searched text are ignored.
package msgsearch

import (
	"fmt"
	"strings"
	"unicode"
)

// MaxQueryLen is the longest query, in runes, that Parse accepts. The menu
// prompt allows no more.
const MaxQueryLen = 80

// Error is a query that could not be parsed. Pos is the 1-based rune
// position in the query where the problem was found, for showing the caller
// where to look; it is 0 when the problem is the query as a whole (empty).
type Error struct {
	Pos int
	Msg string
}

// Error implements error: the message with its position, e.g.
// "unmatched ( at 5".
func (e *Error) Error() string {
	if e.Pos <= 0 {
		return e.Msg
	}
	return fmt.Sprintf("%s at %d", e.Msg, e.Pos)
}

// Query is a parsed query. It is immutable and safe for concurrent use.
type Query struct {
	root node
	src  string
}

// String returns the query as it was typed, trimmed.
func (q *Query) String() string { return q.src }

// Match reports whether the query matches the texts, which are searched
// separately: a term matches when it occurs in any one of them, and a
// phrase never spans two (a message's subject and body, say).
func (q *Query) Match(texts ...string) bool {
	norm := make([]string, len(texts))
	for i, t := range texts {
		norm[i] = Normalize(t)
	}
	return q.root.match(norm)
}

// MatchNormalized is Match for texts already passed through Normalize, for
// callers that test one text against several queries.
func (q *Query) MatchNormalized(texts ...string) bool {
	return q.root.match(texts)
}

// Normalize returns text as Match compares it: lower case, ANSI escape
// sequences removed, and every run of whitespace replaced by one space.
func Normalize(text string) string {
	var b strings.Builder
	b.Grow(len(text))
	space := false
	rs := []rune(text)
	for i := 0; i < len(rs); i++ {
		r := rs[i]
		if r == 0x1b {
			// ESC [ params final: skip the whole CSI sequence; a lone ESC
			// or another escape is dropped on its own
			if i+1 < len(rs) && rs[i+1] == '[' {
				i += 2
				for i < len(rs) && (rs[i] < 0x40 || rs[i] > 0x7e) {
					i++
				}
			}
			continue
		}
		if unicode.IsSpace(r) {
			space = b.Len() > 0
			continue
		}
		if space {
			b.WriteByte(' ')
			space = false
		}
		b.WriteRune(unicode.ToLower(r))
	}
	return b.String()
}

// node is one part of a parsed query.
type node interface {
	match(texts []string) bool
}

type termNode struct{ text string } // a word or phrase, normalized
type notNode struct{ x node }
type andNode struct{ l, r node }
type orNode struct{ l, r node }

func (n termNode) match(texts []string) bool {
	for _, t := range texts {
		if strings.Contains(t, n.text) {
			return true
		}
	}
	return false
}
func (n notNode) match(texts []string) bool { return !n.x.match(texts) }
func (n andNode) match(texts []string) bool { return n.l.match(texts) && n.r.match(texts) }
func (n orNode) match(texts []string) bool  { return n.l.match(texts) || n.r.match(texts) }

// token kinds
type kind int

const (
	kTerm kind = iota
	kAnd
	kOr
	kNot
	kOpen
	kClose
	kEnd
)

type token struct {
	kind kind
	text string // the term, for kTerm
	pos  int    // 1-based rune position
}

// Parse parses a query. It returns an *Error for an empty query, one longer
// than MaxQueryLen, an unterminated quote, an empty phrase or group, an
// unmatched parenthesis, or an operator with nothing on one side.
func Parse(query string) (*Query, error) {
	src := strings.TrimSpace(query)
	if src == "" {
		return nil, &Error{Msg: "empty search"}
	}
	if n := len([]rune(src)); n > MaxQueryLen {
		return nil, &Error{Pos: MaxQueryLen + 1, Msg: fmt.Sprintf("search longer than %d characters", MaxQueryLen)}
	}
	toks, err := lex(src)
	if err != nil {
		return nil, err
	}
	p := &parser{toks: toks}
	root, err := p.or()
	if err != nil {
		return nil, err
	}
	switch t := p.peek(); t.kind {
	case kEnd:
	case kClose:
		return nil, &Error{Pos: t.pos, Msg: "unmatched )"}
	default:
		return nil, &Error{Pos: t.pos, Msg: "unexpected " + describe(t)}
	}
	return &Query{root: root, src: src}, nil
}

func lex(src string) ([]token, error) {
	rs := []rune(src)
	var toks []token
	for i := 0; i < len(rs); {
		r := rs[i]
		pos := i + 1
		switch {
		case unicode.IsSpace(r):
			i++
		case r == '(':
			toks = append(toks, token{kind: kOpen, pos: pos})
			i++
		case r == ')':
			toks = append(toks, token{kind: kClose, pos: pos})
			i++
		case r == '"':
			end := i + 1
			for end < len(rs) && rs[end] != '"' {
				end++
			}
			if end >= len(rs) {
				return nil, &Error{Pos: pos, Msg: "unterminated quote"}
			}
			phrase := Normalize(string(rs[i+1 : end]))
			if phrase == "" {
				return nil, &Error{Pos: pos, Msg: "empty phrase"}
			}
			toks = append(toks, token{kind: kTerm, text: phrase, pos: pos})
			i = end + 1
		default:
			end := i
			for end < len(rs) && !unicode.IsSpace(rs[end]) && rs[end] != '(' && rs[end] != ')' && rs[end] != '"' {
				end++
			}
			word := string(rs[i:end])
			switch strings.ToUpper(word) {
			case "AND":
				toks = append(toks, token{kind: kAnd, pos: pos})
			case "OR":
				toks = append(toks, token{kind: kOr, pos: pos})
			case "NOT":
				toks = append(toks, token{kind: kNot, pos: pos})
			default:
				// a word of nothing but escape codes searches for nothing
				if w := Normalize(word); w != "" {
					toks = append(toks, token{kind: kTerm, text: w, pos: pos})
				}
			}
			i = end
		}
	}
	return append(toks, token{kind: kEnd, pos: len(rs) + 1}), nil
}

// parser is a recursive-descent parser over the tokens:
//
//	or   = and { OR and }
//	and  = not { [AND] not }      adjacent terms are ANDed
//	not  = NOT not | atom
//	atom = term | ( or )
type parser struct {
	toks []token
	i    int
}

func (p *parser) peek() token { return p.toks[p.i] }
func (p *parser) next() token { t := p.toks[p.i]; p.i++; return t }

func (p *parser) or() (node, error) {
	l, err := p.and()
	if err != nil {
		return nil, err
	}
	for p.peek().kind == kOr {
		p.next()
		r, err := p.and()
		if err != nil {
			return nil, err
		}
		l = orNode{l, r}
	}
	return l, nil
}

func (p *parser) and() (node, error) {
	l, err := p.not()
	if err != nil {
		return nil, err
	}
	for {
		switch p.peek().kind {
		case kAnd:
			p.next()
		case kTerm, kNot, kOpen:
			// implicit AND
		default:
			return l, nil
		}
		r, err := p.not()
		if err != nil {
			return nil, err
		}
		l = andNode{l, r}
	}
}

func (p *parser) not() (node, error) {
	if p.peek().kind == kNot {
		p.next()
		x, err := p.not()
		if err != nil {
			return nil, err
		}
		return notNode{x}, nil
	}
	return p.atom()
}

func (p *parser) atom() (node, error) {
	t := p.next()
	switch t.kind {
	case kTerm:
		return termNode{t.text}, nil
	case kOpen:
		if p.peek().kind == kClose {
			return nil, &Error{Pos: t.pos, Msg: "empty ( )"}
		}
		x, err := p.or()
		if err != nil {
			return nil, err
		}
		if c := p.next(); c.kind != kClose {
			return nil, &Error{Pos: t.pos, Msg: "unmatched ("}
		}
		return x, nil
	case kEnd:
		return nil, &Error{Pos: t.pos, Msg: "search ends with an operator"}
	default:
		return nil, &Error{Pos: t.pos, Msg: "missing search term before " + describe(t)}
	}
}

func describe(t token) string {
	switch t.kind {
	case kAnd:
		return "AND"
	case kOr:
		return "OR"
	case kNot:
		return "NOT"
	case kOpen:
		return "("
	case kClose:
		return ")"
	case kEnd:
		return "end"
	}
	return fmt.Sprintf("%q", t.text)
}
