package menu

import (
	"slices"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// newUserFormInput scripts a complete signup after the caller has reached the
// form: accept "Apply for access?", dismiss NEWUSER.ANS, then handle, password
// twice, real name, note, location, and the closing pause.
func newUserFormInput(handle, password, realName, note, location string) string {
	return "Y" + "\r" + handle + "\r" + password + "\r" + password + "\r" +
		realName + "\r" + note + "\r" + location + "\r" + "\r"
}

// mustGetUser reloads a user from disk-backed state and fails if absent.
func mustGetUser(t *testing.T, env *menuEnv, handle string) *user.User {
	t.Helper()
	u, ok := env.um.GetUser(handle)
	if !ok {
		t.Fatalf("user %q was not created", handle)
	}
	return u
}

// A completed signup creates the account with the configured new-user level
// and the details entered, joins the auto-join areas, and — when that level
// can log on but the account is not auto-validated — says it can log on now
// and that a SysOp will review it. NEWUSER from a menu does not switch the
// session to the new account.
func TestNewUser_CompletedSignupCreatesAccount(t *testing.T) {
	env := newMenuEnv(t)
	env.um.SetNewUserLevel(10)
	env.um.SetAutoValidateNewUsers(false)

	r := env.runCmd("NEWUSER", nil, "", newUserFormInput("Newbie", "pw123", "Nora Newbie", "hi there", "Springfield"))
	if r.err != nil || r.next != "" || r.user != nil {
		t.Fatalf("user=%v next=%q err=%v, want no session change", r.user, r.next, r.err)
	}
	u := mustGetUser(t, env, "Newbie")
	if u.AccessLevel != 10 || u.Validated {
		t.Errorf("level=%d validated=%v, want 10 / false", u.AccessLevel, u.Validated)
	}
	if u.RealName != "Nora Newbie" || u.GroupLocation != "Springfield" || u.PrivateNote != "hi there" {
		t.Errorf("details = %q / %q / %q", u.RealName, u.GroupLocation, u.PrivateNote)
	}
	if u.CreatedAt.IsZero() {
		t.Error("CreatedAt not set")
	}
	if u.ID != 3 || !r.has("Your User # is 3") {
		t.Errorf("ID = %d, want 3 and announced", u.ID)
	}
	var want []string
	for _, a := range env.e.MessageMgr.ListAreas() {
		if a.AutoJoin {
			want = append(want, a.Tag)
		}
	}
	if len(want) == 0 {
		t.Fatal("shipped config has no auto_join areas; test needs one")
	}
	if !slices.Equal(u.TaggedMessageAreaTags, want) {
		t.Errorf("tagged areas = %v, want auto-join %v", u.TaggedMessageAreaTags, want)
	}
	if _, ok := env.um.Authenticate("Newbie", "pw123"); !ok {
		t.Error("new account does not authenticate with its password")
	}
	if !r.has("You can log on now.", "A SysOp will review your account.") {
		t.Errorf("wrong closing message: %q", r.text())
	}
}

// With autoValidateNewUsers the account is created validated and the caller is
// not told a review is pending.
func TestNewUser_AutoValidate(t *testing.T) {
	env := newMenuEnv(t)
	env.um.SetNewUserLevel(10)
	env.um.SetAutoValidateNewUsers(true)

	env.runCmd("NEWUSER", nil, "", newUserFormInput("Valida", "pw123", "Val Ida", "", "Here"))
	r := env.runCmd("NEWUSER", nil, "", newUserFormInput("Validb", "pw123", "Val Idb", "", "Here"))
	if u := mustGetUser(t, env, "Validb"); !u.Validated {
		t.Error("auto-validated signup not marked validated")
	}
	if r.has("A SysOp will review") {
		t.Errorf("auto-validated signup told a review is pending")
	}
}

// When the new-user level is below logonLevel the caller is told a SysOp must
// raise their access before they can log on.
func TestNewUser_BelowLogonLevelToldToWait(t *testing.T) {
	env := newMenuEnv(t)
	env.um.SetNewUserLevel(5)
	setServerField(env.e, func(c *config.ServerConfig) { c.LogonLevel = 10 })

	r := env.runCmd("NEWUSER", nil, "", newUserFormInput("Waiter", "pw123", "Wait Er", "", "Here"))
	if u := mustGetUser(t, env, "Waiter"); u.AccessLevel != 5 {
		t.Errorf("level = %d, want 5", u.AccessLevel)
	}
	if !r.has("SysOp must raise your access") || r.has("You can log on now.") {
		t.Errorf("wrong closing message: %q", r.text())
	}
}

// With allowNewUsers off the form never starts and no account is created.
func TestNewUser_ClosedToNewUsers(t *testing.T) {
	env := newMenuEnv(t)
	setServerField(env.e, func(c *config.ServerConfig) { c.AllowNewUsers = false })

	r := env.runCmd("NEWUSER", nil, "", newUserFormInput("Sneaky", "pw123", "Snea Ky", "", "Here"))
	if !r.has("not accepting new users") {
		t.Errorf("no closed message: %q", r.text())
	}
	if _, ok := env.um.GetUser("Sneaky"); ok {
		t.Error("account created while new users are closed")
	}
	if r.has("Apply") {
		t.Error("apply prompt shown while closed")
	}
}

// Answering No to "Apply for access?" ends signup with nothing created.
func TestNewUser_DeclineApply(t *testing.T) {
	env := newMenuEnv(t)
	before := env.um.NextUserID()
	r := env.runCmd("NEWUSER", nil, "", "N")
	if r.err != nil || r.next != "" {
		t.Fatalf("next=%q err=%v", r.next, r.err)
	}
	if env.um.NextUserID() != before {
		t.Error("declining created an account")
	}
}

// A handle already in use (in any case) or failing the handle rules is
// refused and re-asked; the next acceptable handle is used.
func TestNewUser_RejectsDuplicateAndInvalidHandles(t *testing.T) {
	t.Parallel()
	env := newMenuEnv(t)
	env.um.SetNewUserLevel(10)

	in := "Y\r" + "cAlLeR\r" + "new\r" + "Fresh\r" + "pw123\rpw123\r" + "Fresh Face\r\rHere\r\r"
	r := env.runCmd("NEWUSER", nil, "", in)
	if !r.has("Name is already in use!", "Invalid Name .. Try again!") {
		t.Errorf("missing rejection messages: %q", r.text())
	}
	if u := mustGetUser(t, env, "Caller"); u.ID != 2 || u.RealName != "Carl Caller" {
		t.Errorf("existing Caller account changed: %+v", u)
	}
	if _, ok := env.um.GetUser("new"); ok {
		t.Error("reserved handle 'new' was accepted")
	}
	mustGetUser(t, env, "Fresh")
}

// The password must be at least three characters and typed the same twice;
// the real name must be first and last. Each failure re-asks.
func TestNewUser_PasswordAndRealNameValidation(t *testing.T) {
	t.Parallel()
	env := newMenuEnv(t)
	env.um.SetNewUserLevel(10)

	in := "Y\r" + "Picky\r" +
		"ab\r" + // too short
		"abcd\rabce\r" + // mismatch
		"abcd\rabcd\r" +
		"Pat\r" + // no last name
		"Pat Picky\r" + "\rHere\r\r"
	r := env.runCmd("NEWUSER", nil, "", in)
	if !r.has("Password must be at least 3 characters.", "They don't match!", "first") {
		t.Errorf("missing validation messages: %q", r.text())
	}
	u := mustGetUser(t, env, "Picky")
	if u.RealName != "Pat Picky" {
		t.Errorf("real name = %q", u.RealName)
	}
	if _, ok := env.um.Authenticate("Picky", "abcd"); !ok {
		t.Error("password is not the confirmed one")
	}
}

// ESC in the form asks "Exit New User Signup?"; Yes leaves (logging off from
// a menu) with no account, No returns to the same field.
func TestNewUser_EscapeConfirmsExit(t *testing.T) {
	t.Parallel()
	env := newMenuEnv(t)
	env.um.SetNewUserLevel(10)

	r := env.runCmd("NEWUSER", nil, "", "Y\r\x1bY")
	if r.next != "LOGOFF" {
		t.Errorf("exit confirmed: next=%q, want LOGOFF", r.next)
	}
	if !r.has("Exit New User Signup?", "Maybe another time") {
		t.Errorf("missing exit prompt: %q", r.text())
	}

	// Staying: ESC then No at the handle prompt, then finish normally.
	r = env.runCmd("NEWUSER", nil, "", "Y\r\x1bN"+"Stayer\rpw123\rpw123\rStay Er\r\rHere\r\r")
	if r.next != "" || r.err != nil {
		t.Fatalf("next=%q err=%v", r.next, r.err)
	}
	mustGetUser(t, env, "Stayer")
}

// A blank handle asks "Cannot be empty. Retry?"; No ends signup.
func TestNewUser_BlankHandleDeclineRetry(t *testing.T) {
	t.Parallel()
	env := newMenuEnv(t)
	before := env.um.NextUserID()
	r := env.runCmd("NEWUSER", nil, "", "Y\r\rN")
	if !r.has("Cannot be empty.") || r.next != "LOGOFF" {
		t.Errorf("next=%q out=%q", r.next, r.text())
	}
	if env.um.NextUserID() != before {
		t.Error("account created from a blank handle")
	}
}

// With useNuv and autoAddNuv the new account is queued for new-user voting.
func TestNewUser_AutoAddsToNUVQueue(t *testing.T) {
	env := newMenuEnv(t)
	env.um.SetNewUserLevel(10)
	setServerField(env.e, func(c *config.ServerConfig) { c.UseNUV = true; c.AutoAddNUV = true })

	r := env.runCmd("NEWUSER", nil, "", newUserFormInput("Voteme", "pw123", "Vo Teme", "", "Here"))
	if !r.has("submitted for community review") {
		t.Errorf("no NUV message: %q", r.text())
	}
	nd, err := loadNUVData(env.dataDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(nd.Candidates) != 1 || nd.Candidates[0].Handle != "Voteme" {
		t.Errorf("NUV queue = %+v, want [Voteme]", nd.Candidates)
	}
}

// With requireNewUserEmail, dropping at the introduction gate leaves the
// account flagged as owing the introduction (so login resumes the gate), with
// one abandoned attempt recorded, and the session logs off.
func TestNewUser_RequiredIntroDisconnectLeavesPending(t *testing.T) {
	env := newMenuEnv(t)
	env.um.SetNewUserLevel(10)
	setServerField(env.e, func(c *config.ServerConfig) { c.RequireNewUserEmail = true })

	in := newUserFormInput("Shy", "pw123", "Shy Guy", "", "Here")
	r := env.runCmd("NEWUSER", nil, "", in[:len(in)-1]) // no final pause: drop in the gate
	if r.next != "LOGOFF" {
		t.Errorf("next = %q, want LOGOFF", r.next)
	}
	u := mustGetUser(t, env, "Shy")
	if !u.IntroPending || u.IntroAttempts != 1 {
		t.Errorf("IntroPending=%v IntroAttempts=%d, want true / 1", u.IntroPending, u.IntroAttempts)
	}
}

// ESC at any field, answered No to "Exit New User Signup?", and a blank
// password or real name answered Yes to "Retry?", all return to the same field
// without losing what was already entered.
func TestNewUser_StayAtEveryField(t *testing.T) {
	env := newMenuEnv(t)
	env.um.SetNewUserLevel(10)

	in := "Y\r" + "Keeper\r" +
		"\x1bN" + // ESC at password
		"\rY" + // blank password, retry
		"pw123\r\x03N" + // Ctrl+C at confirmation
		"pw123\rpw123\r" +
		"\x1bN" + "\rY" + "Kee Per\r" + // real name: ESC, blank, then valid
		"\x1bN" + "a note\r" + // note
		"\x1bN" + "Somewhere\r" + // location
		"\r"
	r := env.runCmd("NEWUSER", nil, "", in)
	if r.next != "" || r.err != nil {
		t.Fatalf("next=%q err=%v out=%q", r.next, r.err, r.text())
	}
	u := mustGetUser(t, env, "Keeper")
	if u.RealName != "Kee Per" || u.PrivateNote != "a note" || u.GroupLocation != "Somewhere" {
		t.Errorf("details = %q / %q / %q", u.RealName, u.PrivateNote, u.GroupLocation)
	}
	if _, ok := env.um.Authenticate("Keeper", "pw123"); !ok {
		t.Error("password not set")
	}
}

// Dropping the connection at any step logs off. Before the account is saved
// nothing is created; after it (at the closing pause) the account exists.
func TestNewUser_DisconnectAtEachStep(t *testing.T) {
	env := newMenuEnv(t)
	env.um.SetNewUserLevel(10)
	cases := []struct {
		name, input string
		created     bool
	}{
		{"apply", "", false},
		{"welcome screen", "Y", false},
		{"handle", "Y\rDrop", false},
		{"password", "Y\rDropa\rpw", false},
		{"confirmation", "Y\rDropb\rpw123\rpw", false},
		{"real name", "Y\rDropc\rpw123\rpw123\rDro", false},
		{"note", "Y\rDropd\rpw123\rpw123\rDro Pd\rno", false},
		{"location", "Y\rDrope\rpw123\rpw123\rDro Pe\r\rLo", false},
		{"closing pause", "Y\rDropf\rpw123\rpw123\rDro Pf\r\rHere\r", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			before := env.um.NextUserID()
			r := env.sub(t).runCmd("NEWUSER", nil, "", tc.input)
			if r.next != "LOGOFF" {
				t.Errorf("next = %q, want LOGOFF", r.next)
			}
			if created := env.um.NextUserID() != before; created != tc.created {
				t.Errorf("account created = %v, want %v", created, tc.created)
			}
		})
	}
}
