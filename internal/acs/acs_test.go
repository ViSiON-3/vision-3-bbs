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

// TestMatchesOldEvaluator checks that Check decides every legacy string
// exactly as the evaluator it replaced (oracle_test.go). A legacy string is
// one Parse reports nothing for, with no comparison characters and no term
// that needs a session (the oracle's checks have none). Every other string
// changed on purpose, and is held to what changed:
//   - a string Parse reports anything for fails for every caller;
//   - a valid string with L or A in it fails without a session;
//   - a string read with comparisons (S<20) failed for everyone before, and
//     TestComparisons pins what it means now.
//
// TestStrictPolicyChanges lists examples of the first two with both results.
func TestMatchesOldEvaluator(t *testing.T) {
	start := time.Now()
	for _, s := range oracleCases() {
		e := Parse(s)
		special := s == "" || s == "*"
		lacks := e.Valid() && usesSession(e.Root)
		comparisons := e.Valid() && strings.ContainsFunc(s, isCompareRune)
		for _, u := range oracleUsers() {
			want := oldCheckACS(s, u, nil, nil, start)
			ctx := ctxFor(u)
			ctx.Start = start
			got := Check(s, ctx)
			switch {
			case special || u == nil || (e.Valid() && !lacks && !comparisons):
				if got != want {
					t.Errorf("Check(%q) for %+v = %v, old evaluator %v", s, u, got, want)
				}
			case comparisons && !lacks:
				if want {
					t.Errorf("old evaluator passed %q, which is read with comparisons", s)
				}
			case got:
				t.Errorf("Check(%q) for %+v passed; a string with a problem, or a session term and no session, must fail", s, u)
			}
		}
	}
}

// usesSession reports whether any term under n reads the caller's session.
func usesSession(n *Node) bool {
	if n == nil {
		return false
	}
	if n.Kind == TermNode {
		t, _ := parseTerm(n.Text)
		return t.code == "L" || t.code == "A"
	}
	return usesSession(n.X) || usesSession(n.Y)
}

// TestStrictPolicyChanges lists strings whose result the strict policy
// changed, with what the old evaluator gave and what Check gives now, for
// the oracle's users (a new user, a flagged regular, a sysop) and no
// session. Each one is a string vision3 doctor reported before the policy
// changed, as a warning, or one that reads the session.
func TestStrictPolicyChanges(t *testing.T) {
	type res = [3]bool // new user, regular, sysop
	for _, tc := range []struct {
		acs      string
		old, now res
	}{
		// An unknown letter or a bad argument was false, so '!' passed it.
		{"!Q", res{true, true, true}, res{}},
		{"!Sx", res{true, true, true}, res{}},
		{"S10|!Q", res{true, true, true}, res{}},
		{"!FAB", res{true, true, true}, res{}},
		{"H25|S50", res{false, true, true}, res{}},
		{"Y8/17|SYSOP", res{false, false, true}, res{}},
		// A dropped character left the rest of the string working.
		{"S20-", res{false, true, true}, res{}},
		{"S50@", res{false, true, true}, res{}},
		{"S20-&FA", res{false, true, false}, res{}},
		// A session term with no session was false, so '!' or another
		// branch passed.
		{"!L", res{true, true, true}, res{}},
		{"!A", res{true, true, true}, res{}},
		{"SYSOP|L", res{false, false, true}, res{}},
		{"S10&!L", res{true, true, true}, res{}},
	} {
		start := time.Now()
		for i, u := range oracleUsers()[1:] {
			if got := oldCheckACS(tc.acs, u, nil, nil, start); got != tc.old[i] {
				t.Errorf("old evaluator on %q for user %d = %v, listed %v", tc.acs, u.ID, got, tc.old[i])
			}
			ctx := ctxFor(u)
			ctx.Start = start
			if got := Check(tc.acs, ctx); got != tc.now[i] {
				t.Errorf("Check(%q) for user %d = %v, want %v", tc.acs, u.ID, got, tc.now[i])
			}
		}
	}
}

