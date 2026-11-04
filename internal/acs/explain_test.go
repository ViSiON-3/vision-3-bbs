package acs

import (
	"strings"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// testerContexts are the contexts the agreement test checks each string in:
// no session, and sessions local and remote, with and without a terminal
// or chat credit, at times that move the clock terms both ways.
func testerContexts() []Tester {
	var ts []Tester
	for _, at := range []time.Time{
		time.Date(2026, 11, 4, 22, 0, 0, 0, time.Local), // a Wednesday
		time.Date(2026, 11, 1, 8, 30, 0, 0, time.Local), // a Sunday
	} {
		for _, s := range []*TesterSession{
			nil,
			{Local: true, Terminal: true},
			{Elapsed: 45 * time.Minute, Credit: 10 * time.Minute},
			{Terminal: true, Elapsed: 2 * time.Hour},
		} {
			ts = append(ts, Tester{Session: s, At: at, SysOpLevel: oracleSysOp, CoSysOpLevel: oracleCoSysOp})
		}
	}
	return ts
}

// TestExplainAgreesWithCheck is the tester's guarantee: for the same string
// and context, Explain's result is Check's, and every step's result is
// what evaluating that part on its own gives.
func TestExplainAgreesWithCheck(t *testing.T) {
	for _, s := range oracleCases() {
		for _, u := range oracleUsers() {
			for _, tc := range testerContexts() {
				ctx := tc.Context(u)
				x := Explain(s, ctx)
				if want := Check(s, ctx); x.Result != want {
					t.Fatalf("Explain(%q) for %+v in %+v = %v, Check %v", s, u, tc, x.Result, want)
				}
				if x.Root != nil {
					checkSteps(t, s, x.Root, ctx)
				}
			}
		}
	}
}

func checkSteps(t *testing.T, s string, st *Step, ctx Context) {
	t.Helper()
	var want bool
	switch st.Kind {
	case TermNode:
		want = EvalTerm(st.Text, ctx)
	case NotNode:
		checkSteps(t, s, st.X, ctx)
		want = !st.X.Result
	case AndNode:
		checkSteps(t, s, st.X, ctx)
		checkSteps(t, s, st.Y, ctx)
		want = st.X.Result && st.Y.Result
	default:
		checkSteps(t, s, st.X, ctx)
		checkSteps(t, s, st.Y, ctx)
		want = st.X.Result || st.Y.Result
	}
	if st.Result != want {
		t.Fatalf("Explain(%q): step %q = %v, want %v", s, st.Text, st.Result, want)
	}
}

// explainUser is a level-20 caller with flags A and C and a 60-minute limit.
func explainUser() *user.User {
	return &user.User{ID: 5, Handle: "Bob", AccessLevel: 20, Flags: "AC", Validated: true, TimeLimit: 60,
		TimesCalled: 10, NumUploads: 3, NumDownloads: 6, FilePoints: 40, PrivateNote: "trusted caller",
		CurrentMsgConferenceID: 2, CurrentFileConferenceID: 3}
}

// at22 is Wednesday 4 November 2026, 22:00.
var at22 = time.Date(2026, 11, 4, 22, 0, 0, 0, time.Local)

// TestExplainSteps checks each part's result, reason and detail for
// representative strings.
func TestExplainSteps(t *testing.T) {
	type want struct {
		result bool
		reason Reason
		detail string // a substring of the step's detail
	}
	session := &TesterSession{Terminal: true, Elapsed: 50 * time.Minute}
	for _, tc := range []struct {
		acs     string
		session *TesterSession
		result  bool
		reason  Reason
		steps   map[string]want // by step text
	}{
		{"S20", nil, true, Match, map[string]want{"S20": {true, Match, "level 20 is at least 20"}}},
		{"S50", nil, false, NoMatch, map[string]want{"S50": {false, NoMatch, "level 20 is below 50"}}},
		{"FA&FB", nil, false, NoMatch, map[string]want{
			"FA&FB": {false, NoMatch, "FB failed"},
			"FA":    {true, Match, "has flag A (flags: AC)"},
			"FB":    {false, NoMatch, "doesn't have flag B"},
		}},
		{"S50|FC", nil, true, Match, map[string]want{"S50|FC": {true, Match, "FC passed"}}},
		{"!FB", nil, true, Match, map[string]want{"!FB": {true, Match, "FB failed, so this passes"}}},
		{"(S99|FA)&FB", nil, false, NoMatch, map[string]want{
			"(S99|FA)&FB": {false, NoMatch, "FB failed"},
			"S99|FA":      {true, Match, "FA passed"},
		}},
		{"!(FA|FB)", nil, false, NoMatch, map[string]want{"!(FA|FB)": {false, NoMatch, "FA|FB passed, so this fails"}}},
		// An invalid term is false, so ! makes it pass, as at runtime.
		{"!Q", nil, true, Invalid, map[string]want{
			"!Q": {true, Invalid, "stricter ACS check will deny it"},
			"Q":  {false, Invalid, "Q is not an ACS term"},
		}},
		{"S10|Q", nil, true, Match, map[string]want{"S10|Q": {true, Match, "S10 passed"}}},
		{"S10&!Q", nil, true, Invalid, map[string]want{"S10&!Q": {true, Invalid, "rests on an invalid term"}}},
		{"S50&!Q", nil, false, NoMatch, map[string]want{"S50&!Q": {false, NoMatch, "S50 failed"}}},
		{"Sx", nil, false, Invalid, map[string]want{"Sx": {false, Invalid, "S needs a whole number"}}},
		{"FAB|S10", nil, true, Match, map[string]want{"FAB": {false, Invalid, "exactly one flag"}}},
		// The dropped @ is reported; the tree is S10 alone.
		{"S10@", nil, true, Match, map[string]want{"S10": {true, Match, "at least 10"}}},
		// Session terms without a session, and with one.
		{"L", nil, false, NoSession, map[string]want{"L": {false, NoSession, "needs a session"}}},
		{"!A", nil, true, NoSession, map[string]want{"!A": {true, NoSession, "rests on a session"}}},
		{"A&!L", session, true, Match, map[string]want{
			"A": {true, Match, "has a terminal"},
			"L": {false, NoMatch, "not local"},
		}},
		{"T30", nil, true, NoSession, map[string]want{"T30": {true, NoSession, "taken to have just started: minutes left 60 is at least 30"}}},
		{"T30", session, false, NoMatch, map[string]want{"T30": {false, NoMatch, "minutes left 10 is below 30"}}},
		// Clock terms at 22:00 on a Wednesday.
		{"H22&W3", nil, true, Match, map[string]want{"H22": {true, Match, "the hour is 22"}, "W3": {true, Match, "Wednesday"}}},
		{"H8|Y08:00/17:00", nil, false, NoMatch, map[string]want{
			"H8":           {false, NoMatch, "the hour is 22, not 8"},
			"Y08:00/17:00": {false, NoMatch, "22:00 is outside 08:00-17:00"},
		}},
		{"SYSOP", nil, false, NoMatch, map[string]want{"SYSOP": {false, NoMatch, "below 255 (sysOpLevel)"}}},
		{"E30&B50&P40&Ztrusted&C2&X3&U5&V", nil, true, Match, map[string]want{
			"E30":      {true, Match, "3 uploads in 10 calls: 30% is at least 30"},
			"B50":      {true, Match, "50% is at least 50"},
			"Ztrusted": {true, Match, `contains "trusted"`},
			"C2":       {true, Match, "message conference is 2"},
		}},
	} {
		x := Tester{Session: tc.session, At: at22, SysOpLevel: 255, CoSysOpLevel: 250}.Explain(tc.acs, explainUser())
		if x.Result != tc.result || x.Reason != tc.reason {
			t.Errorf("Explain(%q) = %v, %s; want %v, %s", tc.acs, x.Result, x.Reason, tc.result, tc.reason)
		}
		got := map[string]*Step{}
		var walk func(*Step)
		walk = func(st *Step) {
			if st != nil {
				got[st.Text] = st
				walk(st.X)
				walk(st.Y)
			}
		}
		walk(x.Root)
		for text, w := range tc.steps {
			st, ok := got[text]
			if !ok {
				t.Errorf("Explain(%q): no step %q", tc.acs, text)
				continue
			}
			if st.Result != w.result || st.Reason != w.reason || !strings.Contains(st.Detail, w.detail) {
				t.Errorf("Explain(%q) step %q = %v, %s, %q; want %v, %s, containing %q",
					tc.acs, text, st.Result, st.Reason, st.Detail, w.result, w.reason, w.detail)
			}
		}
	}
}

// TestExplainWholeString covers the results that have no tree.
func TestExplainWholeString(t *testing.T) {
	ctx := Tester{At: at22}.Context(explainUser())
	for _, tc := range []struct {
		acs    string
		user   bool
		result bool
		reason Reason
	}{
		{"", true, true, Match},
		{"*", true, true, Match},
		{"*", false, true, Match},
		{"S0", false, false, NoMatch},
		{"S20&", true, false, Invalid},
		{"!!FA", true, false, Invalid},
		{"T30|H8-17", true, false, Invalid},
	} {
		c := ctx
		if !tc.user {
			c.User = nil
		}
		x := Explain(tc.acs, c)
		if x.Result != tc.result || x.Reason != tc.reason || x.Root != nil || x.Detail == "" {
			t.Errorf("Explain(%q, user %v) = %v, %s, root %v, detail %q", tc.acs, tc.user, x.Result, x.Reason, x.Root, x.Detail)
		}
	}
	x := Explain("S10@&s5", ctx)
	if len(x.Ignored) != 1 || x.Ignored[0].Column() != 4 {
		t.Errorf("Ignored = %v, want the @ at column 4", x.Ignored)
	}
}

// TestExplainCurrentTime checks that a context without a time is checked at
// the current time, read once, and says so.
func TestExplainCurrentTime(t *testing.T) {
	before := time.Now()
	x := Tester{}.Explain("H0|!H0", explainUser())
	if !x.CurrentTime || x.Context.Now.Before(before) || x.Context.Now.After(time.Now()) {
		t.Errorf("CurrentTime %v, Now %v", x.CurrentTime, x.Context.Now)
	}
	if x.Context.Session != nil || !x.Context.Start.Equal(x.Context.Now) {
		t.Errorf("default context: session %v, start %v, now %v", x.Context.Session, x.Context.Start, x.Context.Now)
	}
	if y := (Tester{At: at22}).Explain("H22", explainUser()); y.CurrentTime || !y.Result {
		t.Errorf("explicit time: CurrentTime %v, result %v", y.CurrentTime, y.Result)
	}
}

func TestRender(t *testing.T) {
	render := func(acs string, tc Tester) string {
		tc.SysOpLevel, tc.CoSysOpLevel = 255, 250
		return strings.Join(Render(tc.Explain(acs, explainUser()), 79), "\n")
	}
	for _, tc := range []struct {
		acs    string
		tester Tester
		want   string
	}{
		{"S50&(FA|!Q)", Tester{At: at22}, `User:       Bob (#5), level 20
Session:    none: L and A fail, and T counts the call as just started
Time:       Wed 2026-11-04 22:00
Expression: S50&(FA|!Q)
            ---  ++  ?
Result:     FAIL

  FAIL            S50&(FA|!Q)  both parts must pass: S50 failed
  FAIL              S50        level 20 is below 50
  PASS              FA|!Q      either part may pass: both passed
  PASS                FA       has flag A (flags: AC)
  PASS invalid        !Q       Q failed, so this passes; that rests on an
                               invalid term, and a stricter ACS check will deny
                               it
  FAIL invalid          Q      "Q": Q is not an ACS term, so it is always false

A pass means this expression passed for this user in this context. It doesn't
mean every access check on a resource passes: a conference, area or door can
have rules of its own.`},
		{"T30|H8-17", Tester{At: at22}, `User:       Bob (#5), level 20
Session:    none: L and A fail, and T counts the call as just started
Time:       Wed 2026-11-04 22:00
Expression: T30|H8-17
                  ^XX
Result:     FAIL: the string can't be read as a whole

the string can't be read as a whole, so it fails for everyone and '!' can't
make it pass

  warning at column 7: unexpected character '-' is ignored and ends the term
  before it
  error at column 8: no operator before "17": put & or | between terms
  warning at column 8: "17": 1 is not an ACS term

A pass means this expression passed for this user in this context. It doesn't
mean every access check on a resource passes: a conference, area or door can
have rules of its own.`},
		{"T30&A|S10@", Tester{At: at22, Session: &TesterSession{Local: true, Elapsed: 40 * time.Minute}}, `User:       Bob (#5), level 20
Session:    local call, no terminal, 40 min in, 0 min chat credit
Time:       Wed 2026-11-04 22:00
Expression: T30&A|S10@
            --- - +++^
Result:     PASS

  PASS            T30&A|S10  either part may pass: S10 passed
  FAIL              T30&A    both parts must pass: both failed
  FAIL                T30    minutes left 20 is below 30 (limit 60)
  FAIL                A      the connection has no terminal
  PASS              S10      level 20 is at least 10

  warning at column 10: unexpected character '@' is ignored and ends the term
  before it

A pass means this expression passed for this user in this context. It doesn't
mean every access check on a resource passes: a conference, area or door can
have rules of its own.`},
	} {
		if got := render(tc.acs, tc.tester); got != tc.want {
			t.Errorf("Render(%q):\n%s\n\nwant:\n%s", tc.acs, got, tc.want)
		}
	}

	// Every line fits the width asked for.
	x := Tester{At: at22}.Explain("(S10&FA&FC&V&!FX&E30&B50)|(Ztrusted&C2&X3&U5&!L&T30&H22&W3&Y20:00/23:00)", explainUser())
	for _, l := range Render(x, 40) {
		if len([]rune(l)) > 40 {
			t.Errorf("line wider than 40: %q", l)
		}
	}
}

func TestParseTesterTime(t *testing.T) {
	today := time.Date(2026, 11, 4, 9, 15, 0, 0, time.Local)
	for in, want := range map[string]time.Time{
		"":                 {},
		" now ":            {},
		"22:00":            time.Date(2026, 11, 4, 22, 0, 0, 0, time.Local),
		"2026-12-25 08:30": time.Date(2026, 12, 25, 8, 30, 0, 0, time.Local),
	} {
		got, err := ParseTesterTime(in, today)
		if err != nil || !got.Equal(want) {
			t.Errorf("ParseTesterTime(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	for _, in := range []string{"10pm", "25:00", "2026-13-01 00:00"} {
		if _, err := ParseTesterTime(in, today); err == nil {
			t.Errorf("ParseTesterTime(%q) accepted", in)
		}
	}
}
