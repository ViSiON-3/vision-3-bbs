package usereditor

import (
	"crypto/ed25519"
	"crypto/rand"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"golang.org/x/crypto/ssh"

	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// makeTestKey returns a valid OpenSSH ed25519 authorized_keys line for testing.
func makeTestKey(t *testing.T, comment string) string {
	t.Helper()
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("gen key: %v", err)
	}
	sshPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		t.Fatalf("ssh pub: %v", err)
	}
	line := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sshPub)))
	if comment != "" {
		line += " " + comment
	}
	return line
}

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

// TestUpdateDispatchKeyAdd verifies that the Update method correctly routes
// modeKeyAdd Enter key events through to keyDialogAdd, returning to modeKeyList
// with the key added to the user.
func TestUpdateDispatchKeyAdd(t *testing.T) {
	goodKey := "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAILyFECUpFcGnHIkwAWL3AMrv1bJXGnCKjdN9zXYfRUAo testcomment"

	u := &user.User{Handle: "dispatch-test"}
	m := newTestModelEditing(u)
	m.openKeyDialog()

	// Transition to modeKeyAdd as the real "A" keypress does
	m.mode = modeKeyAdd
	m.keyDialogErr = ""
	m.textInput.SetValue(goodKey)
	m.textInput.EchoMode = textinput.EchoNormal
	m.textInput.CharLimit = 512
	m.textInput.Width = 68
	m.textInput.Focus()

	// Send Enter through the Update dispatch
	result, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	got, ok := result.(Model)
	if !ok {
		t.Fatal("Update must return a Model")
	}

	if got.mode != modeKeyList {
		t.Fatalf("expected modeKeyList after successful add, got mode %v", got.mode)
	}
	if len(got.editingUser().PublicKeys) != 1 {
		t.Fatalf("expected 1 key after add via Update, got %d", len(got.editingUser().PublicKeys))
	}
}

// TestUpdateDispatchKeyListDelete verifies that the Update method correctly routes
// modeKeyList "D" key events through to keyDialogDelete.
func TestUpdateDispatchKeyListDelete(t *testing.T) {
	goodKey := "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAILyFECUpFcGnHIkwAWL3AMrv1bJXGnCKjdN9zXYfRUAo testcomment"

	u := &user.User{Handle: "dispatch-delete-test"}
	m := newTestModelEditing(u)
	m.openKeyDialog()
	if err := m.keyDialogAdd(goodKey); err != nil {
		t.Fatalf("setup add failed: %v", err)
	}
	m.keySelected = 0

	// Send "d" through the Update dispatch while in modeKeyList
	result, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	got, ok := result.(Model)
	if !ok {
		t.Fatal("Update must return a Model")
	}

	if got.mode != modeKeyList {
		t.Fatalf("expected to stay in modeKeyList after delete, got mode %v", got.mode)
	}
	if len(got.editingUser().PublicKeys) != 0 {
		t.Fatalf("expected 0 keys after delete via Update, got %d", len(got.editingUser().PublicKeys))
	}
}

// TestKeyListDialogScrollWindow verifies that when keySelected moves past the 8th
// key the rendered overlay includes the selected key's fingerprint and that the
// highlight tracks keys beyond the initial window (C2 scroll-window fix).
func TestKeyListDialogScrollWindow(t *testing.T) {
	u := &user.User{Handle: "scrolltest"}
	m := newTestModelEditing(u)
	m.openKeyDialog()

	// Add 10 distinct keys.
	var fps []string
	for i := 0; i < 10; i++ {
		line := makeTestKey(t, "")
		if err := m.keyDialogAdd(line); err != nil {
			t.Fatalf("add key %d: %v", i, err)
		}
		keys, _ := u.ListPublicKeys()
		fps = append(fps, keys[len(keys)-1].Fingerprint)
	}
	if len(fps) != 10 {
		t.Fatalf("expected 10 fingerprints, got %d", len(fps))
	}

	// Move selection to the last key (index 9, 0-based).
	m.keySelected = 9

	// Render the dialog over a blank background.
	background := strings.Repeat(strings.Repeat(" ", minWidth)+"\n", minHeight)
	rendered := m.overlayKeyListDialog(background)

	// Fingerprints are rendered truncated to 47 chars by padRight; use a prefix
	// that is guaranteed present whether or not the full string is clipped.
	fpPrefix := func(fp string) string {
		r := []rune(fp)
		if len(r) > 47 {
			return string(r[:47])
		}
		return fp
	}

	// The last key's fingerprint (or its 47-char prefix) must appear in the
	// rendered output — scroll window must have advanced to include key 10.
	lastFP := fps[9]
	if !strings.Contains(rendered, fpPrefix(lastFP)) {
		t.Fatalf("rendered dialog does not contain fingerprint of key 10 (%s); scroll window is broken", lastFP)
	}

	// The first key's fingerprint must NOT appear (it has scrolled off).
	firstFP := fps[0]
	if strings.Contains(rendered, fpPrefix(firstFP)) {
		t.Fatalf("rendered dialog contains fingerprint of key 1 (%s); scroll window did not advance", firstFP)
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
