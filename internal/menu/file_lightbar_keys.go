package menu

import (
	"io"
	"strings"

	"github.com/ViSiON-3/vision-3-bbs/internal/editor"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// fileLightbar key handling: reading input, navigation, the sysop-bar
// toggle, and the command dispatch switch.

// readKeyOrLogoff reads the next key from lb.ih, mapping a disconnect or
// idle timeout to the "LOGOFF" result convention documented on
// runListFilesLightbar. exit is true whenever run() should return
// immediately (either a real read error or the LOGOFF mapping); keyInt is
// only meaningful when exit is false.
func (lb *fileLightbar) readKeyOrLogoff() (keyInt int, exit bool, action string, err error) {
	keyInt, readErr := lb.ih.ReadKey()
	if readErr != nil {
		if logoffIfDisconnected(readErr) {
			return 0, true, "LOGOFF", io.EOF
		}
		return 0, true, "", readErr
	}
	return keyInt, false, "", nil
}

// handleNavKey processes one key code read by run()'s main loop. It reports
// whether run() should exit immediately (exit, with result/action/err as the
// values run() should return — used for bare Esc); otherwise dispatch tells
// run() whether key was set to a command that the command switch
// (dispatchCommand) should handle. Pure navigation keys (arrows, paging,
// Home/End) mutate lb's selection/viewport/cmdIndex fields directly and
// report dispatch=false so run() simply loops around, matching the
// `continue` behavior of the original inline switch.
func (lb *fileLightbar) handleNavKey(keyInt int) (key string, dispatch bool, exit bool, result *user.User, action string, err error) {
	// Navigation keys — matched directly on integer key codes so that
	// multi-byte escape sequences (PageUp/PageDown etc.) are handled
	// atomically by the InputHandler and can never be split by the
	// 100 ms inter-byte ESC timeout, which previously caused bare ESC
	// to be returned and the lightbar to exit unexpectedly.
	switch keyInt {
	case editor.KeyArrowUp: // Up
		lb.selectedIndex--
		return "", false, false, nil, "", nil
	case editor.KeyArrowDown: // Down
		lb.selectedIndex++
		return "", false, false, nil, "", nil
	case editor.KeyArrowRight: // Right — command bar
		lb.cmdIndex++
		if lb.cmdIndex >= len(lb.cmdEntries) {
			lb.cmdIndex = 0
		}
		return "", false, false, nil, "", nil
	case editor.KeyArrowLeft: // Left — command bar
		lb.cmdIndex--
		if lb.cmdIndex < 0 {
			lb.cmdIndex = len(lb.cmdEntries) - 1
		}
		return "", false, false, nil, "", nil
	case editor.KeyPageUp, editor.KeyCtrlR: // Page Up
		newTop := lb.topIndexForPrevPage()
		lb.topIndex = newTop
		lb.selectedIndex = newTop
		return "", false, false, nil, "", nil
	case editor.KeyPageDown: // Page Down
		count := lb.filesVisibleFrom(lb.topIndex)
		nextTop := lb.topIndex + count
		if nextTop >= len(lb.allFiles) {
			if len(lb.allFiles) > 0 {
				lb.selectedIndex = len(lb.allFiles) - 1
			}
		} else {
			lb.topIndex = nextTop
			lb.selectedIndex = nextTop
		}
		return "", false, false, nil, "", nil
	case editor.KeyHome: // Home
		lb.selectedIndex = 0
		return "", false, false, nil, "", nil
	case editor.KeyEnd: // End
		if len(lb.allFiles) > 0 {
			lb.selectedIndex = len(lb.allFiles) - 1
		}
		return "", false, false, nil, "", nil
	case editor.KeyEsc: // Bare Esc
		return "", false, true, nil, "", nil
	case editor.KeyEnter: // Enter: execute selected command bar item
		return lb.cmdEntries[lb.cmdIndex].hotkey, true, false, nil, "", nil
	default:
		if keyInt >= 32 && keyInt < 127 {
			return strings.ToLower(string(rune(keyInt))), true, false, nil, "", nil
		}
		return "", false, false, nil, "", nil // Ignore non-printable, non-navigation keys
	}
}

// toggleSysopBar flips whether the sysop-only command bar entries are
// shown, swapping cmdEntries between the sysop and user sets and resetting
// cmdIndex to the first entry.
func (lb *fileLightbar) toggleSysopBar() {
	lb.showSysopBar = !lb.showSysopBar
	if lb.showSysopBar {
		lb.cmdEntries = make([]cmdEntry, len(lb.sysopEntries))
		copy(lb.cmdEntries, lb.sysopEntries)
	} else {
		lb.cmdEntries = make([]cmdEntry, len(lb.userEntries))
		copy(lb.cmdEntries, lb.userEntries)
	}
	lb.cmdIndex = 0
}

// dispatchCommand executes the hotkey or Enter-selected command bar item in
// key: mark toggle, quit, info overlay, view, download, upload, and the
// sysop-only edit/kill/move/rename commands. It follows the (exit, result,
// action, err) signal convention documented on handleNavKey: false/nil/""/nil
// means "handled, run() should loop around". Every bare `continue` in the
// original inline switch becomes `return false, nil, "", nil` here, because
// the switch was the last statement in run()'s for loop — continuing was
// already equivalent to falling out of the switch. continue/break statements
// inside a case's own nested for/switch are untouched: they still target
// that inner loop, not run()'s.
func (lb *fileLightbar) dispatchCommand(key string, frame *lbFrame) (exit bool, result *user.User, action string, err error) {
	switch key {
	case " ": // Space: toggle mark
		lb.toggleMark()

	case "q":
		return true, nil, "", nil

	case "i": // Info: show file detail overlay
		refresh, infoExit, infoResult, infoAction, infoErr := lb.showFileInfo()
		if infoExit {
			return true, infoResult, infoAction, infoErr
		}
		if refresh {
			frame.needFullRedraw = true
		}

	case "v":
		if lb.viewFile() {
			frame.needFullRedraw = true
		}

	case "d":
		return lb.downloadFiles(frame)

	case "u":
		lb.uploadFiles(frame)

	case "e": // Edit description (sysop only)
		if !lb.isSysop || len(lb.allFiles) == 0 {
			return false, nil, "", nil
		}
		lb.editDescription(frame)

	case "k": // Kill/delete file (sysop only)
		if !lb.isSysop || len(lb.allFiles) == 0 {
			return false, nil, "", nil
		}
		return lb.killFile(frame)

	case "m": // Move file to another area (sysop only)
		if !lb.isSysop || len(lb.allFiles) == 0 {
			return false, nil, "", nil
		}
		return lb.moveFile(frame)

	case "r": // Rename file on disk (sysop only)
		if !lb.isSysop || len(lb.allFiles) == 0 {
			return false, nil, "", nil
		}
		return lb.renameFile(frame)
	}
	return false, nil, "", nil
}
