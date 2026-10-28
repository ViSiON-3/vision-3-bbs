package menu

import (
	"github.com/ViSiON-3/vision-3-bbs/internal/editor"
)

// runMessageListNavigation handles keyboard input for list navigation.
//
// The cursor only ever rests on a message: in threaded mode a deleted
// placeholder row is stepped over, whichever key is moving and whether or
// not that crosses a page. Actions: REFRESH_LINE (cursor moved within the
// page), REFRESH_FULL (page changed), READ with the message number, TOGGLE
// (T: switch flat/threaded), QUIT (Q, Esc or V) and ERROR.
func runMessageListNavigation(ih *editor.InputHandler, state *MessageListState) (action string, selectedMsg int, err error) {
	// moveTo puts the cursor on absolute index i, or leaves it when i is
	// out of range, and names the redraw that needs.
	moveTo := func(i int) (string, bool) {
		if i < 0 || i >= len(state.Entries) {
			return "", false
		}
		if state.setSelected(i) {
			return "REFRESH_FULL", true
		}
		return "REFRESH_LINE", true
	}
	for {
		key, err := ih.ReadKey()
		if err != nil {
			return "ERROR", 0, err
		}

		totalPages := state.totalPages()
		cur := state.selectedAbsolute()

		switch key {
		case editor.KeyArrowUp, editor.KeyCtrlE: // Up arrow
			if a, ok := moveTo(state.nextSelectable(cur-1, -1)); ok {
				return a, 0, nil
			}

		case editor.KeyArrowDown, editor.KeyCtrlX: // Down arrow
			if a, ok := moveTo(state.nextSelectable(cur+1, 1)); ok {
				return a, 0, nil
			}

		case editor.KeyPageUp, editor.KeyCtrlR: // Page Up: first message of the previous page
			if state.CurrentPage > 1 {
				start := (state.CurrentPage - 2) * state.ItemsPerPage
				i := state.nextSelectable(start, 1)
				if i >= start+state.ItemsPerPage {
					// That page holds only placeholders: go to the first
					// message of the nearest page above with one, rather
					// than to a message on the page being left.
					if above := state.nextSelectable(start-1, -1); above >= 0 {
						i = state.nextSelectable(above/state.ItemsPerPage*state.ItemsPerPage, 1)
					} else {
						i = -1
					}
				}
				if i >= 0 {
					state.setSelected(i)
					return "REFRESH_FULL", 0, nil
				}
			}

		case editor.KeyPageDown, editor.KeyCtrlC: // Page Down: first message of the next page
			if state.CurrentPage < totalPages {
				start := state.CurrentPage * state.ItemsPerPage
				if i := state.nextSelectable(start, 1); i >= 0 {
					state.setSelected(i)
					return "REFRESH_FULL", 0, nil
				}
			}

		case editor.KeyHome, editor.KeyCtrlW: // Home
			if i := state.nextSelectable(0, 1); i >= 0 && i != cur {
				state.setSelected(i)
				return "REFRESH_FULL", 0, nil
			}

		case editor.KeyEnd, editor.KeyCtrlP: // End
			if i := state.nextSelectable(len(state.Entries)-1, -1); i >= 0 {
				state.setSelected(i)
				return "REFRESH_FULL", 0, nil
			}

		case editor.KeyEnter: // Enter - read selected message
			if state.selectable(cur) {
				return "READ", state.Entries[cur].MsgNum, nil
			}

		case 'T', 't': // Switch between flat and threaded
			return "TOGGLE", 0, nil

		case 'Q', 'q', 'V', 'v', editor.KeyEsc: // Quit (back to the reader, when it opened the list)
			return "QUIT", 0, nil

		case '?': // Help (future enhancement)
			// TODO: Show help screen
			return "REFRESH_FULL", 0, nil
		}
	}
}
