package usereditor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// three returns an editor over the sysop plus two ordinary users, with the
// users.json path.
func three(t *testing.T) (Model, string) {
	t.Helper()
	m, path := editorOver(t,
		&user.User{ID: 1, Handle: "Sysop", AccessLevel: 255},
		&user.User{ID: 2, Handle: "Bob", AccessLevel: 5},
		&user.User{ID: 3, Handle: "Carol", AccessLevel: 5},
	)
	return press(t, m, tea.WindowSizeMsg{Width: 80, Height: 25}), path
}

// Deleting a user is a soft delete: it moves them under the DELETED USERS
// separator, offers a purge, and declining the purge keeps the record. Saved
// and reloaded, the user is still there and marked deleted.
func TestDeleteThenDeclinePurgeKeepsASoftDeletedRecord(t *testing.T) {
	m, path := three(t)
	m = press(t, m, key(tea.KeyDown), key(tea.KeyF2))
	if !strings.Contains(stripANSIGolden(m.View()), "Delete Bob?") {
		t.Error("delete prompt does not name the user")
	}
	m = press(t, m, char('y'))
	if m.mode != modePurgeConfirm {
		t.Fatalf("after delete: mode = %v, want the purge offer", m.mode)
	}
	if !strings.Contains(stripANSIGolden(m.View()), "Bob") {
		t.Error("purge prompt does not name the user")
	}
	m = press(t, m, char('n'))
	if m.mode != modeList {
		t.Fatalf("declining purge: mode = %v, want modeList", m.mode)
	}
	if got := ids(m.users); got[2] != 2 || m.cursor != 2 {
		t.Errorf("order %v cursor %d: deleted Bob should sort last and keep the cursor", got, m.cursor)
	}
	if !strings.Contains(stripANSIGolden(m.View()), "DELETED USERS") {
		t.Error("list does not show the deleted-users separator")
	}

	saveAndQuit(t, m)
	got, ok := reloadUser(t, path, 2)
	if !ok {
		t.Fatal("soft-deleted user vanished from disk")
	}
	if !got.DeletedUser || got.DeletedAt == nil {
		t.Errorf("reloaded Bob: DeletedUser=%v DeletedAt=%v, want deleted", got.DeletedUser, got.DeletedAt)
	}
}

