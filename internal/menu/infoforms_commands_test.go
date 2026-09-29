package menu

import (
	"testing"
)

// TestInfoFormViewRejectsBadNumber pins INFOFORMVIEW's input validation.
func TestInfoFormViewRejectsBadNumber(t *testing.T) {
	env := newMenuEnv(t)
	r := env.runCmd("INFOFORMVIEW", env.caller, "", "six\r")
	if !r.has("Invalid form number.") {
		t.Errorf("want the invalid-number notice:\n%s", r.text())
	}
}

// TestInfoFormHuntSysopOnly pins that INFOFORMHUNT refuses non-sysops.
func TestInfoFormHuntSysopOnly(t *testing.T) {
	env := newMenuEnv(t)
	writeInfoformTemplate(t, env, 1, "Q: *")
	saveInfoformAnswers(t, env, env.sysop, 1, "sysop-secret")

	r := env.runCmd("INFOFORMHUNT", env.caller, "", "1\r\r")
	if !r.has("Access denied.") || r.has("sysop-secret") {
		t.Errorf("caller should be refused before any form is shown:\n%s", r.text())
	}
}

// TestInfoFormHuntListsEveryUsersResponse pins that INFOFORMHUNT shows each
// user's answers to the chosen form under their current handle, and ignores
// responses to other forms.
func TestInfoFormHuntListsEveryUsersResponse(t *testing.T) {
	env := newMenuEnv(t)
	writeInfoformTemplate(t, env, 1, "Why: *")
	writeInfoformTemplate(t, env, 2, "Other: *")
	saveInfoformAnswers(t, env, env.sysop, 1, "because-sysop")
	saveInfoformAnswers(t, env, env.caller, 1, "because-caller")
	saveInfoformAnswers(t, env, env.caller, 2, "form-two-only")

	r := env.runCmd("INFOFORMHUNT", env.sysop, "", "1\r\r")
	if r.err != nil {
		t.Fatalf("err = %v", r.err)
	}
	if !r.has("Showing All Forms #1", "Sysop", "Why: because-sysop", "Caller", "Why: because-caller") {
		t.Errorf("hunt output missing a response:\n%s", r.text())
	}
	if r.has("form-two-only") {
		t.Errorf("hunt for form 1 showed a form 2 answer:\n%s", r.text())
	}
}

// TestInfoFormHuntEmptyAndInvalid pins INFOFORMHUNT's refusals and empty
// results: bad number, missing template, no responses directory, and a
// responses directory with nothing for the chosen form.
func TestInfoFormHuntEmptyAndInvalid(t *testing.T) {
	env := newMenuEnv(t)

	for _, tc := range []struct{ input, want string }{
		{"0\r", "Invalid form number."},
		{"4\r", "That form template doesn't exist."},
	} {
		if r := env.runCmd("INFOFORMHUNT", env.sysop, "", tc.input); !r.has(tc.want) {
			t.Errorf("input %q: want %q:\n%s", tc.input, tc.want, r.text())
		}
	}

	writeInfoformTemplate(t, env, 1, "Q: *")
	if r := env.runCmd("INFOFORMHUNT", env.sysop, "", "1\r"); !r.has("No responses found.") {
		t.Errorf("no responses dir: want the empty notice:\n%s", r.text())
	}

	writeInfoformTemplate(t, env, 2, "Q: *")
	saveInfoformAnswers(t, env, env.caller, 2, "x")
	if r := env.runCmd("INFOFORMHUNT", env.sysop, "", "1\r\r"); !r.has("No responses found.") {
		t.Errorf("only other forms answered: want the empty notice:\n%s", r.text())
	}
}

