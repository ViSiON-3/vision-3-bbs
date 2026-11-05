package acs

import (
	"bytes"
	"log/slog"
	"math/rand"
	"strings"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

func ctxFor(u *user.User) Context {
	return Context{User: u, Start: time.Now(), SysOpLevel: oracleSysOp, CoSysOpLevel: oracleCoSysOp}
}

// oracleUsers are the callers TestMatchesOldEvaluator checks every string
// against: none, a new user, a flagged regular and a sysop.
func oracleUsers() []*user.User {
	return []*user.User{
		nil,
		{ID: 1, AccessLevel: 10},
		{ID: 5, AccessLevel: 50, Flags: "AC", Validated: true, TimesCalled: 10, NumUploads: 3, NumDownloads: 6, FilePoints: 40, PrivateNote: "trusted", CurrentMsgConferenceID: 2, CurrentFileConferenceID: 3},
		{ID: 9, AccessLevel: 255, Flags: "ZI", Validated: true, TimeLimit: 60},
	}
}

// TestMatchesOldEvaluator checks that Check decides every string exactly as
// the evaluator it replaced (oracle_test.go).
func TestMatchesOldEvaluator(t *testing.T) {
	start := time.Now()
	for _, s := range oracleCases() {
		for _, u := range oracleUsers() {
			want := oldCheckACS(s, u, nil, nil, start)
			ctx := ctxFor(u)
			ctx.Start = start
			if got := Check(s, ctx); got != want {
				t.Errorf("Check(%q) for %+v = %v, old evaluator %v", s, u, got, want)
			}
		}
	}
}

// oracleCases are the strings TestMatchesOldEvaluator and the tester's
// agreement test check: hand-picked ones, including every quirk the
// reference documents, then random ones.
func oracleCases() []string {
	cases := []string{
		"", "*", "**", " ", "*&S10", "S10", "s20", "S", "Sx", "S20FA", "S20 FA", "S20&FA", "S20 & FA",
		"!FA", "!!FA", "!(FA|FB)", "(S99|FA)&FB", "S99|FA&FC", "FB&FA|FC", "!FA&S10", "S20&", "&S20",
		"(S20", "S20)", "()", "!", "|", "Q", "!Q", "!Sx", "S<20", "S<30", "F(A)", "FAC", "fc", "Z", "Ztrusted",
		"V", "V5", "I", "I5", "U5", "E30", "E31", "B50", "B999", "P40", "C2", "X3", "T999", "T30",
		"L", "!L", "A", "!A", "SYSOP", "sysop", "COSYSOP", "SYSOP|U1", "V&!FX", "(S50|FM)&T10",
		"Y00:00/23:59", "Y8/17", "H25", "W7", "S50@FA", "S50\tFA", "((S10))", "!(!(S10))", "S10&(FA|(FC&!FB))",
		"S10||FA", "S10&&FA", "S10&|FA", "S10!FA", "S10&!", "!(", ")(", "S10)(FA",
	}
	r := rand.New(rand.NewSource(608))
	const alphabet = "SFVZQLAIUPECXBD0123456789&|!() :/<*"
	for range 4000 {
		var b strings.Builder
		for range 1 + r.Intn(10) {
			b.WriteByte(alphabet[r.Intn(len(alphabet))])
		}
		cases = append(cases, b.String())
	}
	return cases
}

// TestParseTree checks the tree's shape and precedence: ! before & before |,
// left to right.
func TestParseTree(t *testing.T) {
	for _, tc := range []struct{ acs, want string }{
		{"S50", "S50"},
		{"S50&FA", "(S50 & FA)"},
		{"S50&!L", "(S50 & !L)"},
		{"S50 & FA", "(S50 & FA)"},
		{"A|B&C", "(A | (B & C))"},
		{"A&B|C", "((A & B) | C)"},
		{"(A|B)&C", "((A | B) & C)"},
		{"!A&B", "(!A & B)"},
		{"!(A|B)", "!(A | B)"},
		{"A|B|C", "((A | B) | C)"},
		{"S50@FA&V", "(FA & V)"}, // never reached: S50 FA has no operator
	} {
		e := Parse(tc.acs)
		if tc.acs == "S50@FA&V" {
			if e.Valid() {
				t.Errorf("Parse(%q) is valid, want an error", tc.acs)
			}
			continue
		}
		if got := show(e.Root); got != tc.want {
			t.Errorf("Parse(%q) = %s, want %s", tc.acs, got, tc.want)
		}
	}
}

func show(n *Node) string {
	if n == nil {
		return "<nil>"
	}
	switch n.Kind {
	case TermNode:
		return n.Text
	case NotNode:
		return "!" + show(n.X)
	case AndNode:
		return "(" + show(n.X) + " & " + show(n.Y) + ")"
	default:
		return "(" + show(n.X) + " | " + show(n.Y) + ")"
	}
}

// TestParsePositions checks node spans, in runes. A span covers the node's
// terms and operators; a closing parenthesis after it is not part of it.
func TestParsePositions(t *testing.T) {
	e := Parse("S20 & !(FA|FB)")
	root := e.Root
	if root.Kind != AndNode || root.Pos != 0 || root.End != 13 || root.Op != 4 {
		t.Fatalf("root = %+v", root)
	}
	if x := root.X; x.Text != "S20" || x.Pos != 0 || x.End != 3 {
		t.Errorf("left = %+v", x)
	}
	if n := root.Y; n.Kind != NotNode || n.Pos != 6 || n.X.Kind != OrNode || n.X.X.Pos != 8 || n.X.Y.End != 13 {
		t.Errorf("right = %+v / %+v", n, n.X)
	}
}

// TestDiagnostics checks what Parse reports and where.
func TestDiagnostics(t *testing.T) {
	type d struct {
		sev  Severity
		col  int
		text string
	}
	for _, tc := range []struct {
		acs  string
		want []d
	}{
		{"S20&FA", nil},
		{"SYSOP|U1", nil},
		{"V5", nil}, // an ignored argument is not reported
		{"S20&", []d{{Error, 4, "needs a term on each side"}}},
		{"(S20", []d{{Error, 1, "never closed"}}},
		{"S20)", []d{{Error, 4, "no matching"}}},
		{"!!FA", []d{{Error, 1, "nothing after it"}}},
		{"S20 FA", []d{{Error, 5, `no operator before "FA"`}}},
		{"T30|H8-17", []d{{Warning, 7, "unexpected character '-'"}, {Error, 8, `no operator before "17"`}, {Warning, 8, "1 is not an ACS term"}}},
		{"(S20)FA", []d{{Error, 6, `no operator before "FA"`}}},
		{"S20FA", []d{{Warning, 1, "needs a whole number"}}},
		{" ", []d{{Error, 1, "no term"}}},
		{"S<20", []d{{Warning, 1, `"S": S needs a whole number`}, {Warning, 2, "unexpected character '<'"}, {Error, 3, `no operator before "20"`}, {Warning, 3, "2 is not an ACS term"}}},
		{"S50@", []d{{Warning, 4, "unexpected character '@'"}}},
		{"F(A)", []d{{Error, 3, `no operator before "A"`}, {Warning, 1, "exactly one flag"}}},
		{"Q", []d{{Warning, 1, "not an ACS term, so it is always false"}}},
		{"S10|!Q", []d{{Warning, 6, "the '!' before it makes that part pass"}}},
		{"!(FA&Sx)", []d{{Warning, 6, "the '!' before it"}}},
		{"FAB", []d{{Warning, 1, "exactly one flag"}}},
		{"H25|W7", []d{{Warning, 1, "hour from 0 to 23"}, {Warning, 5, "weekday"}}},
		{"Y8/17", []d{{Warning, 1, "hh:mm/hh:mm"}}},
	} {
		got := Parse(tc.acs).Diagnostics
		if len(got) != len(tc.want) {
			t.Errorf("Parse(%q): %d diagnostics %v, want %d", tc.acs, len(got), got, len(tc.want))
			continue
		}
		for _, w := range tc.want {
			found := false
			for _, g := range got {
				if g.Severity == w.sev && g.Column() == w.col && strings.Contains(g.Message, w.text) {
					found = true
				}
			}
			if !found {
				t.Errorf("Parse(%q) = %v, want a %s at column %d containing %q", tc.acs, got, w.sev, w.col, w.text)
			}
		}
	}
}

// TestTermProblemAgreesWithEvalTerm: a term termProblem reports is false for
// every caller, and a term it accepts is evaluated.
func TestTermProblemAgreesWithEvalTerm(t *testing.T) {
	bad := []string{"Q", "Sx", "S", "D", "Ux", "Px", "Ex", "Bx", "Tx", "Cx", "Xx", "H24", "Hx", "W7", "W", "F", "FAB", "Y", "Y8", "Y8/17", "Y25:00/26:00", "S20FA"}
	for _, term := range bad {
		if termProblem(term) == "" {
			t.Errorf("termProblem(%q) = \"\", want a problem", term)
		}
		for _, u := range oracleUsers()[1:] {
			if EvalTerm(term, ctxFor(u)) {
				t.Errorf("EvalTerm(%q) = true for %+v, want false", term, u)
			}
		}
	}
	good := []string{"S10", "D0", "U1", "P0", "E0", "B0", "T0", "C0", "X0", "H0", "H23", "W0", "W6", "FA", "F1", "Y00:00/23:59", "L", "A", "V", "I", "Z", "Zx", "SYSOP", "cosysop", "V5", "I5"}
	for _, term := range good {
		if p := termProblem(term); p != "" {
			t.Errorf("termProblem(%q) = %q, want none", term, p)
		}
	}
}

// TestValidate checks that "" and "*" report nothing.
func TestValidate(t *testing.T) {
	if d := Validate(""); d != nil {
		t.Errorf("Validate(\"\") = %v", d)
	}
	if d := Validate("*"); d != nil {
		t.Errorf("Validate(\"*\") = %v", d)
	}
	if d := Validate("**"); len(d) == 0 {
		t.Error("Validate(\"**\") reported nothing")
	}
}

// TestContextNow checks that the clock terms use Context.Now when it is set.
func TestContextNow(t *testing.T) {
	u := &user.User{AccessLevel: 10}
	ctx := Context{User: u, Now: time.Date(2026, 11, 3, 14, 30, 0, 0, time.UTC)} // a Tuesday
	for acs, want := range map[string]bool{"H14": true, "H15": false, "W2": true, "Y14:00/15:00": true, "Y15:00/16:00": false} {
		if got := Check(acs, ctx); got != want {
			t.Errorf("Check(%q) at 14:30 Tuesday = %v, want %v", acs, got, want)
		}
	}
}

func TestInTimeRange(t *testing.T) {
	at := func(h, m, s int) time.Time { return time.Date(2026, 10, 26, h, m, s, 0, time.UTC) }
	hm := func(h, m int) time.Time { return time.Date(0, 1, 1, h, m, 0, 0, time.UTC) }
	for _, tc := range []struct {
		now        time.Time
		start, end time.Time
		want       bool
	}{
		{at(0, 0, 0), hm(0, 0), hm(23, 59), true},
		{at(23, 59, 40), hm(0, 0), hm(23, 59), true},
		{at(12, 0, 0), hm(9, 0), hm(17, 0), true},
		{at(17, 0, 59), hm(9, 0), hm(17, 0), true},
		{at(17, 1, 0), hm(9, 0), hm(17, 0), false},
		{at(8, 59, 59), hm(9, 0), hm(17, 0), false},
		{at(22, 0, 0), hm(22, 0), hm(6, 0), true},
		{at(3, 0, 0), hm(22, 0), hm(6, 0), true},
		{at(6, 0, 30), hm(22, 0), hm(6, 0), true},
		{at(12, 0, 0), hm(22, 0), hm(6, 0), false},
	} {
		if got := InTimeRange(tc.now, tc.start, tc.end); got != tc.want {
			t.Errorf("InTimeRange(%s, %s-%s) = %v, want %v", tc.now.Format("15:04:05"),
				tc.start.Format("15:04"), tc.end.Format("15:04"), got, tc.want)
		}
	}
}

// TestCheckWarnsOnce checks that Check logs a string's warnings, such as an
// ignored character, the first time it sees the string and not on each
// evaluation after.
func TestCheckWarnsOnce(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	const s = "s10@&s5"
	warned.Delete(s)
	u := &user.User{AccessLevel: 20}
	for range 3 {
		Check(s, ctxFor(u))
	}
	if n := strings.Count(buf.String(), "ACS string warning"); n != 1 {
		t.Errorf("%d warnings logged for %q over 3 checks, want 1:\n%s", n, s, buf.String())
	}
	if !strings.Contains(buf.String(), "unexpected character '@'") {
		t.Errorf("warning doesn't name the ignored character:\n%s", buf.String())
	}
	Check("s10&s5", ctxFor(u))
	if n := strings.Count(buf.String(), "ACS string warning"); n != 1 {
		t.Errorf("a clean string logged a warning:\n%s", buf.String())
	}
}
