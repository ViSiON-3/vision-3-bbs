package menu

import (
	"github.com/ViSiON-3/vision-3-bbs/internal/editor"
)

// runMessageListNavigation handles keyboard input for list navigation
func runMessageListNavigation(ih *editor.InputHandler, state *MessageListState) (action string, selectedMsg int, err error) {
	for {
		key, err := ih.ReadKey()
		if err != nil {
			return "ERROR", 0, err
		}

		// Calculate total pages
		totalPages := (state.TotalMessages + state.ItemsPerPage - 1) / state.ItemsPerPage
		if totalPages < 1 {
			totalPages = 1
		}

		// Calculate current page boundaries
		start, end := calculatePagination(len(state.Entries), state.ItemsPerPage, state.CurrentPage)
		itemsOnPage := end - start

		switch key {
		case editor.KeyArrowUp, editor.KeyCtrlE: // Up arrow
			if state.SelectedIndex > 0 {
				state.SelectedIndex--
				return "REFRESH_LINE", 0, nil
			} else if state.CurrentPage > 1 {
				// Move to previous page, select last item
				state.CurrentPage--
				start, end := calculatePagination(len(state.Entries), state.ItemsPerPage, state.CurrentPage)
				state.SelectedIndex = (end - start) - 1
				return "REFRESH_FULL", 0, nil
			}

		case editor.KeyArrowDown, editor.KeyCtrlX: // Down arrow
			if state.SelectedIndex < itemsOnPage-1 {
				state.SelectedIndex++
				return "REFRESH_LINE", 0, nil
			} else if state.CurrentPage < totalPages {
				// Move to next page, select first item
				state.CurrentPage++
				state.SelectedIndex = 0
				return "REFRESH_FULL", 0, nil
			}

		case editor.KeyPageUp, editor.KeyCtrlR: // Page Up
			if state.CurrentPage > 1 {
				state.CurrentPage--
				state.SelectedIndex = 0
				return "REFRESH_FULL", 0, nil
			}

		case editor.KeyPageDown, editor.KeyCtrlC: // Page Down
			if state.CurrentPage < totalPages {
				state.CurrentPage++
				state.SelectedIndex = 0
				return "REFRESH_FULL", 0, nil
			}

		case editor.KeyHome, editor.KeyCtrlW: // Home
			if state.CurrentPage != 1 || state.SelectedIndex != 0 {
				state.CurrentPage = 1
				state.SelectedIndex = 0
				return "REFRESH_FULL", 0, nil
			}

		case editor.KeyEnd, editor.KeyCtrlP: // End
			lastPage := totalPages
			state.CurrentPage = lastPage
			start, end := calculatePagination(len(state.Entries), state.ItemsPerPage, state.CurrentPage)
			state.SelectedIndex = (end - start) - 1
			return "REFRESH_FULL", 0, nil

		case editor.KeyEnter: // Enter - read selected message
			actualIndex := start + state.SelectedIndex
			if actualIndex < len(state.Entries) {
				return "READ", state.Entries[actualIndex].MsgNum, nil
			}

		case 'Q', 'q': // Quit
			return "QUIT", 0, nil

		case '?': // Help (future enhancement)
			// TODO: Show help screen
			return "REFRESH_FULL", 0, nil
		}
	}
}