// Accepting the purge removes the record entirely and deletes the user's
// infoform responses; saved, the user is gone from users.json.
func TestDeleteThenPurgeRemovesRecordAndResponses(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "users.json")
	if _, err := SaveUsers(path, []*user.User{
		{ID: 1, Handle: "Sysop"}, {ID: 2, Handle: "Bob"}, {ID: 3, Handle: "Carol"},
	}); err != nil {
		t.Fatal(err)
	}
	responses := filepath.Join(dir, "infoforms", "responses")
	if err := os.MkdirAll(responses, 0o755); err != nil {
		t.Fatal(err)
	}
	bobForm := filepath.Join(responses, "2_1.json")
	carolForm := filepath.Join(responses, "3_1.json")
	for _, f := range []string{bobForm, carolForm} {
		if err := os.WriteFile(f, []byte("{}"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	m, err := New(path, dir)
	if err != nil {
		t.Fatal(err)
	}

	m = press(t, m, key(tea.KeyDown), key(tea.KeyF2), char('y'), char('y'))
	if m.mode != modeList {
		t.Fatalf("mode = %v, want modeList", m.mode)
	}
	if got := ids(m.users); len(got) != 2 || got[1] != 3 {
		t.Fatalf("ids after purge = %v, want [1 3]", got)
	}
	if _, err := os.Stat(bobForm); !os.IsNotExist(err) {
		t.Errorf("Bob's infoform response survived the purge: %v", err)
	}
	if _, err := os.Stat(carolForm); err != nil {
		t.Errorf("Carol's response was removed too: %v", err)
	}

	saveAndQuit(t, m)
	if _, ok := reloadUser(t, path, 2); ok {
		t.Error("purged user is still in users.json")
	}
	if _, ok := reloadUser(t, path, 3); !ok {
		t.Error("Carol went missing")
	}
}

// F2 on an already-deleted user offers to undelete them; confirming restores
// them to the live section of the list.
func TestF2OnADeletedUserUndeletes(t *testing.T) {
	m, path := three(t)
	m = press(t, m, key(tea.KeyDown), key(tea.KeyF2), char('y'), char('n')) // Bob now last, cursor on him
	m = press(t, m, key(tea.KeyF2))
	if m.mode != modeUndeleteConfirm {
		t.Fatalf("F2 on a deleted user: mode = %v, want modeUndeleteConfirm", m.mode)
	}
	if !strings.Contains(stripANSIGolden(m.View()), "Undelete Bob?") {
		t.Error("undelete prompt does not name the user")
	}
	m = press(t, m, char('y'))
	if m.mode != modeList {
		t.Fatalf("mode = %v, want modeList", m.mode)
	}
	if got := ids(m.users); got[1] != 2 || m.users[1].DeletedUser {
		t.Errorf("ids %v: Bob should be restored to ID order and live", got)
	}

	saveAndQuit(t, m)
	got, _ := reloadUser(t, path, 2)
	if got.DeletedUser || got.DeletedAt != nil {
		t.Errorf("undelete did not persist: %+v", got)
	}
}

// F4 on a deleted user purges it after confirmation, and declining leaves it.
func TestF4PurgesADeletedUser(t *testing.T) {
	m, _ := three(t)
	m = press(t, m, key(tea.KeyDown), key(tea.KeyF2), char('y'), char('n'))
	m = press(t, m, key(tea.KeyF4), char('n'))
	if len(m.users) != 3 {
		t.Fatal("declining the purge removed the user")
	}
	m = press(t, m, key(tea.KeyF4), char('y'))
	if len(m.users) != 2 || m.cursor != 1 {
		t.Errorf("after purge: %d users, cursor %d; want 2 users and cursor clamped to 1", len(m.users), m.cursor)
	}
}

// F5 quick-validates the highlighted user to the house defaults, which then
// persist.
func TestF5QuickValidates(t *testing.T) {
	m, path := three(t)
	m = press(t, m, key(tea.KeyDown), key(tea.KeyF5))
	if !strings.Contains(stripANSIGolden(m.View()), "Set Bob to Default?") {
		t.Error("validate prompt does not name the user")
	}
	m = press(t, m, char('y'))

	saveAndQuit(t, m)
	got, _ := reloadUser(t, path, 2)
	if got.AccessLevel != 10 || !got.Validated || got.FilePoints != 100 || got.TimeLimit != 60 {
		t.Errorf("validated Bob = level %d validated %v points %d time %d, want 10 true 100 60",
			got.AccessLevel, got.Validated, got.FilePoints, got.TimeLimit)
	}
	if carol, _ := reloadUser(t, path, 3); carol.Validated {
		t.Error("validating Bob also validated Carol")
	}
}

// With nothing tagged, the mass actions refuse with a message instead of
// opening a prompt (Shift+F2, Shift+F5, Shift+F4).
func TestMassActionsNeedTags(t *testing.T) {
	m, _ := three(t)
	for _, k := range []tea.KeyType{tea.KeyShiftF2, tea.KeyShiftF5} {
		m = press(t, m, key(k))
		if m.mode != modeList || !strings.Contains(m.message, "not tagged anyone") {
			t.Errorf("%s with no tags: mode=%v message=%q", k, m.mode, m.message)
		}
	}
	m = press(t, m, key(tea.KeyShiftF4))
	if m.mode != modeList || m.message != "No deleted users to purge." {
		t.Errorf("shift+f4 with none deleted: mode=%v message=%q", m.mode, m.message)
	}
}

// Mass validate applies the defaults to every tagged user and only those, and
// clears the tags afterwards.
func TestMassValidateTagged(t *testing.T) {
	m, path := three(t)
	m = press(t, m, key(tea.KeyDown), key(tea.KeySpace), key(tea.KeySpace)) // tag Bob, Carol
	m = press(t, m, key(tea.KeyShiftF5))
	if !strings.Contains(stripANSIGolden(m.View()), "Set All Tagged (2) Users") {
		t.Error("mass-validate prompt does not give the tag count")
	}
	m = press(t, m, char('y'))
	if m.taggedCount() != 0 {
		t.Error("tags survived the mass validate")
	}

	saveAndQuit(t, m)
	for id, want := range map[int]bool{1: false, 2: true, 3: true} {
		u, _ := reloadUser(t, path, id)
		if u.Validated != want {
			t.Errorf("user %d validated = %v, want %v", id, u.Validated, want)
		}
	}
}

// Mass purge removes every deleted user at once and reports the count.
func TestMassPurgeRemovesAllDeleted(t *testing.T) {
	m, path := three(t)
	m = press(t, m, key(tea.KeyDown), key(tea.KeyF2), char('y'), char('n'))
	m = press(t, m, key(tea.KeyHome), key(tea.KeyDown), key(tea.KeyF2), char('y'), char('n'))
	if m.deletedCount() != 2 {
		t.Fatalf("setup: %d deleted, want 2", m.deletedCount())
	}
	m = press(t, m, key(tea.KeyShiftF4))
	if !strings.Contains(stripANSIGolden(m.View()), "purge 2 deleted user(s)") {
		t.Error("mass-purge prompt does not give the count")
	}
	m = press(t, m, char('y'))
	if len(m.users) != 1 || m.message != "Purged 2 deleted user(s)" {
		t.Fatalf("after mass purge: %d users, message %q", len(m.users), m.message)
	}

	saveAndQuit(t, m)
	for _, id := range []int{2, 3} {
		if _, ok := reloadUser(t, path, id); ok {
			t.Errorf("user %d survived the mass purge on disk", id)
		}
	}
}

// Mass delete with a single tag soft-deletes exactly that user and clears the
// tags.
func TestMassDeleteSingleTagged(t *testing.T) {
	m, _ := three(t)
	m = press(t, m, key(tea.KeyEnd), key(tea.KeySpace), key(tea.KeyShiftF2))
	if !strings.Contains(stripANSIGolden(m.View()), "Delete All Tagged (1) Users?") {
		t.Error("mass-delete prompt does not give the tag count")
	}
	m = press(t, m, char('y'))
	for _, u := range m.users {
		if u.DeletedUser != (u.ID == 3) {
			t.Errorf("user %d deleted = %v", u.ID, u.DeletedUser)
		}
	}
	if m.taggedCount() != 0 {
		t.Error("tags survived the mass delete")
	}
}
