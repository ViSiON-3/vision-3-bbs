package menu

import (
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// pendingNewbie is an account awaiting validation: unvalidated, not deleted,
// and not banned (level above 0).
func pendingNewbie(id int, handle string) *user.User {
	return &user.User{ID: id, Handle: handle, RealName: handle + " Person", AccessLevel: 5, TimeLimit: 60}
}

// TestValidateUser_ValidatesAndEmptiesQueue pins the pending queue end to
// end: [G] then [S] on the only pending user validates it on disk, lifts it to
// the regular user level, audits the change, and ends on the "all validated"
// screen.
func TestValidateUser_ValidatesAndEmptiesQueue(t *testing.T) {
	env := newMenuEnv(t)
	env.seedUsers(pendingNewbie(3, "Newbie"))

	r := env.runCmd("VALIDATEUSER", env.sysop, "", "gs\r")
	if r.err != nil {
		t.Fatalf("err = %v", r.err)
	}
	if !r.has("Validate Pending Users", "Newbie", "All users have been validated!") {
		t.Errorf("output:\n%s", r.text())
	}
	u, _ := env.diskUser(3)
	if !u.Validated {
		t.Error("newbie not validated on disk")
	}
	if want := env.e.GetServerConfig().RegularUserLevel; u.AccessLevel != want {
		t.Errorf("level = %d, want regular level %d", u.AccessLevel, want)
	}
	if l, ok := modLogField(modAdminLog(t, env), 3, "validated"); !ok || l.NewValue != "true" || l.AdminID != 1 {
		t.Errorf("validated audit entry = %+v (found %v)", l, ok)
	}
}

// TestValidateUser_QueueListsOnlyPending pins that the queue hides validated,
// banned and deleted users, and that validating the last row of two clamps
// the selection back onto the remaining user instead of leaving the editor.
func TestValidateUser_QueueListsOnlyPending(t *testing.T) {
	env := newMenuEnv(t)
	banned := &user.User{ID: 5, Handle: "Banned", AccessLevel: 0}
	deleted := pendingNewbie(6, "Gone")
	deleted.DeletedUser = true
	env.seedUsers(pendingNewbie(3, "First"), pendingNewbie(4, "Second"), banned, deleted)

	// Move to Second, validate it, then quit the editor.
	r := env.runCmd("VALIDATEUSER", env.sysop, "", "jgsq")
	if r.err != nil {
		t.Fatalf("err = %v", r.err)
	}
	if r.has("All users have been validated!") {
		t.Errorf("queue reported empty with First still pending:\n%s", r.text())
	}
	for _, hidden := range []string{"Banned", "Gone", "Caller"} {
		if r.has(hidden) {
			t.Errorf("queue shows non-pending user %q", hidden)
		}
	}
	if u, _ := env.diskUser(4); !u.Validated {
		t.Error("Second not validated")
	}
	if u, _ := env.diskUser(3); u.Validated {
		t.Error("First validated but was never selected")
	}
}

// TestValidateUser_EmptyQueue pins the message shown when nobody is pending.
func TestValidateUser_EmptyQueue(t *testing.T) {
	env := newMenuEnv(t)
	r := env.runCmd("VALIDATEUSER", env.sysop, "", "\r")
	if r.err != nil || !r.has("No users pending validation.") {
		t.Errorf("err = %v output:\n%s", r.err, r.text())
	}
}

// TestNewUserValidation_SilentWhenNothingPending pins that NEWUSERVAL, which
// runs on every sysop login, prints nothing when the queue is empty.
func TestNewUserValidation_SilentWhenNothingPending(t *testing.T) {
	env := newMenuEnv(t)
	r := env.runCmd("NEWUSERVAL", env.sysop, "", "")
	if r.err != nil || strings.TrimSpace(r.text()) != "" {
		t.Errorf("err = %v output: %q", r.err, r.text())
	}
}

// TestNewUserValidation_HidesCountFromNonSysop pins that a non-sysop reaching
// the item is not told how many users are pending.
func TestNewUserValidation_HidesCountFromNonSysop(t *testing.T) {
	env := newMenuEnv(t)
	env.seedUsers(pendingNewbie(3, "Newbie"))
	r := env.runCmd("NEWUSERVAL", env.caller, "", "Y")
	if r.err != nil || strings.TrimSpace(r.text()) != "" {
		t.Errorf("err = %v output: %q", r.err, r.text())
	}
	if r := env.runCmd("NEWUSERVAL", nil, "", "Y"); strings.TrimSpace(r.text()) != "" {
		t.Errorf("logged-out output: %q", r.text())
	}
}

// TestNewUserValidation_PromptsWithCount pins the singular/plural count text
// and that declining returns without opening the queue.
func TestNewUserValidation_PromptsWithCount(t *testing.T) {
	env := newMenuEnv(t)
	env.seedUsers(pendingNewbie(3, "Newbie"))
	r := env.runCmd("NEWUSERVAL", env.sysop, "", "N")
	if !r.has("1 new user. Review?") || r.has("Validate Pending Users") {
		t.Errorf("single pending output:\n%s", r.text())
	}

	env.seedUsers(pendingNewbie(3, "Newbie"), pendingNewbie(4, "Other"))
	r = env.runCmd("NEWUSERVAL", env.sysop, "", "N")
	if !r.has("2 new users. Review?") {
		t.Errorf("two pending output:\n%s", r.text())
	}
	if u, _ := env.diskUser(3); u.Validated {
		t.Error("declining validated a user")
	}
}

// TestNewUserValidation_YesOpensQueue pins that answering Yes hands over to
// the validation queue, where the sysop can validate the user.
func TestNewUserValidation_YesOpensQueue(t *testing.T) {
	env := newMenuEnv(t)
	env.seedUsers(pendingNewbie(3, "Newbie"))
	r := env.runCmd("NEWUSERVAL", env.sysop, "", "Ygs\r")
	if r.err != nil {
		t.Fatalf("err = %v", r.err)
	}
	if !r.has("Validate Pending Users", "All users have been validated!") {
		t.Errorf("output:\n%s", r.text())
	}
	if u, _ := env.diskUser(3); !u.Validated {
		t.Error("newbie not validated")
	}
}

// TestUnvalidateUser_ClearsValidation pins that UNVALIDATEUSER clears the
// flag on disk for the chosen user and leaves the level alone.
func TestUnvalidateUser_ClearsValidation(t *testing.T) {
	env := newMenuEnv(t)
	r := env.runCmd("UNVALIDATEUSER", env.sysop, "", "j\rY\r")
	if r.err != nil {
		t.Fatalf("err = %v", r.err)
	}
	if !r.has("User set to unvalidated: Caller") {
		t.Errorf("output:\n%s", r.text())
	}
	u, _ := env.diskUser(2)
	if u.Validated || u.AccessLevel != 10 {
		t.Errorf("caller = validated %v level %d, want false/10", u.Validated, u.AccessLevel)
	}
	if _, ok := modLogField(modAdminLog(t, env), 2, "validated"); !ok {
		t.Error("no audit entry for validated")
	}
}

// TestUnvalidateUser_RefusesUserOneAndHonoursNo pins that the shared save
// path refuses to unvalidate User #1 (after confirmation) and that No cancels.
func TestUnvalidateUser_RefusesUserOneAndHonoursNo(t *testing.T) {
	env := newMenuEnv(t)
	if r := env.runCmd("UNVALIDATEUSER", env.sysop, "", "\rY\r"); !r.has("Cannot unvalidate User #1!") {
		t.Errorf("user #1 output:\n%s", r.text())
	}
	if r := env.runCmd("UNVALIDATEUSER", env.sysop, "", "j\rN\r"); !r.has("Cancelled.") {
		t.Errorf("decline output:\n%s", r.text())
	}
	for _, id := range []int{1, 2} {
		if u, _ := env.diskUser(id); !u.Validated {
			t.Errorf("user %d unvalidated", id)
		}
	}
	if r := env.runCmd("UNVALIDATEUSER", env.sysop, "", "q"); r.err != nil || r.has("Set Sysop") {
		t.Errorf("quit: err = %v output:\n%s", r.err, r.text())
	}
}
