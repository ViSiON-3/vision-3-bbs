package usereditor

import (
	"testing"

	"github.com/charmbracelet/bubbles/textinput"

	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// newTestModelEditing constructs a minimal Model whose edit-target points at u,
// mirroring the real model's edit-target storage (editIndex into users slice).
func newTestModelEditing(u *user.User) Model {
	ti := textinput.New()
	ti.Prompt = ""
	ti.CharLimit = 256
	ti.Width = 60
	return Model{
		users:     []*user.User{u},
		editIndex: 0,
		mode:      modeEdit,
		textInput: ti,
		fields:    editFields(),
		tagged:    make(map[int]bool),
		width:     minWidth,
		height:    minHeight,
	}
}

func TestOpenKeyDialog(t *testing.T) {
	u := &user.User{Handle: "testuser"}
	m := newTestModelEditing(u)

	m.openKeyDialog()

	if m.mode != modeKeyList {
		t.Fatalf("expected modeKeyList after openKeyDialog, got %v", m.mode)
	}
	if m.keySelected != 0 {
		t.Fatalf("expected keySelected=0, got %d", m.keySelected)
	}
}

func TestKeyDialogAdd(t *testing.T) {
	// Use a real ed25519 public key line (from pubkey_ops_test.go pattern)
	// We'll use a known-good authorized_keys line.
	goodKey := "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAILyFECUpFcGnHIkwAWL3AMrv1bJXGnCKjdN9zXYfRUAo testcomment"

	u := &user.User{Handle: "testuser"}
	m := newTestModelEditing(u)
	m.openKeyDialog()

	if err := m.keyDialogAdd(goodKey); err != nil {
		t.Fatalf("keyDialogAdd failed: %v", err)
	}
	if len(u.PublicKeys) != 1 {
		t.Fatalf("expected 1 key after add, got %d", len(u.PublicKeys))
	}

	// Adding same key again must fail
	if err := m.keyDialogAdd(goodKey); err == nil {
		t.Fatal("expected error on duplicate add, got nil")
	}
	if len(u.PublicKeys) != 1 {
		t.Fatalf("key count must stay 1 after dup, got %d", len(u.PublicKeys))
	}

	// Bad key must fail
	if err := m.keyDialogAdd("not-a-key"); err == nil {
		t.Fatal("expected error on invalid key, got nil")
	}
}

func TestKeyDialogDelete(t *testing.T) {
	goodKey := "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAILyFECUpFcGnHIkwAWL3AMrv1bJXGnCKjdN9zXYfRUAo testcomment"

	u := &user.User{Handle: "testuser"}
	m := newTestModelEditing(u)
	m.openKeyDialog()

	if err := m.keyDialogAdd(goodKey); err != nil {
		t.Fatalf("setup add failed: %v", err)
	}
	if len(u.PublicKeys) != 1 {
		t.Fatalf("expected 1 key after add, got %d", len(u.PublicKeys))
	}

	// Delete by 1-based index "1"
	m.keySelected = 0
	if err := m.keyDialogDelete("1"); err != nil {
		t.Fatalf("keyDialogDelete failed: %v", err)
	}
	if len(u.PublicKeys) != 0 {
		t.Fatalf("expected 0 keys after delete, got %d", len(u.PublicKeys))
	}

	// Deleting when empty must error
	if err := m.keyDialogDelete("1"); err == nil {
		t.Fatal("expected error deleting from empty list, got nil")
	}
}

func TestKeyDialogEditingUserMutates(t *testing.T) {
	goodKey := "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAILyFECUpFcGnHIkwAWL3AMrv1bJXGnCKjdN9zXYfRUAo testcomment"

	u := &user.User{Handle: "mutate-test"}
	m := newTestModelEditing(u)

	// Confirm editingUser() returns the same pointer as u
	if m.editingUser() != u {
		t.Fatal("editingUser() must return the pointer stored at users[editIndex]")
	}

	m.openKeyDialog()
	if err := m.keyDialogAdd(goodKey); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Mutation is visible through the original u pointer
	if len(u.PublicKeys) == 0 {
		t.Fatal("u.PublicKeys must be mutated through editingUser()")
	}
}
