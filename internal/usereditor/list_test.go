package usereditor

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// manyHandles returns n distinct handles, enough to overflow the list box.
func manyHandles(n int) []string {
	hs := make([]string, n)
	for i := range hs {
		hs[i] = fmt.Sprintf("User%02d", i+1)
	}
	return hs
}

// Init names the terminal window; it is the only command the editor issues at
// startup.
func TestInitSetsTheWindowTitle(t *testing.T) {
	m := listing(t, "Alice")
	if m.Init() == nil {
		t.Fatal("Init returned no command")
	}
}

// A resize smaller than the editor's minimum is clamped rather than honoured,
// so the fixed-size boxes never draw off-screen.
func TestWindowSizeClampsToTheMinimum(t *testing.T) {
	m := press(t, listing(t, "Alice"), tea.WindowSizeMsg{Width: 40, Height: 10})
	if m.width != minWidth || m.height != minHeight {
		t.Errorf("size = %dx%d, want %dx%d", m.width, m.height, minWidth, minHeight)
	}
	m = press(t, m, tea.WindowSizeMsg{Width: 120, Height: 45})
	if m.width != 120 || m.height != 45 {
		t.Errorf("size = %dx%d, want 120x45", m.width, m.height)
	}
}

// The cursor keys move the lightbar and stop at the ends of the list; the
// page keys move by a screenful and clamp; the list scrolls to keep the
// cursor visible.
func TestListCursorKeys(t *testing.T) {
	m := listing(t, manyHandles(30)...)

	m = press(t, m, key(tea.KeyUp))
	if m.cursor != 0 {
		t.Errorf("Up at the top moved to %d", m.cursor)
	}
	m = press(t, m, key(tea.KeyDown), key(tea.KeyDown))
	if m.cursor != 2 {
		t.Errorf("Down x2 = %d, want 2", m.cursor)
	}
	m = press(t, m, key(tea.KeyPgDown))
	if m.cursor != 2+listVisible {
		t.Errorf("PgDn = %d, want %d", m.cursor, 2+listVisible)
	}
	if m.scrollOffset == 0 {
		t.Error("list did not scroll to follow the cursor past the visible window")
	}
	m = press(t, m, key(tea.KeyPgDown), key(tea.KeyPgDown))
	if m.cursor != 29 {
		t.Errorf("PgDn past the end = %d, want 29", m.cursor)
	}
	m = press(t, m, key(tea.KeyDown))
	if m.cursor != 29 {
		t.Errorf("Down at the bottom moved to %d", m.cursor)
	}
	if !strings.Contains(stripANSIGolden(m.View()), "User30") {
		t.Error("the last user is not on screen with the cursor on it")
	}
	m = press(t, m, key(tea.KeyPgUp))
	if m.cursor != 29-listVisible {
		t.Errorf("PgUp = %d, want %d", m.cursor, 29-listVisible)
	}
	m = press(t, m, key(tea.KeyPgUp), key(tea.KeyPgUp))
	if m.cursor != 0 {
		t.Errorf("PgUp past the top = %d, want 0", m.cursor)
	}
	m = press(t, m, key(tea.KeyEnd))
	if m.cursor != 29 {
		t.Errorf("End = %d, want 29", m.cursor)
	}
	m = press(t, m, key(tea.KeyHome))
	if m.cursor != 0 || m.scrollOffset != 0 {
		t.Errorf("Home = cursor %d offset %d, want 0 0", m.cursor, m.scrollOffset)
	}
}

// Left and Right cycle the list's data columns between the four views and
// stop at either end; the column header follows.
func TestListColumnViews(t *testing.T) {
	m := listing(t, "Alice")
	want := []string{"Level", "Group/Location", "Posts", "Last Date"}

	for i, w := range want {
		if m.listType != i+1 {
			t.Fatalf("listType = %d, want %d", m.listType, i+1)
		}
		if !strings.Contains(stripANSIGolden(m.View()), w) {
			t.Errorf("view %d does not show the %q column", i+1, w)
		}
		m = press(t, m, key(tea.KeyRight))
	}
	if m.listType != 4 {
		t.Errorf("Right past the last view = %d, want 4", m.listType)
	}
	for i := 0; i < 5; i++ {
		m = press(t, m, key(tea.KeyLeft))
	}
	if m.listType != 1 {
		t.Errorf("Left past the first view = %d, want 1", m.listType)
	}
}

