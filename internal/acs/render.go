package acs

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// The markers Render puts under the expression.
const (
	markPass    = '+'
	markFail    = '-'
	markSession = '~'
	markError   = 'X'
)

// PassNote is the footnote under every explanation.
const PassNote = "A pass means this expression passed for this user in this context. It " +
	"doesn't mean every access check on a resource passes: a conference, area or " +
	"door can have rules of its own."

// labelWidth is the width of the labels down the left of Render's header.
const labelWidth = 12

// Render lays an explanation out as plain text, one string per line, no
// longer than width columns (0 for no limit). ./ue and the BBS's user editor
// both show these lines as they are, so the two always say the same thing.
//
// The header names the user, the session and the time used, then the
// expression with a marker under each term (+ passed, - failed, ~ needs a
// session or a value the record lacks) or under each problem (X), and the
// verdict. Under
// it is every part of the expression, indented by depth, with its result and
// why, and last the fixed note that a pass is about this expression only.
func Render(x Explanation, width int) []string {
	out := contextLines(x.Context, x.CurrentTime, width)
	field := func(label, text string) {
		out = append(out, wrap(fmt.Sprintf("%-*s", labelWidth, label), text, width)...)
	}

	src, marks := []rune(x.Source), markers(x)
	if len(src) == 0 {
		src = []rune(`""`)
		marks = []rune("  ")
	}
	chunk := len(src)
	if width > labelWidth {
		chunk = width - labelWidth
	}
	for i := 0; i < len(src); i += chunk {
		end := min(i+chunk, len(src))
		label := ""
		if i == 0 {
			label = "Expression:"
		}
		out = append(out, fmt.Sprintf("%-*s%s", labelWidth, label, string(src[i:end])))
		if m := strings.TrimRight(string(marks[i:end]), " "); m != "" {
			out = append(out, strings.Repeat(" ", labelWidth)+m)
		}
	}
	field("Result:", verdict(x))

	if x.Root != nil {
		out = append(out, "")
		out = append(out, renderSteps(x.Root, width)...)
	}
	if x.Detail != "" {
		out = append(out, "")
		out = append(out, wrap("", x.Detail, width)...)
	}
	if len(x.Diagnostics) > 0 {
		out = append(out, "")
		for _, d := range x.Diagnostics {
			out = append(out, wrap("  ", fmt.Sprintf("error at column %d: %s", d.Column(), d.Message), width)...)
		}
	}
	out = append(out, "")
	out = append(out, wrap("", PassNote, width)...)
	return out
}

// ContextLines are the first lines Render draws, for showing the context
// before anything is tested: the user, the session and the time t uses.
func ContextLines(t Tester, u *user.User, width int) []string {
	return contextLines(t.Context(u), t.At.IsZero(), width)
}

func contextLines(ctx Context, current bool, width int) []string {
	var out []string
	field := func(label, text string) {
		out = append(out, wrap(fmt.Sprintf("%-*s", labelWidth, label), text, width)...)
	}
	field("User:", describeUser(ctx.User))
	field("Session:", describeSession(ctx))
	when := ctx.Now.Format("Mon 2006-01-02 15:04")
	if current {
		when += " (now)"
	}
	field("Time:", when)
	return out
}

func describeUser(u *user.User) string {
	if u == nil {
		return "none (not logged in)"
	}
	return fmt.Sprintf("%s (#%d), level %d", u.Handle, u.ID, u.AccessLevel)
}

// describeSession says what the session terms read in ctx.
func describeSession(ctx Context) string {
	s := ctx.Session
	if s == nil {
		return "none: L or A fails the string, T as if the call just started"
	}
	where := "remote"
	if EvalTerm("L", ctx) {
		where = "local"
	}
	term := "no terminal"
	if s.HasPTY() {
		term = "a terminal"
	}
	return fmt.Sprintf("%s call, %s, %d min in, %d min chat credit",
		where, term, int(ctx.Now.Sub(ctx.Start)/time.Minute), int(s.ChatCredit()/time.Minute))
}

