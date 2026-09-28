package usereditor

import (
	"crypto/ed25519"
	"crypto/rand"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"golang.org/x/crypto/ssh"

	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// Regression tests for #456: shifted-F mass actions that could not be
// reached, bulk actions that hit the wrong users, and an F10 Abort that
// aborted nothing.

// rawSequence stands in for bubbletea's unexported unknownCSISequenceMsg,
// which is how a sequence missing from its key table (xterm's Shift+F10)
// reaches Update.
type rawSequence []byte

func keyOf(k tea.KeyType) tea.Msg { return tea.KeyMsg{Type: k} }

func runesOf(s string) tea.Msg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }

// press feeds each message to the model through Update, in order.
func press(t *testing.T, m Model, msgs ...tea.Msg) Model {
	t.Helper()
	for _, msg := range msgs {
		updated, _ := m.Update(msg)
		m = updated.(Model)
	}
	return m
}

// seeded returns a model on the user list over users.json holding one user
// per handle, IDs from 1 in order, none validated.
func seeded(t *testing.T, handles ...string) (Model, string) {
	t.Helper()
	users := make([]*user.User, len(handles))
	for i, h := range handles {
		users[i] = &user.User{ID: i + 1, Handle: h, AccessLevel: 1, GroupLocation: "Earth"}
	}
	m, path := editorOver(t, users...)
	return press(t, m, tea.WindowSizeMsg{Width: 80, Height: 25}), path
}

// byHandle returns the in-memory record for handle.
func (m Model) byHandle(t *testing.T, handle string) *user.User {
	t.Helper()
	for _, u := range m.users {
		if u.Handle == handle {
			return u
		}
	}
	t.Fatalf("no user %q in the list", handle)
	return nil
}

// saveAndQuit leaves the editor through its exit prompt, saving.
func saveAndQuit(t *testing.T, m Model) {
	t.Helper()
	m = press(t, m, keyOf(tea.KeyEscape))
	if m.mode != modeExitConfirm {
		t.Fatalf("Esc with unsaved changes gave mode %v, want the save prompt", m.mode)
	}
	m = press(t, m, runesOf("y"))
	if m.dirty {
		t.Fatalf("still dirty after saving on exit: %q", m.message)
	}
}

// reloaded reads users.json back the way the BBS does.
func reloaded(t *testing.T, path, handle string) *user.User {
	t.Helper()
	um, err := user.NewUserManager(filepath.Dir(path))
	if err != nil {
		t.Fatalf("NewUserManager: %v", err)
	}
	u, ok := um.GetUser(handle)
	if !ok {
		t.Fatalf("%s missing from users.json", handle)
	}
	return u
}

// Each shifted mass action is reachable by the key the vendored bubbletea
// actually reports for it from an xterm-style terminal. Matching only
// "shift+fN", as before, left every one of them dead.
func TestShiftedMassActionKeysAreReachable(t *testing.T) {
	cases := []struct {
		name string
		key  tea.Msg
		want editorMode
	}{
		{"Shift+F2 (xterm ESC[1;2Q = f14)", keyOf(tea.KeyF14), modeMassDelete},
		{"Shift+F4 (xterm ESC[1;2S = f16)", keyOf(tea.KeyF16), modeMassPurge},
		{"Shift+F5 (xterm ESC[15;2~ = f17)", keyOf(tea.KeyF17), modeMassValidate},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m, _ := seeded(t, "Alpha", "Bravo", "Charlie")
			// Tag Bravo and delete Charlie, so every mass action has a target.
			m = press(t, m, keyOf(tea.KeyDown), keyOf(tea.KeySpace),
				keyOf(tea.KeyF2), runesOf("y"), runesOf("n"))
			m = press(t, m, tc.key)
			if m.mode != tc.want {
				t.Errorf("mode = %v, want %v (message %q)", m.mode, tc.want, m.message)
			}
		})
	}
}

// Shift+F10 untags everything, whether it arrives as xterm's unmapped
// ESC[21;2~ or as f20 (the Linux console and rxvt).
func TestShiftF10UntagsAll(t *testing.T) {
	for name, key := range map[string]tea.Msg{
		"xterm ESC[21;2~":  rawSequence("\x1b[21;2~"),
		"linux/rxvt (f20)": keyOf(tea.KeyF20),
	} {
		t.Run(name, func(t *testing.T) {
			m, _ := seeded(t, "Alpha", "Bravo", "Charlie")
			m = press(t, m, keyOf(tea.KeyF10), key, keyOf(tea.KeyF17))
			if m.mode != modeList || !strings.Contains(m.message, "not tagged anyone") {
				t.Errorf("after untag, mass validate gave mode %v, message %q; want the nobody-tagged message",
					m.mode, m.message)
			}
		})
	}
}