// TestInfoFormRequiredForcesUnvalidatedUser pins INFOFORMREQUIRED on a new
// (unvalidated) user: each required, installed, unanswered form is filled in
// turn and saved; optional and already-answered forms are skipped.
func TestInfoFormRequiredForcesUnvalidatedUser(t *testing.T) {
	env := newMenuEnv(t)
	env.caller.Validated = false
	writeInfoformConfig(t, env, InfoFormConfig{RequiredForms: "13"})
	writeInfoformTemplate(t, env, 1, "Real name: *!")
	writeInfoformTemplate(t, env, 2, "Optional: *")
	writeInfoformTemplate(t, env, 3, "Already: *")
	saveInfoformAnswers(t, env, env.caller, 3, "done-before")

	r := env.runCmd("INFOFORMREQUIRED", env.caller, "", "Carl Caller\r")
	if r.err != nil || r.next != "" {
		t.Fatalf("result = (%q, %v), want (\"\", nil)", r.next, r.err)
	}
	if got := savedInfoformAnswers(t, env, env.caller, 1); len(got) != 1 || got[0] != "Carl Caller" {
		t.Errorf("form 1 answers = %q, want [Carl Caller]", got)
	}
	if hasCompletedForm(env.dataDir(), env.caller.ID, 2) {
		t.Error("optional form 2 was forced")
	}
	if r.has("Already:") {
		t.Errorf("already-answered form 3 was shown again:\n%s", r.text())
	}
}

// TestInfoFormRequiredLogsOffOnIncompleteForm pins that a new user who drops
// out of a required form is logged off rather than let in.
func TestInfoFormRequiredLogsOffOnIncompleteForm(t *testing.T) {
	env := newMenuEnv(t)
	env.caller.Validated = false
	writeInfoformConfig(t, env, InfoFormConfig{RequiredForms: "1"})
	writeInfoformTemplate(t, env, 1, "First: * Second: *")

	r := env.runCmd("INFOFORMREQUIRED", env.caller, "", "only-first\r")
	if r.next != "LOGOFF" {
		t.Errorf("next = %q, want LOGOFF", r.next)
	}
	if !r.has("Required form #1 was not completed. Disconnecting.") {
		t.Errorf("missing disconnect notice:\n%s", r.text())
	}
	if hasCompletedForm(env.dataDir(), env.caller.ID, 1) {
		t.Error("partial required form was saved")
	}
}

// TestInfoFormNukeErasesAllForms pins INFOFORMNUKE: after confirming, every
// stored form for the named user is deleted and other users' are kept.
func TestInfoFormNukeErasesAllForms(t *testing.T) {
	env := newMenuEnv(t)
	saveInfoformAnswers(t, env, env.caller, 1, "a")
	saveInfoformAnswers(t, env, env.caller, 5, "e")
	saveInfoformAnswers(t, env, env.sysop, 1, "keep")

	r := env.runCmd("INFOFORMNUKE", env.sysop, "", "caller\rY")
	if !r.has("Erase ALL info-forms for Caller", "All infoforms deleted.") {
		t.Errorf("missing confirm/done text:\n%s", r.text())
	}
	for _, n := range []int{1, 5} {
		if hasCompletedForm(env.dataDir(), env.caller.ID, n) {
			t.Errorf("caller form %d survived the nuke", n)
		}
	}
	if !hasCompletedForm(env.dataDir(), env.sysop.ID, 1) {
		t.Error("nuke removed another user's form")
	}
}

// TestInfoFormNukeRefusals pins that INFOFORMNUKE deletes nothing when the
// user is not a sysop, the handle is unknown or blank, or the sysop declines.
func TestInfoFormNukeRefusals(t *testing.T) {
	env := newMenuEnv(t)
	saveInfoformAnswers(t, env, env.caller, 1, "a")

	cases := []struct {
		name  string
		as    bool // true = sysop
		input string
		want  string
	}{
		{"non-sysop", false, "caller\rY", "Access denied."},
		{"unknown handle", true, "nobody\r", "User not found."},
		{"blank handle", true, "\r", "Handle to nuke infoforms for:"},
		{"declined", true, "caller\rN", "Are you sure?"},
	}
	for _, tc := range cases {
		u := env.caller
		if tc.as {
			u = env.sysop
		}
		r := env.runCmd("INFOFORMNUKE", u, "", tc.input)
		if !r.has(tc.want) {
			t.Errorf("%s: want %q:\n%s", tc.name, tc.want, r.text())
		}
		if !hasCompletedForm(env.dataDir(), env.caller.ID, 1) {
			t.Fatalf("%s: form deleted", tc.name)
		}
	}
}