// Each column view renders its own data for the user rows.
func TestListColumnViewsRenderUserData(t *testing.T) {
	m, _ := editorOver(t, &user.User{
		ID: 1, Handle: "Alice", AccessLevel: 77, TimesCalled: 1234,
		GroupLocation: "Nowhere", MessagesPosted: 55, Validated: true,
	})
	m = press(t, m, tea.WindowSizeMsg{Width: 80, Height: 25})
	for i, want := range []string{"1234", "Nowhere", "55", "Never"} {
		if got := stripANSIGolden(m.View()); !strings.Contains(got, want) {
			t.Errorf("view %d does not show %q", i+1, want)
		}
		m = press(t, m, key(tea.KeyRight))
	}
}

// Space toggles a tag on the highlighted user and advances; F10 tags everyone.
// A tagged row is marked with an asterisk.
func TestTaggingUsers(t *testing.T) {
	m := listing(t, "Alice", "Bob", "Carol")

	m = press(t, m, key(tea.KeySpace))
	if !m.tagged[0] || m.cursor != 1 {
		t.Fatalf("Space: tagged=%v cursor=%d, want user 0 tagged and cursor 1", m.tagged, m.cursor)
	}
	if !strings.Contains(stripANSIGolden(m.View()), "*  1 Alice") {
		t.Error("tagged row is not marked with an asterisk")
	}
	m = press(t, m, key(tea.KeyUp), key(tea.KeySpace))
	if m.tagged[0] {
		t.Error("second Space did not untag")
	}
	if m.taggedCount() != 0 {
		t.Errorf("taggedCount = %d, want 0", m.taggedCount())
	}

	m = press(t, m, key(tea.KeyEnd), key(tea.KeySpace))
	if m.cursor != 2 || !m.tagged[2] {
		t.Error("Space on the last row must tag it and stay put")
	}

	m = press(t, m, key(tea.KeyF10))
	if m.taggedCount() != 3 {
		t.Errorf("F10 tagged %d users, want 3", m.taggedCount())
	}
}

// F3 toggles alphabetical order and back to ID order, resetting the cursor
// and saying which it did.
func TestF3TogglesAlphabeticalOrder(t *testing.T) {
	m := listing(t, "Zed", "Amy", "Mo")
	m = press(t, m, key(tea.KeyDown), key(tea.KeyF3))

	if got := ids(m.users); fmt.Sprint(got) != "[2 3 1]" {
		t.Errorf("alpha order ids = %v, want [2 3 1]", got)
	}
	if m.cursor != 0 || !strings.Contains(m.message, "Alphabetizing") {
		t.Errorf("cursor=%d message=%q", m.cursor, m.message)
	}

	m = press(t, m, key(tea.KeyF3))
	if got := ids(m.users); fmt.Sprint(got) != "[1 2 3]" {
		t.Errorf("ID order ids = %v, want [1 2 3]", got)
	}
	if !strings.Contains(m.message, "Restoring") {
		t.Errorf("message = %q", m.message)
	}
}