// TestLackingContextFailsWholeString checks that a term needing context the
// check doesn't have fails the whole string, under '!' and beside a passing
// branch, and that the same string is decided normally once the context is
// there.
func TestLackingContextFailsWholeString(t *testing.T) {
	sysop := &user.User{ID: 1, AccessLevel: 255, CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	local := &TesterSession{Local: true, Terminal: true}
	for _, tc := range []struct {
		acs              string
		without, withCtx bool
	}{
		{"L", false, true},
		{"!L", false, false},
		{"SYSOP|L", false, true},
		{"SYSOP|!A", false, true},
		{"S10&(FA|L)", false, true},
	} {
		ctx := ctxFor(sysop)
		if got := Check(tc.acs, ctx); got != tc.without {
			t.Errorf("Check(%q) with no session = %v, want %v", tc.acs, got, tc.without)
		}
		ctx.Session = local
		if got := Check(tc.acs, ctx); got != tc.withCtx {
			t.Errorf("Check(%q) with a local session = %v, want %v", tc.acs, got, tc.withCtx)
		}
	}
	noDate := &user.User{ID: 2, AccessLevel: 255}
	for _, s := range []string{"AGE>=0", "!AGE>=0", "SYSOP|AGE>=0", "SYSOP|!(AGE<1)"} {
		if Check(s, ctxFor(noDate)) {
			t.Errorf("Check(%q) passed for a user with no sign-up date", s)
		}
		if s != "!AGE>=0" && !Check(s, ctxFor(sysop)) {
			t.Errorf("Check(%q) failed for a sysop who signed up in January", s)
		}
	}
}

// TestComparisons pins the comparisons, ranges and readable names against
// the oracle's users: a new user (level 10, nothing else), a flagged
// regular (level 50, ID 5, 3 uploads in 10 calls and per 6 downloads, 40
// file points, conferences 2 and 3) and a sysop (level 255, no downloads,
// a 60-minute limit), at 22:30 on a Wednesday.
func TestComparisons(t *testing.T) {
	now := time.Date(2026, 11, 4, 22, 30, 0, 0, time.Local)
	for _, tc := range []struct {
		acs  string
		want [3]bool // new user, regular, sysop
	}{
		{"S<20", [3]bool{true, false, false}},
		{"S<=50", [3]bool{true, true, false}},
		{"S>50", [3]bool{false, false, true}},
		{"S>=50", [3]bool{false, true, true}},
		{"S=50", [3]bool{false, true, false}},
		{"S20-99", [3]bool{false, true, false}},
		{"S=20-99", [3]bool{false, true, false}},
		{"D<20", [3]bool{true, false, false}},
		{"LEVEL>=50", [3]bool{false, true, true}},
		{"level<20", [3]bool{true, false, false}},
		{"LEVEL=20-99", [3]bool{false, true, false}},
		{"USER=5", [3]bool{false, true, false}},
		{"U<5", [3]bool{true, false, false}},
		{"FILEPOINTS>=40", [3]bool{false, true, false}},
		{"P<40", [3]bool{true, false, true}},
		{"UPLOADSPERCALL>=30", [3]bool{false, true, false}}, // no calls yet fails either way
		{"E<30", [3]bool{false, false, false}},
		{"ULDLRATIO>=50", [3]bool{true, true, true}}, // no downloads is above every number
		{"B<50", [3]bool{false, false, false}},
		{"B=0-49", [3]bool{false, false, false}},
		{"TIMELEFT>=30", [3]bool{true, true, true}}, // no limit, or co-sysop and up
		{"T<30", [3]bool{false, false, false}},
		{"HOUR=22", [3]bool{true, true, true}},
		{"H<22", [3]bool{false, false, false}},
		{"H8-17", [3]bool{false, false, false}},
		{"H22-6", [3]bool{true, true, true}},
		{"HOUR=21-23", [3]bool{true, true, true}},
		{"WEEKDAY=3", [3]bool{true, true, true}},
		{"W1-5", [3]bool{true, true, true}},
		{"W5-1", [3]bool{false, false, false}},
		{"W4-2", [3]bool{false, false, false}},
		{"W6-3", [3]bool{true, true, true}},
		{"MSGCONF=2", [3]bool{false, true, false}},
		{"FILECONF=3", [3]bool{false, true, false}},
		{"C>0&X>0", [3]bool{false, true, false}},
		{"FLAG:A", [3]bool{false, true, false}},
		{"flag:c", [3]bool{false, true, false}},
		{"NOTE:trust", [3]bool{false, true, false}},
		{"TIME:22:00/06:00", [3]bool{true, true, true}},
		{"VALIDATED", [3]bool{false, true, true}},
		{"LEVEL>=50&FLAG:A|SYSOP", [3]bool{false, true, true}},
		{"S<20 | S>99", [3]bool{true, false, true}},
		{"!(S20-99)", [3]bool{true, false, true}},
		// A comparison that can't be read fails the whole string, and '!'
		// can't make it pass: such a string failed for everyone before.
		{"!LEVEL=99-20", [3]bool{false, false, false}},
		{"!S<<20", [3]bool{false, false, false}},
		{"!H25-3", [3]bool{false, false, false}},
		{"!S<20-30", [3]bool{false, false, false}},
		{"!FA=B", [3]bool{false, false, false}},
		{"!LOCAL=1", [3]bool{false, false, false}},
	} {
		users := oracleUsers()[1:]
		for i, u := range users {
			ctx := ctxFor(u)
			ctx.Now = now
			if got := Check(tc.acs, ctx); got != tc.want[i] {
				t.Errorf("Check(%q) for user %d = %v, want %v", tc.acs, u.ID, got, tc.want[i])
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
		{"FA|FB&FC", "(FA | (FB & FC))"},
		{"FA&FB|FC", "((FA & FB) | FC)"},
		{"(FA|FB)&FC", "((FA | FB) & FC)"},
		{"!FA&FB", "(!FA & FB)"},
		{"!(FA|FB)", "!(FA | FB)"},
		{"FA|FB|FC", "((FA | FB) | FC)"},
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

// TestDiagnostics checks what Parse reports and where. Any diagnostic
// leaves the string without a tree.
func TestDiagnostics(t *testing.T) {
	type d struct {
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
		{"S20&", []d{{4, "needs a term on each side"}}},
		{"(S20", []d{{1, "never closed"}}},
		{"S20)", []d{{4, "no matching"}}},
		{"!!FA", []d{{1, "nothing after it"}}},
		{"S20 FA", []d{{5, `no operator before "FA"`}}},
		{"T30|H8-17", nil},
		{"S<20", nil},
		{"LEVEL=99-20", []d{{1, "runs from low to high"}}},
		{"S<<20", []d{{1, "the comparison is one of"}}},
		{"S<20-30", []d{{1, "a range takes = or no comparison"}}},
		{"T30|H25-3", []d{{5, "hour from 0 to 23"}}},
		{"(S<20", []d{{1, "never closed"}}},
		{"S20-&FA", []d{{4, "unexpected character '-'"}}},
		{"FLAG:AB", []d{{1, "exactly one flag"}}},
		{"TERMINAL|LOCAL", nil},
		{"(S20)FA", []d{{6, `no operator before "FA"`}}},
		{"S20FA", []d{{1, "needs a whole number"}}},
		{" ", []d{{1, "no term"}}},
		{"S50@", []d{{4, "unexpected character '@'"}}},
		{"F(A)", []d{{1, "exactly one flag"}, {3, `no operator before "A"`}}},
		{"Q", []d{{1, "not an ACS term"}}},
		{"S10|!Q", []d{{6, "not an ACS term"}}},
		{"!(FA&Sx)", []d{{6, "needs a whole number"}}},
		{"FAB", []d{{1, "exactly one flag"}}},
		{"H25|W7", []d{{1, "hour from 0 to 23"}, {5, "weekday"}}},
		{"Y8/17", []d{{1, "hh:mm/hh:mm"}}},
	} {
		e := Parse(tc.acs)
		got := e.Diagnostics
		if e.Valid() != (len(tc.want) == 0) {
			t.Errorf("Parse(%q).Valid() = %v with diagnostics %v", tc.acs, e.Valid(), got)
		}
		if len(got) != len(tc.want) {
			t.Errorf("Parse(%q): %d diagnostics %v, want %d", tc.acs, len(got), got, len(tc.want))
			continue
		}
		for i, w := range tc.want {
			if g := got[i]; g.Column() != w.col || !strings.Contains(g.Message, w.text) {
				t.Errorf("Parse(%q) diagnostic %d = %v, want column %d containing %q", tc.acs, i, g, w.col, w.text)
			}
		}
	}
}

// TestTermProblemAgreesWithEvalTerm: a term parseTerm reports a problem for
// is false for every caller, and a term it accepts is evaluated.
func TestTermProblemAgreesWithEvalTerm(t *testing.T) {
	bad := []string{"Q", "Sx", "S", "D", "Ux", "Px", "Ex", "Bx", "Tx", "Cx", "Xx", "H24", "Hx", "W7", "W", "F", "FAB", "Y", "Y8", "Y8/17", "Y25:00/26:00", "S20FA", "FLAG:AB", "FLAG:", "NOTE:", "TIME:8/17", "LEVEL=99-20", "S<<20", "S<20-30", "H25-3", "W7-1", "LOCAL=1", "FA=B"}
	for _, term := range bad {
		if _, p := parseTerm(term); p == "" {
			t.Errorf("parseTerm(%q) reports no problem, want one", term)
		}
		for _, u := range oracleUsers()[1:] {
			if EvalTerm(term, ctxFor(u)) {
				t.Errorf("EvalTerm(%q) = true for %+v, want false", term, u)
			}
		}
	}
	good := []string{"S10", "D0", "U1", "P0", "E0", "B0", "T0", "C0", "X0", "H0", "H23", "W0", "W6", "FA", "F1", "Y00:00/23:59", "L", "A", "V", "I", "Z", "Zx", "SYSOP", "cosysop", "V5", "I5", "S<20", "S=20-99", "LEVEL>=20", "HOUR=8-17", "H22-6", "W5-1", "FLAG:A", "NOTE:x", "TIME:22:00/06:00", "TERMINAL", "LOCAL", "VALIDATED", "Zfoo-bar"}
	for _, term := range good {
		if _, p := parseTerm(term); p != "" {
			t.Errorf("parseTerm(%q) = %q, want no problem", term, p)
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

// TestCheckLogsInvalidOnce checks that Check logs an invalid string's
// problems the first time it sees the string and not on each evaluation
// after, and that the string fails every time.
func TestCheckLogsInvalidOnce(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	const s = "s10@&s5"
	logged.Delete(s)
	u := &user.User{AccessLevel: 20}
	for range 3 {
		if Check(s, ctxFor(u)) {
			t.Errorf("Check(%q) passed", s)
		}
	}
	if n := strings.Count(buf.String(), "invalid ACS string"); n != 1 {
		t.Errorf("%d lines logged for %q over 3 checks, want 1:\n%s", n, s, buf.String())
	}
	if !strings.Contains(buf.String(), "unexpected character '@'") {
		t.Errorf("the log doesn't name the character:\n%s", buf.String())
	}
	Check("s10&s5", ctxFor(u))
	if n := strings.Count(buf.String(), "invalid ACS string"); n != 1 {
		t.Errorf("a clean string was logged:\n%s", buf.String())
	}
}
