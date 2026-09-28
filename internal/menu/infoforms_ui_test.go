package menu

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// writeInfoformTemplate installs form formNum's template in env's infoforms
// data directory (configs/../data/infoforms, i.e. env.dataDir()/infoforms).
func writeInfoformTemplate(t *testing.T, env *menuEnv, formNum int, body string) {
	t.Helper()
	p := infoformsTemplatePath(env.cfgDir(), formNum)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// writeInfoformConfig installs an infoforms config.json for env.
func writeInfoformConfig(t *testing.T, env *menuEnv, cfg InfoFormConfig) {
	t.Helper()
	b, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	p := infoformsConfigPath(env.cfgDir())
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, b, 0o644); err != nil {
		t.Fatal(err)
	}
}

// saveInfoformAnswers stores a completed form for u directly, as a prior
// session would have.
func saveInfoformAnswers(t *testing.T, env *menuEnv, u *user.User, formNum int, answers ...string) {
	t.Helper()
	if err := saveInfoFormResponse(env.cfgDir(), &InfoFormResponse{
		UserID: u.ID, Handle: u.Handle, FormNum: formNum,
		FilledOutAt: time.Date(2026, 5, 6, 14, 30, 0, 0, time.UTC), Answers: answers,
	}); err != nil {
		t.Fatalf("saveInfoFormResponse: %v", err)
	}
}

// savedInfoformAnswers loads u's stored answers for formNum, or nil.
func savedInfoformAnswers(t *testing.T, env *menuEnv, u *user.User, formNum int) []string {
	t.Helper()
	resp, err := loadInfoFormResponse(env.cfgDir(), u.ID, formNum)
	if err != nil {
		t.Fatalf("loadInfoFormResponse: %v", err)
	}
	if resp == nil {
		return nil
	}
	return resp.Answers
}

// TestInfoFormsFillSavesAnswers pins the fill path from the INFOFORMS menu:
// each * field's answer is saved in order, a |B limit truncates its field,
// and the relisted menu shows the form as completed.
func TestInfoFormsFillSavesAnswers(t *testing.T) {
	env := newMenuEnv(t)
	writeInfoformTemplate(t, env, 1, "Name: *\r\n|B4;City: *\r\nThanks, |VN!")

	r := env.runCmd("INFOFORMS", env.caller, "", "1\rCarl\rParisian\rQ\r")
	if r.err != nil || r.user != env.caller {
		t.Fatalf("result = (%v, %v)", r.user, r.err)
	}
	got := savedInfoformAnswers(t, env, env.caller, 1)
	if strings.Join(got, ",") != "Carl,Pari" {
		t.Errorf("answers = %q, want [Carl Pari] (City capped at 4)", got)
	}
	if !r.has("New User Application", "Optional", "Incomplete!", "Name: ", "City: ", "Form completed!", "Completed..") {
		t.Errorf("menu/fill output missing expected text:\n%s", r.text())
	}
	if r.has("|VN") {
		t.Errorf("|VN was not expanded in the trailing text:\n%s", r.text())
	}
}

// TestInfoFormsRequiredFieldReprompts pins that a *! field will not take a
// blank answer: the caller is told and asked again.
func TestInfoFormsRequiredFieldReprompts(t *testing.T) {
	env := newMenuEnv(t)
	writeInfoformTemplate(t, env, 1, "Must: *!")

	r := env.runCmd("INFOFORMS", env.caller, "", "1\r  \rfilled\rQ\r")
	if !r.has("This field is required.") {
		t.Errorf("blank required answer not refused:\n%s", r.text())
	}
	if got := savedInfoformAnswers(t, env, env.caller, 1); len(got) != 1 || got[0] != "filled" {
		t.Errorf("answers = %q, want [filled]", got)
	}
}

// TestInfoFormsRefillAsksBeforeReplacing pins that refilling a completed
// form asks first: No keeps the old answers, Yes replaces them.
func TestInfoFormsRefillAsksBeforeReplacing(t *testing.T) {
	env := newMenuEnv(t)
	writeInfoformTemplate(t, env, 1, "Q: *")
	saveInfoformAnswers(t, env, env.caller, 1, "old")

	r := env.runCmd("INFOFORMS", env.caller, "", "1\rN\rQ\r")
	if !r.has("You have already filled out form #1! Replace it?") {
		t.Errorf("missing replace prompt:\n%s", r.text())
	}
	if got := savedInfoformAnswers(t, env, env.caller, 1); len(got) != 1 || got[0] != "old" {
		t.Errorf("after No: answers = %q, want [old]", got)
	}

	env.runCmd("INFOFORMS", env.caller, "", "1\rYnew\rQ\r")
	if got := savedInfoformAnswers(t, env, env.caller, 1); len(got) != 1 || got[0] != "new" {
		t.Errorf("after Yes: answers = %q, want [new]", got)
	}
}

// TestInfoFormsDisconnectMidFormSavesNothing pins that a caller who drops
// out partway through a form leaves no partial response behind.
func TestInfoFormsDisconnectMidFormSavesNothing(t *testing.T) {
	env := newMenuEnv(t)
	writeInfoformTemplate(t, env, 1, "A: * B: *")

	env.runCmd("INFOFORMS", env.caller, "", "1\ronly-one\r")
	if hasCompletedForm(env.cfgDir(), env.caller.ID, 1) {
		t.Error("partial form was saved")
	}
}