// Alt-H opens the help overlay and any key dismisses it.
func TestAltHOpensHelp(t *testing.T) {
	m := listing(t, "Alice")
	m = press(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h'}, Alt: true})
	if m.mode != modeHelp {
		t.Fatalf("mode = %v, want modeHelp", m.mode)
	}
	if !strings.Contains(stripANSIGolden(m.View()), "V/3 User Editor Help") {
		t.Error("help overlay is not drawn")
	}
	m = press(t, m, char('x'))
	if m.mode != modeList {
		t.Errorf("a key did not dismiss help, mode = %v", m.mode)
	}
}

// Escape with nothing changed asks a plain "exit?" question: No stays in the
// editor, Yes quits.
func TestEscapeWithNoChangesConfirmsExit(t *testing.T) {
	m := listing(t, "Alice")
	m = press(t, m, key(tea.KeyEscape))
	if m.mode != modeExitClean {
		t.Fatalf("mode = %v, want modeExitClean", m.mode)
	}
	if !strings.Contains(stripANSIGolden(m.View()), "Exit user editor?") {
		t.Error("exit prompt is not drawn")
	}

	m, cmd := send(t, m, char('n'))
	if m.mode != modeList || quits(cmd) {
		t.Fatalf("No: mode=%v quit=%v, want back on the list", m.mode, quits(cmd))
	}

	m = press(t, m, key(tea.KeyEscape))
	_, cmd = send(t, m, char('y'))
	if !quits(cmd) {
		t.Error("Yes did not quit")
	}
}

// With unsaved edits, Escape asks to save; answering No quits and leaves the
// file untouched.
func TestEscapeWithChangesAndNoDiscards(t *testing.T) {
	m, path := editorOver(t, &user.User{ID: 1, Handle: "Alice", AccessLevel: 10})
	m = press(t, m, key(tea.KeyF5), char('y')) // validate: level becomes 10, file points 100
	if !m.dirty {
		t.Fatal("validation did not mark the model dirty")
	}

	m = press(t, m, key(tea.KeyEscape))
	if m.mode != modeExitConfirm {
		t.Fatalf("mode = %v, want modeExitConfirm", m.mode)
	}
	if !strings.Contains(stripANSIGolden(m.View()), "Save changes before exit?") {
		t.Error("save prompt is not drawn")
	}
	_, cmd := send(t, m, char('n'))
	if !quits(cmd) {
		t.Fatal("No did not quit")
	}
	got, _ := reloadUser(t, path, 1)
	if got.FilePoints != 0 {
		t.Errorf("discarded change reached disk: file points = %d", got.FilePoints)
	}
}

// Escape from a confirm dialog backs out of it without acting, and Left/Right
// move the Yes/No selection that Enter then acts on.
func TestConfirmDialogEscapeAndButtonToggle(t *testing.T) {
	m := listing(t, "Alice", "Bob")
	m = press(t, m, key(tea.KeyDown), key(tea.KeyF2))
	if m.mode != modeDeleteConfirm || m.confirmYes {
		t.Fatalf("F2: mode=%v confirmYes=%v, want delete prompt defaulting to No", m.mode, m.confirmYes)
	}
	m = press(t, m, key(tea.KeyEscape))
	if m.mode != modeList || m.users[1].DeletedUser {
		t.Fatalf("Escape: mode=%v deleted=%v, want list and nothing deleted", m.mode, m.users[1].DeletedUser)
	}

	// Enter on the default No does nothing either.
	m = press(t, m, key(tea.KeyF2), key(tea.KeyEnter))
	if m.mode != modeList || m.users[1].DeletedUser {
		t.Fatal("Enter on No deleted the user")
	}

	// Right moves to Yes; Enter then acts.
	m = press(t, m, key(tea.KeyF2), key(tea.KeyRight))
	if !m.confirmYes {
		t.Fatal("Right did not move to Yes")
	}
	m = press(t, m, key(tea.KeyEnter))
	if !m.users[len(m.users)-1].DeletedUser {
		t.Error("Enter on Yes did not delete")
	}
}

// User 1 is the sysop and cannot be deleted or purged from the list; the
// editor says so in an alert that any key dismisses.
func TestUserOneIsProtectedOnTheList(t *testing.T) {
	for _, k := range []tea.KeyType{tea.KeyF2, tea.KeyF4} {
		m := listing(t, "Sysop", "Bob")
		m = press(t, m, key(k))
		if m.mode != modeInfoAlert {
			t.Fatalf("%v on user 1: mode = %v, want modeInfoAlert", k, m.mode)
		}
		if !strings.Contains(stripANSIGolden(m.View()), "User 1") {
			t.Errorf("%v alert does not explain the refusal", k)
		}
		m = press(t, m, char('q'))
		if m.mode != modeList {
			t.Errorf("alert dismissal returned to %v, want modeList", m.mode)
		}
		if m.users[0].DeletedUser {
			t.Error("user 1 was deleted")
		}
	}
}

// F4 on a live user refuses: a user must be deleted before they can be purged.
func TestF4RefusesToPurgeALiveUser(t *testing.T) {
	m := listing(t, "Sysop", "Bob")
	m = press(t, m, key(tea.KeyDown), key(tea.KeyF4))
	if m.mode != modeInfoAlert || !strings.Contains(m.alertMessage, "must be deleted") {
		t.Fatalf("mode=%v alert=%q", m.mode, m.alertMessage)
	}
	if len(m.users) != 2 {
		t.Error("a live user was purged")
	}
}

// An empty user file shows an empty list; only Escape does anything, and it
// quits directly.
func TestEmptyListOnlyEscapes(t *testing.T) {
	m, _ := editorOver(t)
	m, cmd := send(t, m, key(tea.KeyEnter))
	if m.mode != modeList || cmd != nil {
		t.Errorf("Enter on an empty list: mode=%v", m.mode)
	}
	_, cmd = send(t, m, key(tea.KeyEscape))
	if !quits(cmd) {
		t.Error("Escape on an empty list did not quit")
	}
}