// verdict is the one-line result.
func verdict(x Explanation) string {
	v := "FAIL"
	if x.Result {
		v = "PASS"
	}
	switch {
	case x.Root == nil && x.Reason == Invalid:
		return v + ": the string has a problem"
	case x.Root == nil:
		return v
	case x.Reason == NoSession:
		return v + ": a part needs a session the context doesn't have"
	case x.Reason == Unavailable:
		return v + ": a part needs a value the user's record doesn't have"
	}
	return v
}

// markers is the line under the expression: one marker per term character,
// or per character of each problem.
func markers(x Explanation) []rune {
	m := []rune(strings.Repeat(" ", utf8.RuneCountInString(x.Source)))
	fill := func(pos, end int, r rune) {
		for i := max(pos, 0); i < end && i < len(m); i++ {
			m[i] = r
		}
	}
	var walk func(st *Step)
	walk = func(st *Step) {
		if st == nil {
			return
		}
		if st.Kind == TermNode {
			r := markFail
			switch {
			case st.Reason == NoSession || st.Reason == Unavailable:
				r = markSession
			case st.Result:
				r = markPass
			}
			fill(st.Pos, st.End, r)
		}
		walk(st.X)
		walk(st.Y)
	}
	walk(x.Root)
	for _, d := range x.Diagnostics {
		fill(d.Pos, d.End, markError)
	}
	return m
}

// renderSteps lists every step under root, one per line, indented by depth:
// result, reason when it isn't an ordinary one, the part's text and detail.
func renderSteps(root *Step, width int) []string {
	type row struct {
		head, detail string
	}
	var rows []row
	var walk func(st *Step, depth int)
	walk = func(st *Step, depth int) {
		res := "FAIL"
		if st.Result {
			res = "PASS"
		}
		why := ""
		if st.Reason.problem() {
			why = st.Reason.String()
		}
		rows = append(rows, row{fmt.Sprintf("  %s %-10s %s%s", res, why, strings.Repeat("  ", depth), st.Text), st.Detail})
		if st.X != nil {
			walk(st.X, depth+1)
		}
		if st.Y != nil {
			walk(st.Y, depth+1)
		}
	}
	walk(root, 0)

	col := 0
	for _, r := range rows {
		col = max(col, utf8.RuneCountInString(r.head)+2)
	}
	if width > 0 {
		col = min(col, width/2)
	}
	var out []string
	for _, r := range rows {
		head := r.head
		if h := []rune(head); width > 3 && len(h) > width {
			head = string(h[:width-3]) + "..."
		}
		if n := utf8.RuneCountInString(head); n+2 > col {
			// Too long to share a line with its detail.
			out = append(out, head)
			head = ""
		}
		out = append(out, wrap(fmt.Sprintf("%-*s", col, head), r.detail, width)...)
	}
	return out
}

// wrap word-wraps text to width after prefix, indenting the lines after the
// first by prefix's width. A width of 0 doesn't wrap.
func wrap(prefix, text string, width int) []string {
	indent := strings.Repeat(" ", utf8.RuneCountInString(prefix))
	room := width - utf8.RuneCountInString(prefix)
	if width <= 0 || room < 20 {
		return []string{strings.TrimRight(prefix+text, " ")}
	}
	var out []string
	line := prefix
	n := 0
	for _, field := range strings.Fields(text) {
		// A word longer than the room (an ACS string has no spaces to break
		// at) is split at the room, so no line runs past width.
		for _, word := range splitAt(field, room) {
			w := utf8.RuneCountInString(word)
			if n > 0 && n+1+w > room {
				out = append(out, line)
				line, n = indent, 0
			}
			if n > 0 {
				line += " "
				n++
			}
			line += word
			n += w
		}
	}
	return append(out, strings.TrimRight(line, " "))
}

// splitAt cuts word into pieces of at most n runes.
func splitAt(word string, n int) []string {
	r := []rune(word)
	if len(r) <= n {
		return []string{word}
	}
	var parts []string
	for len(r) > n {
		parts = append(parts, string(r[:n]))
		r = r[n:]
	}
	return append(parts, string(r))
}