// The repro from #456: tag B and C out of A..D and mass delete. Deleting
// re-sorts the list, and walking it by index deleted B and D, sparing C.
func TestMassDeleteDeletesExactlyTheTaggedUsers(t *testing.T) {
	m, path := seeded(t, "Alpha", "Bravo", "Charlie", "Delta")
	m = press(t, m, keyOf(tea.KeyDown), keyOf(tea.KeySpace), keyOf(tea.KeySpace))
	m = press(t, m, keyOf(tea.KeyF14), runesOf("y"))

	want := map[string]bool{"Alpha": false, "Bravo": true, "Charlie": true, "Delta": false}
	for h, deleted := range want {
		if got := m.byHandle(t, h).DeletedUser; got != deleted {
			t.Errorf("in memory: %s deleted = %v, want %v", h, got, deleted)
		}
	}
	saveAndQuit(t, m)
	for h, deleted := range want {
		if got := reloaded(t, path, h).DeletedUser; got != deleted {
			t.Errorf("users.json: %s deleted = %v, want %v", h, got, deleted)
		}
	}
}

// A tag stays on its user when F3 re-sorts the list. Keyed by row, it
// stayed on the row and moved to whoever sorted into it.
func TestTagFollowsItsUserThroughASort(t *testing.T) {
	m, path := seeded(t, "Delta", "Charlie", "Bravo", "Alpha")
	m = press(t, m, keyOf(tea.KeyDown), keyOf(tea.KeySpace)) // tag Charlie, row 1
	m = press(t, m, keyOf(tea.KeyF3))                        // alphabetical: Charlie moves to row 2
	m = press(t, m, keyOf(tea.KeyF17), runesOf("y"))

	for _, h := range []string{"Alpha", "Bravo", "Charlie", "Delta"} {
		if got, want := m.byHandle(t, h).Validated, h == "Charlie"; got != want {
			t.Errorf("in memory: %s validated = %v, want %v", h, got, want)
		}
	}
	saveAndQuit(t, m)
	for _, h := range []string{"Alpha", "Bravo", "Charlie", "Delta"} {
		if got, want := reloaded(t, path, h).Validated, h == "Charlie"; got != want {
			t.Errorf("users.json: %s validated = %v, want %v", h, got, want)
		}
	}
}

// A tag stays on its user when a single F2 delete moves another user to the
// bottom of the list and shifts everyone below it up a row.
func TestTagFollowsItsUserThroughASingleDelete(t *testing.T) {
	m, path := seeded(t, "Alpha", "Bravo", "Charlie", "Delta")
	m = press(t, m, keyOf(tea.KeyDown), keyOf(tea.KeyDown), keyOf(tea.KeySpace)) // tag Charlie
	m = press(t, m, keyOf(tea.KeyUp), keyOf(tea.KeyUp))                          // back to Bravo
	m = press(t, m, keyOf(tea.KeyF2), runesOf("y"), runesOf("n"))                // delete Bravo, don't purge
	if !m.byHandle(t, "Bravo").DeletedUser {
		t.Fatalf("Bravo was not deleted: %q", m.message)
	}
	m = press(t, m, keyOf(tea.KeyF17), runesOf("y"))

	for _, h := range []string{"Alpha", "Bravo", "Charlie", "Delta"} {
		if got, want := m.byHandle(t, h).Validated, h == "Charlie"; got != want {
			t.Errorf("in memory: %s validated = %v, want %v", h, got, want)
		}
	}
	saveAndQuit(t, m)
	for _, h := range []string{"Alpha", "Bravo", "Charlie", "Delta"} {
		if got, want := reloaded(t, path, h).Validated, h == "Charlie"; got != want {
			t.Errorf("users.json: %s validated = %v, want %v", h, got, want)
		}
	}
}

// Mass purge removes every deleted user and leaves the tags on the users
// that remain where they were.
func TestMassPurgeKeepsSurvivingTags(t *testing.T) {
	m, path := seeded(t, "Alpha", "Bravo", "Charlie", "Delta", "Echo")
	// Delete Bravo and Delta (each delete re-sorts, so re-find them).
	m = press(t, m, keyOf(tea.KeyDown), keyOf(tea.KeyF2), runesOf("y"), runesOf("n"))
	m = press(t, m, keyOf(tea.KeyHome), keyOf(tea.KeyDown), keyOf(tea.KeyDown),
		keyOf(tea.KeyF2), runesOf("y"), runesOf("n"))
	// List is now Alpha, Charlie, Echo, Bravo*, Delta*. Tag Echo.
	m = press(t, m, keyOf(tea.KeyHome), keyOf(tea.KeyDown), keyOf(tea.KeyDown), keyOf(tea.KeySpace))
	m = press(t, m, keyOf(tea.KeyF16), runesOf("y"))
	if got := len(m.users); got != 3 {
		t.Fatalf("%d users after mass purge, want 3: %q", got, m.message)
	}
	m = press(t, m, keyOf(tea.KeyF17), runesOf("y"))

	for _, h := range []string{"Alpha", "Charlie", "Echo"} {
		if got, want := m.byHandle(t, h).Validated, h == "Echo"; got != want {
			t.Errorf("in memory: %s validated = %v, want %v", h, got, want)
		}
	}
	saveAndQuit(t, m)
	if got := reloaded(t, path, "Echo"); !got.Validated {
		t.Error("users.json: Echo not validated")
	}
}

