package usereditor

import (
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

func ids(users []*user.User) []int {
	out := make([]int, len(users))
	for i, u := range users {
		out[i] = u.ID
	}
	return out
}

func TestSortUsers_ByID_DeletedLast(t *testing.T) {
	users := []*user.User{
		{ID: 2, Handle: "Bob"},
		{ID: 1, Handle: "Alice", DeletedUser: true},
		{ID: 3, Handle: "Carol"},
	}
	sortUsers(users, false)
	// Non-deleted come first (by ID), deleted last.
	got := ids(users)
	want := []int{2, 3, 1}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("by-ID order = %v, want %v", got, want)
		}
	}
}

func TestSortUsers_Alpha_DeletedLast(t *testing.T) {
	users := []*user.User{
		{ID: 1, Handle: "charlie"},
		{ID: 2, Handle: "Alice"},
		{ID: 3, Handle: "bob", DeletedUser: true},
	}
	sortUsers(users, true)
	// Case-insensitive handle order among non-deleted, deleted ("bob") last.
	if users[0].Handle != "Alice" || users[1].Handle != "charlie" {
		t.Fatalf("alpha order = [%s, %s, %s], want [Alice, charlie, bob]",
			users[0].Handle, users[1].Handle, users[2].Handle)
	}
	if !users[2].DeletedUser {
		t.Errorf("deleted user should sort last, got %s", users[2].Handle)
	}
}

func TestShouldSwap_DeletedAlwaysAfter(t *testing.T) {
	active := &user.User{ID: 5, Handle: "z", DeletedUser: false}
	deleted := &user.User{ID: 1, Handle: "a", DeletedUser: true}
	less := func(a, b *user.User) bool { return a.ID < b.ID }

	// Active before deleted regardless of the less() result.
	if !shouldSwap(active, deleted, less) {
		t.Error("active user should sort before a deleted user")
	}
	if shouldSwap(deleted, active, less) {
		t.Error("deleted user should not sort before an active user")
	}
}
