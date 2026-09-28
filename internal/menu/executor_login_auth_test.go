package menu

import (
	"sync"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// fakeLockout records what the login handler reports to the IP lockout
// checker, and can be told to report an IP as locked.
type fakeLockout struct {
	mu       sync.Mutex
	lockedIP string
	until    time.Time
	failed   map[string]int
	cleared  []string
	queried  []string
}

func newFakeLockout() *fakeLockout { return &fakeLockout{failed: map[string]int{}} }

func (f *fakeLockout) IsIPLockedOut(ip string) (bool, time.Time, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.queried = append(f.queried, ip)
	if ip == f.lockedIP {
		return true, f.until, 5
	}
	return false, time.Time{}, 0
}

func (f *fakeLockout) RecordFailedLoginAttempt(ip string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failed[ip]++
	return false
}

func (f *fakeLockout) ClearFailedLoginAttempts(ip string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cleared = append(f.cleared, ip)
}

// addLoginUser creates an account with a real bcrypt password at level.
func addLoginUser(t *testing.T, env *menuEnv, handle, password string, level int) *user.User {
	t.Helper()
	u, err := env.um.AddUser(password, handle, handle+" Tester", "Testville")
	if err != nil {
		t.Fatalf("AddUser(%s): %v", handle, err)
	}
	u.AccessLevel = level
	if err := env.um.UpdateUser(u); err != nil {
		t.Fatalf("UpdateUser(%s): %v", handle, err)
	}
	return u
}

// The right handle and password log the caller in: the handler returns their
// account (with the login stamped), and the IP's failed-attempt count is
// cleared. The typed password is masked, and a backspace edits it.
func TestAuthenticate_CorrectPasswordLogsIn(t *testing.T) {
	env := newMenuEnv(t)
	addLoginUser(t, env, "Alice", "secret", 20)
	lock := newFakeLockout()
	env.e.IPLockoutCheck = lock

	r := env.runFrom("203.0.113.5", runAuthenticate, nil, "alice\rsecrex\x7ft\r")
	if r.err != nil {
		t.Fatalf("err = %v", r.err)
	}
	if r.user == nil || r.user.Handle != "Alice" {
		t.Fatalf("user = %+v, want Alice", r.user)
	}
	if r.next != "" {
		t.Errorf("next = %q, want none", r.next)
	}
	if r.user.LastLogin.IsZero() {
		t.Error("LastLogin not stamped on a successful login")
	}
	if len(lock.cleared) != 1 || lock.cleared[0] != "203.0.113.5" {
		t.Errorf("cleared = %v, want [203.0.113.5]", lock.cleared)
	}
	if lock.failed["203.0.113.5"] != 0 {
		t.Errorf("a successful login recorded a failed attempt")
	}
	if r.has("secret") || !r.has("******") {
		t.Errorf("password not masked in output: %q", r.text())
	}
}

// A wrong password is refused with "Login incorrect", returns no user, and is
// counted against the caller's IP.
func TestAuthenticate_WrongPasswordRefusedAndCounted(t *testing.T) {
	t.Parallel()
	env := newMenuEnv(t)
	addLoginUser(t, env, "Alice", "secret", 20)
	lock := newFakeLockout()
	env.e.IPLockoutCheck = lock

	r := env.runFrom("198.51.100.7", runAuthenticate, nil, "Alice\rguess\r")
	if r.user != nil || r.err != nil || r.next != "" {
		t.Fatalf("got user=%v next=%q err=%v, want refused with no action", r.user, r.next, r.err)
	}
	if !r.has("Login incorrect.") {
		t.Errorf("no login-incorrect message: %q", r.text())
	}
	if lock.failed["198.51.100.7"] != 1 {
		t.Errorf("failed attempts for IP = %d, want 1", lock.failed["198.51.100.7"])
	}
	if len(lock.cleared) != 0 {
		t.Errorf("failed login cleared the lockout counter: %v", lock.cleared)
	}
}

// A caller from a locked-out IP is turned away with the lockout message even
// with the right password, and no further attempt is counted.
func TestAuthenticate_LockedIPRefusedEvenWithRightPassword(t *testing.T) {
	t.Parallel()
	env := newMenuEnv(t)
	addLoginUser(t, env, "Alice", "secret", 20)
	lock := newFakeLockout()
	lock.lockedIP = "192.0.2.9"
	lock.until = time.Now().Add(10 * time.Minute)
	env.e.IPLockoutCheck = lock

	r := env.runFrom("192.0.2.9", runAuthenticate, nil, "Alice\rsecret\r")
	if r.user != nil {
		t.Fatalf("locked IP logged in as %s", r.user.Handle)
	}
	if !r.has("Too many failed login attempts", "try again in") {
		t.Errorf("no lockout message: %q", r.text())
	}
	if len(lock.failed) != 0 || len(lock.cleared) != 0 {
		t.Errorf("locked attempt touched counters: failed=%v cleared=%v", lock.failed, lock.cleared)
	}
}

// A correct password on an account below the logon level is denied.
func TestAuthenticate_BelowLogonLevelDenied(t *testing.T) {
	t.Parallel()
	env := newMenuEnv(t)
	addLoginUser(t, env, "Lowly", "secret", 5)
	setServerField(env.e, func(c *config.ServerConfig) { c.LogonLevel = 10 })

	r := env.runFrom("203.0.113.5", runAuthenticate, nil, "Lowly\rsecret\r")
	if r.user != nil {
		t.Fatalf("level 5 account logged in past logonLevel 10")
	}
	if !r.has("Access Denied.") {
		t.Errorf("no access-denied message: %q", r.text())
	}
}

// Running AUTHENTICATE while already logged in says so and changes nothing.
func TestAuthenticate_AlreadyLoggedIn(t *testing.T) {
	t.Parallel()
	env := newMenuEnv(t)
	r := env.runCmd("AUTHENTICATE", env.caller, "", "Sysop\r")
	if r.user != nil || r.next != "" {
		t.Fatalf("user=%v next=%q, want no change", r.user, r.next)
	}
	if !r.has("You are already logged in.") {
		t.Errorf("no already-logged-in message: %q", r.text())
	}
	if r.has("Sysop") {
		t.Errorf("handler read a username while already logged in")
	}
}

// A blank username just redisplays the login screen without asking for a
// password; a disconnect at the username prompt logs off.
func TestAuthenticate_BlankUsernameAndDisconnect(t *testing.T) {
	env := newMenuEnv(t)
	r := env.runCmd("AUTHENTICATE", nil, "", "   \r")
	if r.user != nil || r.next != "" || r.err != nil {
		t.Fatalf("blank username: user=%v next=%q err=%v", r.user, r.next, r.err)
	}
	if r.has("Password:") {
		t.Errorf("blank username still prompted for a password")
	}

	r = env.runCmd("AUTHENTICATE", nil, "", "")
	if r.next != "LOGOFF" {
		t.Errorf("disconnect at username: next=%q, want LOGOFF", r.next)
	}
}

// ESC at either prompt asks "Abort Login?": Yes logs off, No returns to the
// login screen. Ctrl+C at the password prompt aborts the same way.
func TestAuthenticate_AbortPrompts(t *testing.T) {
	env := newMenuEnv(t)
	cases := []struct {
		name, input, next string
	}{
		{"esc at username, yes", "\x1bY", "LOGOFF"},
		{"esc at username, no", "\x1bN", ""},
		{"esc at password, yes", "Alice\r\x1bY", "LOGOFF"},
		{"ctrl-c at password, no", "Alice\r\x03N", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := env.sub(t).runCmd("AUTHENTICATE", nil, "", tc.input)
			if r.user != nil || r.next != tc.next {
				t.Fatalf("user=%v next=%q, want next %q", r.user, r.next, tc.next)
			}
			if !r.has("Abort Login?") {
				t.Errorf("no abort confirmation: %q", r.text())
			}
		})
	}

	// Dropping at the password prompt logs off.
	r := env.runCmd("AUTHENTICATE", nil, "", "Alice\rsec")
	if r.next != "LOGOFF" {
		t.Errorf("disconnect at password: next=%q, want LOGOFF", r.next)
	}
}

// Typing "new" at the username prompt runs the signup form and, when the new
// account meets the logon level, carries the caller straight into a session
// as that account.
func TestAuthenticate_NewStartsSignupAndContinues(t *testing.T) {
	env := newMenuEnv(t)
	env.um.SetNewUserLevel(10)

	r := env.runCmd("AUTHENTICATE", nil, "", "new\r"+newUserFormInput("Newbie", "pw123", "Nora Newbie", "", "Here"))
	if r.err != nil {
		t.Fatalf("err = %v", r.err)
	}
	if r.user == nil || r.user.Handle != "Newbie" {
		t.Fatalf("user = %+v, want the new account Newbie", r.user)
	}
	if _, ok := env.um.Authenticate("Newbie", "pw123"); !ok {
		t.Error("new account does not authenticate with the password set at signup")
	}
}
