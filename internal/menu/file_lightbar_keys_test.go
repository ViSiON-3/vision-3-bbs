package menu

import (
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/editor"
)

// TestHandleNavKey_EnterOnEmptyCmdBarDoesNotPanic pins the bounds-guard fix:
// pressing Enter used to index lb.cmdEntries[lb.cmdIndex] unconditionally,
// which panics if the command bar has no entries (e.g. a misconfigured BAR
// file leaves cmdEntries empty). handleNavKey must instead report "not
// dispatched" so run() just loops around.
func TestHandleNavKey_EnterOnEmptyCmdBarDoesNotPanic(t *testing.T) {
	lb := &fileLightbar{cmdEntries: nil, cmdIndex: 0}

	key, dispatch, exit, result, action, err := lb.handleNavKey(editor.KeyEnter)

	if dispatch {
		t.Errorf("dispatch = true, want false (nothing to dispatch on an empty command bar)")
	}
	if exit || result != nil || action != "" || err != nil {
		t.Errorf("(exit, result, action, err) = (%v, %v, %q, %v), want (false, nil, \"\", nil)", exit, result, action, err)
	}
	if key != "" {
		t.Errorf("key = %q, want empty", key)
	}
}

// TestHandleNavKey_EnterOnNonEmptyCmdBarStillWorks pins that the guard only
// affects the empty case: Enter still selects the entry at cmdIndex.
func TestHandleNavKey_EnterOnNonEmptyCmdBarStillWorks(t *testing.T) {
	lb := &fileLightbar{
		cmdEntries: []cmdEntry{{label: "Mark", hotkey: " "}, {label: "Quit", hotkey: "q"}},
		cmdIndex:   1,
	}

	key, dispatch, exit, _, _, _ := lb.handleNavKey(editor.KeyEnter)

	if !dispatch || exit {
		t.Errorf("(dispatch, exit) = (%v, %v), want (true, false)", dispatch, exit)
	}
	if key != "q" {
		t.Errorf("key = %q, want %q", key, "q")
	}
}
