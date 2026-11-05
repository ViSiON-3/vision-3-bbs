package menu

import (
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/acs"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// TestUserEditor_ACSTester runs the tester from the editor: the selected
// user's record is checked with no session and the current time, both on
// screen, never with the sysop's own session. The sysop calls in from
// loopback, so L would pass if their session were used.
func TestUserEditor_ACSTester(t *testing.T) {
	env := newMenuEnv(t)
	input := "j" + "t" + "S50&(V|!Q)\r" + "e" + strings.Repeat(ueBS, 10) + "L\r" + "q" + "q"
	r := env.runFrom("127.0.0.1", env.e.RunRegistry["ADMINLISTUSERS"], env.sysop, input)
	if r.err != nil {
		t.Fatalf("err = %v", r.err)
	}
	out := r.text()
	for _, want := range []string{
		"[T] ACS Test",
		"ACS Test / Caller",
		"User:       Caller (#2), level 10",
		"Session:    none: L and A fail",
		"(now)",
		"Expression: S50&(V|!Q)",
		"level 10 is below 50",
		"Q is not an ACS term",
		"Expression: L",
		"needs a session and there is none",
		"A pass means this expression passed for this user",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "the connection is local") {
		t.Errorf("the tester used the sysop's session:\n%s", out)
	}
}

// TestUserEditor_ACSTesterSessionAndTime sets a made-up session and a time.
func TestUserEditor_ACSTesterSessionAndTime(t *testing.T) {
	env := newMenuEnv(t)
	// Session: yes; local: yes; terminal: no; 15 minutes in; no credit.
	input := "j" + "t" + "H22&L&!A\r" + "s" + "y" + "y" + "n" + ueBS + "15\r" + "\r" + "t" + "22:30\r" + "q" + "q"
	r := env.runCmd("ADMINLISTUSERS", env.sysop, "", input)
	if r.err != nil {
		t.Fatalf("err = %v", r.err)
	}
	if !r.has("local call, no terminal, 15 min in, 0 min chat credit", "22:30", "Result:     PASS") {
		t.Errorf("output:\n%s", r.text())
	}
}

// TestUserEditor_ACSTesterSessionKeptAndReasked pins the session dialogue's
// defaults: with a session set, S and Enter at each prompt keep it as it
// was, and a bad minutes answer asks just that prompt again, keeping the
// answers before it.
func TestUserEditor_ACSTesterSessionKeptAndReasked(t *testing.T) {
	env := newMenuEnv(t)
	set := "s" + "y" + "y" + "n" + ueBS + "15\r" + "\r"
	keep := "s" + "\r" + "\r" + "\r" + "\r" + "\r"
	retry := "s" + "\r" + "\r" + "\r" + ueBS + ueBS + "x\r" + ueBS + ueBS + "20\r" + "\r"
	input := "j" + "t" + "L&!A\r" + set + keep + retry + "q" + "q"
	r := env.runCmd("ADMINLISTUSERS", env.sysop, "", input)
	if r.err != nil {
		t.Fatalf("err = %v", r.err)
	}
	out := r.text()
	kept := strings.Index(out, "local call, no terminal, 15 min in")
	if kept < 0 || !strings.Contains(out[kept+1:], "local call, no terminal, 15 min in") {
		t.Errorf("Enter through the prompts should keep the session:\n%s", out)
	}
	if !r.has("Enter a whole number of minutes.", "local call, no terminal, 20 min in", "Result:     PASS") {
		t.Errorf("a bad minutes answer should re-ask only that prompt:\n%s", out)
	}
}

// TestUserEditor_ACSTesterGate: the tester is for the SYSOP keyword's level
// only. A caller below it can't open the editor, and an editor opened some
// other way still refuses the key without drawing anything.
func TestUserEditor_ACSTesterGate(t *testing.T) {
	env := newMenuEnv(t)
	r := env.runCmd("ADMINLISTUSERS", env.caller, "", "tS10\rqq")
	if !r.has("Access denied.") || r.has("ACS Test") {
		t.Errorf("a caller reached the tester:\n%s", r.text())
	}

	coSysOp := &user.User{ID: 3, Handle: "Deputy", AccessLevel: env.e.GetServerConfig().CoSysOpLevel}
	st := &userEditorState{
		e:              env.e,
		currentUser:    coSysOp,
		users:          []*user.User{env.caller},
		pendingChanges: map[string]interface{}{},
		// No terminal: drawing the tester would panic.
	}
	refresh, exit, _, _, err := st.handleEditorKey('t', 80, 24)
	if err != nil || exit || refresh {
		t.Fatalf("refresh %v exit %v err %v", refresh, exit, err)
	}
	if st.statusMessage != env.e.Strings().AcsTestDenied {
		t.Errorf("status = %q, want the denial", st.statusMessage)
	}
	if st.acsTesterAllowed() {
		t.Error("a co-sysop is allowed the tester")
	}
	st.currentUser = env.sysop
	if !st.acsTesterAllowed() {
		t.Error("the sysop is refused the tester")
	}
}

// TestUserEditor_ACSTesterPages checks that a long explanation stops for a
// More prompt within the terminal's height.
func TestUserEditor_ACSTesterPages(t *testing.T) {
	env := newMenuEnv(t)
	long := "S1&S2&S3&S4&S5&S6&S7&S8&S9&S10&S11&S12&S13&S14&S15"
	r := env.runCmd("ADMINLISTUSERS", env.sysop, "", "j"+"t"+long+"\r"+"\r"+"q"+"q")
	if r.err != nil {
		t.Fatalf("err = %v", r.err)
	}
	out := r.text()
	more := stripPipe(env.e.Strings().FileMorePrompt)
	i := strings.Index(out, more)
	if i < 0 {
		t.Fatalf("no More prompt:\n%s", out)
	}
	start := strings.LastIndex(out[:i], "ACS Test / Caller")
	if n := strings.Count(out[start:i], "\n"); n > 23 {
		t.Errorf("%d lines before the More prompt on a 24-row terminal", n)
	}
	if lines := acs.Render(acs.Tester{}.Explain(long, env.caller), 79); len(lines) < 24 {
		t.Fatalf("explanation is %d lines, too short to page", len(lines))
	}
}

// stripPipe removes |nn colour codes.
func stripPipe(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '|' && i+2 < len(s) && s[i+1] >= '0' && s[i+1] <= '9' {
			i += 2
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}
