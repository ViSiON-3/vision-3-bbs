package menueditor

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// TestViewSmokeAllModes checks that View renders non-empty output in every
// editor mode.
func TestViewSmokeAllModes(t *testing.T) {
	m := newTestEditor(t)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = asModel(t, updated)

	modes := []editorMode{
		modeMenuList, modeMenuEdit, modeDeleteMenuConfirm, modeAddMenu,
		modeCommandList, modeCommandEdit, modeExitConfirm, modeHelp,
	}
	for _, mode := range modes {
		mm := m
		mm.mode = mode
		if mode == modeCommandList || mode == modeCommandEdit {
			mm.cmds = []CmdData{{Keys: "Q", Command: "LOGOFF"}}
		}
		if out := mm.View(); out == "" {
			t.Errorf("View() in mode %v returned empty output", mode)
		}
	}
}
