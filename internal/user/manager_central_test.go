package user

import (
	"testing"
	"time"
)

// Consent changes only through SetCentralConsent: a session's stale copy,
// saved for something else, cannot restore sharing the caller withdrew on
// another node, and the change survives a reload.
func TestCentralConsentSurvivesStaleSaves(t *testing.T) {
	um, dir, u := newDeviceTestMgr(t)

	// Two sessions hold copies of the account.
	nodeA, _ := um.GetUserByID(u.ID)
	nodeB, _ := um.GetUserByID(u.ID)

	// Node A opts in to both.
	saved, err := um.SetCentralConsent(u.ID, func(x *User) {
		x.CentralPresence = true
		now := time.Now()
		x.CentralLastCallsSince = &now
		x.Handle = "Renamed" // not a consent field: ignored
	})
	if err != nil {
		t.Fatal(err)
	}
	if !saved.CentralPresence || saved.CentralLastCallsSince == nil || saved.Handle != u.Handle {
		t.Fatalf("saved %+v", saved)
	}

	// Node B, whose copy predates that, saves an unrelated change.
	nodeB.HotKeys = true
	if err := um.UpdateUser(nodeB); err != nil {
		t.Fatal(err)
	}
	if got, _ := um.GetUserByID(u.ID); !got.CentralPresence || got.CentralLastCallsSince == nil || !got.HotKeys {
		t.Fatalf("stale save undid the opt-in, or lost its own change: %+v", got)
	}

	// Node B withdraws; node A, still holding its opted-in copy, saves.
	if _, err := um.SetCentralConsent(u.ID, func(x *User) { x.CentralPresence = false; x.CentralLastCallsSince = nil }); err != nil {
		t.Fatal(err)
	}
	nodeA.CentralPresence = true
	off := false
	nodeA.MorePrompts = &off
	if err := um.UpdateUserByID(nodeA); err != nil {
		t.Fatal(err)
	}
	got, _ := um.GetUserByID(u.ID)
	if got.CentralPresence || got.CentralLastCallsSince != nil {
		t.Fatalf("a stale copy restored withdrawn consent: %+v", got)
	}

	reloaded, err := NewUserManager(dir)
	if err != nil {
		t.Fatal(err)
	}
	if r, _ := reloaded.GetUserByID(u.ID); r.CentralPresence || r.CentralLastCallsSince != nil || r.MorePromptsOn() {
		t.Errorf("after reload: %+v", r)
	}
	if _, err := um.SetCentralConsent(9999, func(*User) {}); err == nil {
		t.Error("no error for a missing account")
	}
}
