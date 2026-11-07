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
// what evaluating that part on its own gives: a part with a term under it
// that lacks its context fails, and otherwise the operators combine as
// usual.
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
	lacks := func(c *Step) bool { return c.Reason == NoSession || c.Reason == Unavailable }
	switch st.Kind {
	case TermNode:
		want = EvalTerm(st.Text, ctx)
	case NotNode:
		checkSteps(t, s, st.X, ctx)
		want = !st.X.Result && !lacks(st.X)
	case AndNode:
		checkSteps(t, s, st.X, ctx)
		checkSteps(t, s, st.Y, ctx)
		want = st.X.Result && st.Y.Result
	default:
		checkSteps(t, s, st.X, ctx)
		checkSteps(t, s, st.Y, ctx)
		want = (st.X.Result || st.Y.Result) && !lacks(st.X) && !lacks(st.Y)
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
		// Session terms without a session fail the whole string, and with
		// one are checked as usual.
		{"L", nil, false, NoSession, map[string]want{"L": {false, NoSession, "needs a session"}}},
		{"!A", nil, false, NoSession, map[string]want{"!A": {false, NoSession, "A can't be checked here"}}},
		{"S10|L", nil, false, NoSession, map[string]want{
			"S10|L": {false, NoSession, "L can't be checked here, so this fails whatever the other part says"},
			"S10":   {true, Match, "at least 10"},
		}},
		{"A&!L", session, true, Match, map[string]want{
			"A": {true, Match, "has a terminal"},
			"L": {false, NoMatch, "not local"},
		}},
		{"T30", nil, true, Match, map[string]want{"T30": {true, Match, "taken to have just started: minutes left 60 is at least 30"}}},
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
		// Comparisons, ranges and names, described from the same values.
		{"LEVEL<50&S10-30", nil, true, Match, map[string]want{
			"LEVEL<50": {true, Match, "level 20 is below 50"},
			"S10-30":   {true, Match, "level 20 is between 10 and 30"},
		}},
		{"S>20|USER=3", nil, false, NoMatch, map[string]want{
			"S>20":   {false, NoMatch, "level 20 is not above 20"},
			"USER=3": {false, NoMatch, "user ID is 5, not 3"},
		}},
		{"H22-6&HOUR=8-17", nil, false, NoMatch, map[string]want{
			"H22-6":     {true, Match, "the hour 22 is inside 22-6, which runs past the top"},
			"HOUR=8-17": {false, NoMatch, "the hour 22 is not between 8 and 17"},
		}},
		{"W1-5&WEEKDAY=3", nil, true, Match, map[string]want{
			"W1-5":      {true, Match, "today is 3 (Wednesday): 3 is between 1 and 5"},
			"WEEKDAY=3": {true, Match, "today is 3 (Wednesday)"},
		}},
		{"UPLOADSPERCALL<=20|ULDLRATIO>60", nil, false, NoMatch, map[string]want{
			"UPLOADSPERCALL<=20": {false, NoMatch, "3 uploads in 10 calls: 30% is above 20"},
			"ULDLRATIO>60":       {false, NoMatch, "3 uploads per 6 downloads: 50% is not above 60"},
		}},
		{"TIMELEFT<30", session, true, Match, map[string]want{"TIMELEFT<30": {true, Match, "minutes left 10 is below 30 (limit 60)"}}},
		{"FLAG:C&NOTE:caller&TIME:21:00/23:00&TERMINAL", session, true, Match, map[string]want{
			"FLAG:C":           {true, Match, "has flag C"},
			"NOTE:caller":      {true, Match, `contains "caller"`},
			"TIME:21:00/23:00": {true, Match, "22:00 is inside 21:00-23:00"},
			"TERMINAL":         {true, Match, "has a terminal"},
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
		{"T30|H25-3", true, false, Invalid},
		// Any problem fails the string, and ! can't make it pass.
		{"!Q", true, false, Invalid},
		{"S10|!Q", true, false, Invalid},
		{"FAB|S10", true, false, Invalid},
		{"S10@", true, false, Invalid},
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
	if len(x.Diagnostics) != 1 || x.Diagnostics[0].Column() != 4 {
		t.Errorf("Diagnostics = %v, want the @ at column 4", x.Diagnostics)
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
		{"S50&(FA|!FB)", Tester{At: at22}, `User:       Bob (#5), level 20
Session:    none: L or A fails the string, T as if the call just started
Time:       Wed 2026-11-04 22:00
Expression: S50&(FA|!FB)
            ---  ++  --
Result:     FAIL

  FAIL            S50&(FA|!FB)  both parts must pass: S50 failed
  FAIL              S50         level 20 is below 50
  PASS              FA|!FB      either part may pass: both passed
  PASS                FA        has flag A (flags: AC)
  PASS                !FB       FB failed, so this passes
  FAIL                  FB      doesn't have flag B (flags: AC)

A pass means this expression passed for this user in this context. It doesn't
mean every access check on a resource passes: a conference, area or door can
have rules of its own.`},
		{"T30|H25-3", Tester{At: at22}, `User:       Bob (#5), level 20
Session:    none: L or A fails the string, T as if the call just started
Time:       Wed 2026-11-04 22:00
Expression: T30|H25-3
                XXXXX
Result:     FAIL: the string has a problem

the string has a problem, so it fails for everyone and '!' can't make it pass

  error at column 5: "H25-3": H needs an hour from 0 to 23

A pass means this expression passed for this user in this context. It doesn't
mean every access check on a resource passes: a conference, area or door can
have rules of its own.`},
		{"T30&A|S10", Tester{At: at22, Session: &TesterSession{Local: true, Elapsed: 40 * time.Minute}}, `User:       Bob (#5), level 20
Session:    local call, no terminal, 40 min in, 0 min chat credit
Time:       Wed 2026-11-04 22:00
Expression: T30&A|S10
            --- - +++
Result:     PASS

  PASS            T30&A|S10  either part may pass: S10 passed
  FAIL              T30&A    both parts must pass: both failed
  FAIL                T30    minutes left 20 is below 30 (limit 60)
  FAIL                A      the connection has no terminal
  PASS              S10      level 20 is at least 10

A pass means this expression passed for this user in this context. It doesn't
mean every access check on a resource passes: a conference, area or door can
have rules of its own.`},
		{"S10|!L", Tester{At: at22}, `User:       Bob (#5), level 20
Session:    none: L or A fails the string, T as if the call just started
Time:       Wed 2026-11-04 22:00
Expression: S10|!L
            +++  ~
Result:     FAIL: a part needs a session the context doesn't have

  FAIL no session S10|!L  !L can't be checked here, so this fails whatever the
                          other part says
  PASS              S10   level 20 is at least 10
  FAIL no session   !L    L can't be checked here, so this fails too
  FAIL no session     L   needs a session and there is none, so the whole
                          string fails

A pass means this expression passed for this user in this context. It doesn't
mean every access check on a resource passes: a conference, area or door can
have rules of its own.`},
	} {
		if got := render(tc.acs, tc.tester); got != tc.want {
			t.Errorf("Render(%q):\n%s\n\nwant:\n%s", tc.acs, got, tc.want)
		}
	}

	// Every line fits the width asked for, including a detail that quotes a
	// sub-expression longer than the room, which has no spaces to break at.
	for _, expr := range []string{
		"(S10&FA&FC&V&!FX&E30&B50)|(Ztrusted&C2&X3&U5&!L&T30&H22&W3&Y20:00/23:00)",
		"(FB&FA&FC&FD&FE&FF&FG&FH&FI&FJ&FK&FL&FM&FN&FO)&S10",
	} {
		x := Tester{At: at22}.Explain(expr, explainUser())
		for _, l := range Render(x, 40) {
			if len([]rune(l)) > 40 {
				t.Errorf("%s: line wider than 40: %q", expr, l)
			}
		}
	}
}

// TestWrapSplitsLongWords pins wrap on a word longer than the room: it's
// cut at the room, and the pieces carry on the indented lines.
func TestWrapSplitsLongWords(t *testing.T) {
	got := wrap("  x ", "must pass: S10&FA&FB&FC&FD&FE&FF&FG&FH&FI&FJ&FK failed", 30)
	want := []string{
		"  x must pass:",
		"    S10&FA&FB&FC&FD&FE&FF&FG&F",
		"    H&FI&FJ&FK failed",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("wrap:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
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
