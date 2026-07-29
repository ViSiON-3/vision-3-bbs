package menu

import (
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/editor"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// TestHandleEditorKeyToggleValidatedUnstagesOnDoublePress reproduces a
// pre-existing bug (CodeRabbit PR #138): the 'g' handler derives its toggle
// from !sel.Validated and then compares that negation back against
// sel.Validated, which is always true. That makes the unstage branch dead
// code, so pressing 'g' twice re-stages a no-op "validated" change instead
// of clearing it.
func TestHandleEditorKeyToggleValidatedUnstagesOnDoublePress(t *testing.T) {
	u := &user.User{ID: 2, Handle: "Test", Validated: false}
	st := &userEditorState{
		users:          []*user.User{u},
		pendingChanges: make(map[string]interface{}),
	}

	if _, exit, _, _, err := st.handleEditorKey('g', 80, 24); err != nil || exit {
		t.Fatalf("first toggle: unexpected exit=%v err=%v", exit, err)
	}
	if v, ok := st.pendingChanges["validated"]; !ok || v != true {
		t.Fatalf("after first toggle: pendingChanges[validated] = (%v, %v), want (true, true)", v, ok)
	}

	if _, exit, _, _, err := st.handleEditorKey('g', 80, 24); err != nil || exit {
		t.Fatalf("second toggle: unexpected exit=%v err=%v", exit, err)
	}
	if v, ok := st.pendingChanges["validated"]; ok {
		t.Errorf("after second toggle (back to original): pendingChanges[validated] = %v, want unstaged", v)
	}
	if st.statusMessage != "|08No change.|07" {
		t.Errorf("statusMessage after second toggle = %q, want %q", st.statusMessage, "|08No change.|07")
	}
}

// TestHandleEditorKeyToggleDeletedUnstagesOnDoublePress is the "deleted"
// counterpart of the above (the '9' handler), which had the same bug.
func TestHandleEditorKeyToggleDeletedUnstagesOnDoublePress(t *testing.T) {
	u := &user.User{ID: 2, Handle: "Test", DeletedUser: false}
	st := &userEditorState{
		users:          []*user.User{u},
		pendingChanges: make(map[string]interface{}),
	}

	if _, exit, _, _, err := st.handleEditorKey('9', 80, 24); err != nil || exit {
		t.Fatalf("first toggle: unexpected exit=%v err=%v", exit, err)
	}
	if v, ok := st.pendingChanges["deleted"]; !ok || v != true {
		t.Fatalf("after first toggle: pendingChanges[deleted] = (%v, %v), want (true, true)", v, ok)
	}

	if _, exit, _, _, err := st.handleEditorKey('9', 80, 24); err != nil || exit {
		t.Fatalf("second toggle: unexpected exit=%v err=%v", exit, err)
	}
	if v, ok := st.pendingChanges["deleted"]; ok {
		t.Errorf("after second toggle (back to original): pendingChanges[deleted] = %v, want unstaged", v)
	}
	if st.statusMessage != "|08No change.|07" {
		t.Errorf("statusMessage after second toggle = %q, want %q", st.statusMessage, "|08No change.|07")
	}
}

// TestHandleEditorKeyEscWarnsOnUnsavedChangesOutsidePendingOnly reproduces a
// pre-existing bug (CodeRabbit PR #138): Esc only guarded unsaved changes
// when cfg.pendingOnly was set, so in the normal (non-pendingOnly) editor,
// Esc silently discarded staged edits instead of warning like Q does.
func TestHandleEditorKeyEscWarnsOnUnsavedChangesOutsidePendingOnly(t *testing.T) {
	u := &user.User{ID: 2, Handle: "Test"}
	st := &userEditorState{
		users:          []*user.User{u},
		pendingChanges: map[string]interface{}{"handle": "Changed"},
		cfg:            userEditorConfig{pendingOnly: false},
	}

	_, exit, _, _, err := st.handleEditorKey(editor.KeyEsc, 80, 24)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if exit {
		t.Fatal("Esc with unsaved changes should not exit the editor")
	}
	if st.statusMessage != "|11Unsaved changes! Press [S] to save or [X] to abort.|07" {
		t.Errorf("statusMessage = %q, want the unsaved-changes warning", st.statusMessage)
	}
	if len(st.pendingChanges) != 1 {
		t.Errorf("pendingChanges = %v, want unchanged (discard requires explicit X)", st.pendingChanges)
	}
}
