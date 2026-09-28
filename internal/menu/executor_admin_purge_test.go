package menu

import (
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// deletedUserAt is a soft-deleted account; a nil when means no timestamp.
func deletedUserAt(id int, handle string, when *time.Time) *user.User {
	return &user.User{ID: id, Handle: handle, AccessLevel: 10, Validated: true, DeletedUser: true, DeletedAt: when}
}

// TestPurgeUsers_DisabledByDefault pins that the shipped retention of -1
// turns purging off with an explanatory message and removes nobody.
func TestPurgeUsers_DisabledByDefault(t *testing.T) {
	env := newMenuEnv(t)
	env.seedUsers(deletedUserAt(3, "Old", nil))
	r := env.runCmd("PURGEUSERS", env.sysop, "", "Y\r")
	if r.err != nil || !r.has("User purge is disabled") {
		t.Errorf("err = %v output:\n%s", r.err, r.text())
	}
	if _, ok := env.diskUser(3); !ok {
		t.Error("disabled purge removed a user")
	}
}

// TestPurgeUsers_RemovesOnlyExpiredDeletions pins the retention cutoff: users
// deleted before it (or with no timestamp) are listed, purged from disk on
// Yes and audited as PURGE_USER; a recent deletion and live users survive.
func TestPurgeUsers_RemovesOnlyExpiredDeletions(t *testing.T) {
	env := newMenuEnv(t)
	setServerField(env.e, func(c *config.ServerConfig) { c.DeletedUserRetentionDays = 30 })
	old := time.Now().AddDate(0, 0, -60)
	recent := time.Now().AddDate(0, 0, -1)
	env.seedUsers(
		deletedUserAt(3, "Expired", &old),
		deletedUserAt(4, "NoStamp", nil),
		deletedUserAt(5, "Recent", &recent),
	)

	r := env.runCmd("PURGEUSERS", env.sysop, "", "Y\r")
	if r.err != nil {
		t.Fatalf("err = %v", r.err)
	}
	if !r.has("Purge Deleted Users", "Expired", "NoStamp", "(no timestamp)", "Permanently delete 2 account(s)?", "Purged 2 user account(s).") {
		t.Errorf("output:\n%s", r.text())
	}
	if r.has("Recent") {
		t.Errorf("recent deletion listed for purge:\n%s", r.text())
	}
	for id, want := range map[int]bool{1: true, 2: true, 3: false, 4: false, 5: true} {
		if _, ok := env.diskUser(id); ok != want {
			t.Errorf("user %d present = %v, want %v", id, ok, want)
		}
	}
	purged := map[int]bool{}
	for _, l := range modAdminLog(t, env) {
		if l.Action == "PURGE_USER" && l.AdminID == 1 {
			purged[l.TargetUserID] = true
		}
	}
	if !purged[3] || !purged[4] || len(purged) != 2 {
		t.Errorf("PURGE_USER audit targets = %v, want 3 and 4", purged)
	}
}

// TestPurgeUsers_DeclineKeepsEveryone pins that answering No purges nothing.
func TestPurgeUsers_DeclineKeepsEveryone(t *testing.T) {
	env := newMenuEnv(t)
	setServerField(env.e, func(c *config.ServerConfig) { c.DeletedUserRetentionDays = 0 })
	env.seedUsers(deletedUserAt(3, "Doomed", nil))
	r := env.runCmd("PURGEUSERS", env.sysop, "", "N\r")
	if !r.has("Doomed", "Cancelled.") {
		t.Errorf("output:\n%s", r.text())
	}
	if _, ok := env.diskUser(3); !ok {
		t.Error("declined purge removed the user")
	}
	if logs := modAdminLog(t, env); len(logs) != 0 {
		t.Errorf("admin log written: %+v", logs)
	}
}

// TestPurgeUsers_NothingEligible pins the empty case, which names the
// retention period.
func TestPurgeUsers_NothingEligible(t *testing.T) {
	env := newMenuEnv(t)
	setServerField(env.e, func(c *config.ServerConfig) { c.DeletedUserRetentionDays = 90 })
	recent := time.Now()
	env.seedUsers(deletedUserAt(3, "Fresh", &recent))
	r := env.runCmd("PURGEUSERS", env.sysop, "", "\r")
	if !r.has("No users eligible for purge.", "retention: 90 days") {
		t.Errorf("output:\n%s", r.text())
	}
}