// TestInfoFormsViewShowsAnswers pins the V command: the stored answers are
// replayed into the template, blanks read "No answer", and pipe codes typed
// into an answer are shown literally rather than interpreted.
func TestInfoFormsViewShowsAnswers(t *testing.T) {
	env := newMenuEnv(t)
	writeInfoformTemplate(t, env, 1, "Name: *\r\nAge: *\r\nPet: *\r\nEnd.")
	saveInfoformAnswers(t, env, env.caller, 1, "|12Carl", "")

	r := env.runCmd("INFOFORMS", env.caller, "", "V\r1\r\rQ\r")
	if !r.has("Filled out on: 05/06/2026 at 02:30 PM", "Name: |12Carl", "Age: No answer", "Pet: No answer", "End.") {
		t.Errorf("view output wrong:\n%s", r.text())
	}
}

// TestInfoFormsViewRejectsBadNumbers pins the V command's validation of
// out-of-range and missing forms.
func TestInfoFormsViewRejectsBadNumbers(t *testing.T) {
	env := newMenuEnv(t)
	writeInfoformTemplate(t, env, 1, "Q: *")

	r := env.runCmd("INFOFORMS", env.caller, "", "V\r9\rV\r3\rQ\r")
	if !r.has("Invalid form number.", "That form doesn't exist.") {
		t.Errorf("want both refusals:\n%s", r.text())
	}
}

// TestShowInfoFormPagesAndStops pins showInfoForm's More prompt: a response
// longer than a screen pauses, and Q there stops the replay.
func TestShowInfoFormPagesAndStops(t *testing.T) {
	env := newMenuEnv(t)
	var body strings.Builder
	answers := make([]string, 40)
	for i := range answers {
		fmt.Fprintf(&body, "Line %02d: *\r\n", i)
		answers[i] = fmt.Sprintf("ans%02d", i)
	}
	writeInfoformTemplate(t, env, 1, body.String())
	saveInfoformAnswers(t, env, env.caller, 1, answers...)

	r := env.runCmd("INFOFORMVIEW", env.caller, "", "1\rq")
	if !r.has("MORE", "Line 05: ans05") {
		t.Errorf("want the first page and a More prompt:\n%s", r.text())
	}
	if r.has("Line 39") {
		t.Errorf("Q at More should stop before the last line:\n%s", r.text())
	}

	r = env.runCmd("INFOFORMVIEW", env.caller, "", "1\r \r\r")
	if !r.has("Line 39: ans39") {
		t.Errorf("continuing at More should reach the last line:\n%s", r.text())
	}
}

// TestShowInfoFormMissingPieces pins showInfoForm's fallbacks: no response on
// file, and a response whose template has since been removed.
func TestShowInfoFormMissingPieces(t *testing.T) {
	env := newMenuEnv(t)

	r := env.runCmd("INFOFORMVIEW", env.caller, "", "2\r\r")
	if !r.has("That user has no information form.") {
		t.Errorf("want the no-form notice:\n%s", r.text())
	}

	saveInfoformAnswers(t, env, env.caller, 2, "orphan")
	r = env.runCmd("INFOFORMVIEW", env.caller, "", "2\r\r")
	if !r.has("Infoform #2 is blank.") {
		t.Errorf("want the blank-template notice:\n%s", r.text())
	}
}

// runBrowseInfoForms adapts browseInfoForms (used by the user editors) to a
// RunnableFunc so the harness can drive it, browsing sel's forms.
func runBrowseInfoForms(sel *user.User, cfg *InfoFormConfig) RunnableFunc {
	return func(c *cmdCtx, args string) (*user.User, string, error) {
		err := browseInfoForms(c.e, c.s, c.terminal, c.outputMode, sel, cfg, c.termWidth, c.termHeight)
		return c.currentUser, "", err
	}
}

// TestBrowseInfoFormsShowsStatusAndAnswers pins the sysop form browser: each
// installed form is listed with its completion status, a completed one opens
// to its answers, an incomplete one says so, and Q returns.
func TestBrowseInfoFormsShowsStatusAndAnswers(t *testing.T) {
	env := newMenuEnv(t)
	writeInfoformTemplate(t, env, 1, "Handle: *")
	writeInfoformTemplate(t, env, 3, "Other: *")
	saveInfoformAnswers(t, env, env.caller, 1, "carl-answer")
	cfg := &InfoFormConfig{Descriptions: [5]string{"Application"}}

	r := env.run(runBrowseInfoForms(env.caller, cfg), env.sysop, "", "1\r3\r4q")
	if r.err != nil {
		t.Fatalf("err = %v", r.err)
	}
	if !r.has("InfoForms for Caller", "Application", "[Completed]", "Form #3", "[Incomplete]") {
		t.Errorf("listing wrong:\n%s", r.text())
	}
	if !r.has("Handle: carl-answer", "This form has not been completed.") {
		t.Errorf("viewing forms 1 and 3 wrong:\n%s", r.text())
	}
}

// TestBrowseInfoFormsNoTemplates pins the browser's early return when no
// form templates are installed, and its EOF report on a dropped session.
func TestBrowseInfoFormsNoTemplates(t *testing.T) {
	env := newMenuEnv(t)
	cfg := &InfoFormConfig{}

	r := env.run(runBrowseInfoForms(env.caller, cfg), env.sysop, "", "\r")
	if !r.has("No infoform templates configured.") {
		t.Errorf("want the no-templates notice:\n%s", r.text())
	}

	writeInfoformTemplate(t, env, 1, "Q: *")
	r = env.run(runBrowseInfoForms(env.caller, cfg), env.sysop, "", "")
	if r.err != nil || !r.has("Form #1", "[Incomplete]") {
		t.Errorf("want the listing then a clean EOF return: err=%v\n%s", r.err, r.text())
	}
}
