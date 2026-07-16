package configeditor

import tea "github.com/charmbracelet/bubbletea"

// listNavKey applies Up/Down/Home/End navigation to a cursor over total
// items. It returns the new cursor position and whether the key was a
// navigation key (callers handle all other keys themselves).
func listNavKey(msg tea.KeyMsg, cursor, total int) (int, bool) {
	switch msg.Type {
	case tea.KeyUp:
		if cursor > 0 {
			cursor--
		}
		return cursor, true
	case tea.KeyDown:
		if cursor < total-1 {
			cursor++
		}
		return cursor, true
	case tea.KeyHome:
		return 0, true
	case tea.KeyEnd:
		if total > 0 {
			cursor = total - 1
		}
		return cursor, true
	}
	return cursor, false
}

// clampListScroll adjusts a scroll offset so the cursor stays inside the
// visible window of the given height.
func clampListScroll(cursor, scroll, visible int) int {
	if cursor < scroll {
		scroll = cursor
	}
	if cursor >= scroll+visible {
		scroll = cursor - visible + 1
	}
	if scroll < 0 {
		scroll = 0
	}
	return scroll
}