// editField opens the edit screen on the user at the cursor and replaces the
// labelled field's value with val.
func editFieldTo(t *testing.T, m Model, label, val string) Model {
	t.Helper()
	m = press(t, m, keyOf(tea.KeyEnter))
	if m.mode != modeEdit {
		t.Fatalf("Enter gave mode %v, want the edit screen", m.mode)
	}
	idx := slices.IndexFunc(m.fields, func(f fieldDef) bool { return f.Label == label })
	if idx < 0 {
		t.Fatalf("no field %q", label)
	}
	m.editField = idx
	m = press(t, m, keyOf(tea.KeyEnter), keyOf(tea.KeyCtrlU), runesOf(val), keyOf(tea.KeyEnter))
	if m.mode != modeEdit {
		t.Fatalf("editing %s left mode %v: %q", label, m.mode, m.message)
	}
	return m
}

// The repro from #456: change Group/Location to "Mars", press F10, and the
// change must be gone, the model clean, and nothing written on exit.
func TestAbortDiscardsTheEdit(t *testing.T) {
	m, path := seeded(t, "Alpha", "Bravo")
	m = press(t, m, keyOf(tea.KeyDown))
	m = editFieldTo(t, m, "Group/Location", "Mars")
	if got := m.byHandle(t, "Bravo").GroupLocation; got != "Mars" {
		t.Fatalf("edit did not take: Group/Location = %q", got)
	}

	m = press(t, m, keyOf(tea.KeyF10))
	if m.mode != modeList {
		t.Fatalf("F10 gave mode %v, want the list", m.mode)
	}
	if got := m.byHandle(t, "Bravo").GroupLocation; got != "Earth" {
		t.Errorf("after Abort, Group/Location = %q, want Earth", got)
	}
	if m.dirty {
		t.Error("model still dirty after aborting its only change")
	}

	// Reopening shows the original, and a later, kept change saves without
	// dragging the aborted one along.
	m = press(t, m, keyOf(tea.KeyEnter))
	if u := m.users[m.editIndex]; u.Handle != "Bravo" || u.GroupLocation != "Earth" {
		t.Errorf("reopened %s shows Group/Location %q, want Bravo / Earth", u.Handle, u.GroupLocation)
	}
	m = press(t, m, keyOf(tea.KeyF10), keyOf(tea.KeyF5), runesOf("y"))
	saveAndQuit(t, m)
	got := reloaded(t, path, "Bravo")
	if got.GroupLocation != "Earth" {
		t.Errorf("users.json: Group/Location = %q, want Earth", got.GroupLocation)
	}
	if !got.Validated {
		t.Error("users.json: the kept validate was not saved")
	}
}

// Abort discards only the current user's edit session: changes kept earlier
// stay, and so does the dirty flag they set.
func TestAbortKeepsEarlierChanges(t *testing.T) {
	m, path := seeded(t, "Alpha", "Bravo")
	m = editFieldTo(t, m, "Group/Location", "Venus")    // Alpha
	m = press(t, m, keyOf(tea.KeyEscape), runesOf("n")) // leave, keep in memory, don't write
	m = press(t, m, keyOf(tea.KeyDown))
	m = editFieldTo(t, m, "Group/Location", "Mars") // Bravo
	m = press(t, m, keyOf(tea.KeyF10))

	if !m.dirty {
		t.Fatal("Abort cleared the dirty flag set by Alpha's kept edit")
	}
	saveAndQuit(t, m)
	if got := reloaded(t, path, "Alpha").GroupLocation; got != "Venus" {
		t.Errorf("users.json: Alpha Group/Location = %q, want Venus", got)
	}
	if got := reloaded(t, path, "Bravo").GroupLocation; got != "Earth" {
		t.Errorf("users.json: Bravo Group/Location = %q, want Earth", got)
	}
}

// Abort restores a key removed in the WFC key manager. Removing a key shifts
// the slice in place, so a snapshot sharing its backing array would be
// rewritten along with it.
func TestAbortRestoresARemovedKey(t *testing.T) {
	keys := []string{authorizedKey(t, "first"), authorizedKey(t, "second")}
	m, _ := editorOver(t, &user.User{ID: 1, Handle: "Alpha", PublicKeys: slices.Clone(keys)})
	m = press(t, m, keyOf(tea.KeyEnter))
	m.editField = slices.IndexFunc(m.fields, func(f fieldDef) bool { return f.Label == "WFC Keys" })
	m = press(t, m, keyOf(tea.KeyEnter), runesOf("d"), keyOf(tea.KeyEscape))
	if got := len(m.users[0].PublicKeys); got != 1 {
		t.Fatalf("%d keys after deleting one, want 1: %q", got, m.keyDialogErr)
	}

	m = press(t, m, keyOf(tea.KeyF10))
	if got := m.users[0].PublicKeys; !slices.Equal(got, keys) {
		t.Errorf("after Abort, keys = %q, want %q", got, keys)
	}
	if m.dirty {
		t.Error("model still dirty after aborting its only change")
	}
}

// authorizedKey returns a fresh ed25519 authorized_keys line.
func authorizedKey(t *testing.T, comment string) string {
	t.Helper()
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	sp, err := ssh.NewPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sp))) + " " + comment
}
