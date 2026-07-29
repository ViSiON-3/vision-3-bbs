package menu

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/editor"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// handleEditorKey processes a single key press read by runUserEditor's main
// loop. It reports whether the caller should redraw (refresh) and whether it
// should return immediately (exit, with result/action/err as the values
// runUserEditor should return). This is a verbatim extraction of the former
// main-loop switch; all field mutations (pendingChanges, statusMessage, etc.)
// happen directly on st exactly as they did in the inline switch.
func (st *userEditorState) handleEditorKey(key int, termWidth, termHeight int) (refresh bool, exit bool, result *user.User, action string, err error) {
	switch key {
	case 'k', 'K', 'w', 'W':
		if len(st.pendingChanges) == 0 {
			st.moveUp()
			refresh = true
		}
	case 'j', 'J':
		if len(st.pendingChanges) == 0 {
			st.moveDown()
			refresh = true
		}
	case 's', 'S':
		if len(st.pendingChanges) > 0 {
			target := st.users[st.selectedIndex]
			var saved bool
			st.statusMessage, saved = st.e.applyPendingUserChanges(st.userManager, st.currentUser, target, st.pendingChanges, st.originalTimestamps)
			if saved {
				st.pendingChanges = make(map[string]interface{})
				if reloadExit, msg, reloadErr := st.postSaveReload(termWidth, termHeight); reloadExit {
					return false, true, nil, msg, reloadErr
				}
			}
			refresh = true
		} else {
			st.moveDown()
			refresh = true
		}
	case 'q', 'Q':
		if len(st.pendingChanges) > 0 {
			st.statusMessage = "|11Unsaved changes! Press [S] to save or [X] to abort.|07"
		} else {
			return false, true, nil, "", nil
		}
	case 'x', 'X':
		if len(st.pendingChanges) > 0 {
			st.pendingChanges = make(map[string]interface{})
			st.statusMessage = "|11Changes discarded.|07"
			refresh = true
		}
	case 'a', 'A':
		sel := st.users[st.selectedIndex]
		if newVal, editErr := st.readFieldInput("Handle", sel.Handle, 30, false); editErr == nil {
			trimmedHandle := strings.TrimSpace(newVal)
			if trimmedHandle != sel.Handle {
				st.pendingChanges["handle"] = trimmedHandle
				st.statusMessage = "|10Field marked for update.|07"
			} else {
				delete(st.pendingChanges, "handle")
				st.statusMessage = "|08No change.|07"
			}
			refresh = true
		} else {
			if editErr.Error() != "cancelled" {
				st.statusMessage = fmt.Sprintf("|01Error: %v|07", editErr)
			}
			refresh = true
		}
	case 'b', 'B':
		// Edit Real Name field
		sel := st.users[st.selectedIndex]
		st.stageFieldEdit("realname", "Real Name", sel.RealName, 50)
		refresh = true
	case 'c', 'C':
		sel := st.users[st.selectedIndex]
		st.stageFieldEdit("grouploc", "Group/Location", sel.GroupLocation, 30)
		refresh = true
	case 'd', 'D':
		sel := st.users[st.selectedIndex]
		st.stageFieldEdit("note", "Note", sel.PrivateNote, 50)
		refresh = true
	case 'e', 'E':
		sel := st.users[st.selectedIndex]
		st.stageFieldEdit("flags", "Flags", sel.Flags, 20)
		refresh = true
	case 'f', 'F':
		sel := st.users[st.selectedIndex]
		levelStr := fmt.Sprintf("%d", sel.AccessLevel)
		if newVal, editErr := st.readFieldInput("Level", levelStr, 3, false); editErr == nil {
			if level, parseErr := strconv.Atoi(newVal); parseErr == nil {
				// Protect User #1 from level reduction
				if sel.ID == 1 && level < st.e.ServerCfg.SysOpLevel {
					st.statusMessage = "|01Cannot lower User #1 below SysOp level!|07"
					refresh = true
				} else if level != sel.AccessLevel {
					st.pendingChanges["level"] = level
					st.statusMessage = "|10Field marked for update.|07"
					refresh = true
				} else {
					delete(st.pendingChanges, "level")
					st.statusMessage = "|08No change.|07"
					refresh = true
				}
			} else {
				st.statusMessage = "|01Invalid number.|07"
				refresh = true
			}
		} else {
			if editErr.Error() != "cancelled" {
				st.statusMessage = fmt.Sprintf("|01Error: %v|07", editErr)
			}
			refresh = true
		}
	case 'g', 'G':
		// Toggle validated status
		sel := st.users[st.selectedIndex]
		if sel.ID == 1 && sel.Validated {
			// Don't allow unvalidating User #1
			st.statusMessage = "|01Cannot unvalidate User #1!|07"
			refresh = true
		} else {
			cur := sel.Validated
			if staged, ok := st.pendingChanges["validated"].(bool); ok {
				cur = staged
			}
			newValidated := !cur
			if newValidated != sel.Validated {
				st.pendingChanges["validated"] = newValidated
				if newValidated {
					st.statusMessage = "|10Validated status marked for update.|07"
				} else {
					st.statusMessage = "|11Unvalidated status marked for update.|07"
				}
			} else {
				delete(st.pendingChanges, "validated")
				st.statusMessage = "|08No change.|07"
			}
			refresh = true
		}
	case 'p', 'P':
		// Change password
		if newPassword, editErr := st.readFieldInput("New Password", "", 50, true); editErr == nil {
			if newPassword != "" {
				st.pendingChanges["password"] = newPassword
				st.statusMessage = "|10Password marked for update.|07"
			} else {
				delete(st.pendingChanges, "password")
				st.statusMessage = "|08Password change cancelled.|07"
			}
			refresh = true
		} else {
			if editErr.Error() != "cancelled" {
				st.statusMessage = fmt.Sprintf("|01Error: %v|07", editErr)
			}
			refresh = true
		}
	case '0':
		// Toggle ban user (sets level 0, unvalidated) or unban (restore to regular level)
		sel := st.users[st.selectedIndex]
		if sel.ID == 1 {
			st.statusMessage = "|01Cannot ban User #1!|07"
		} else {
			// Check if user is currently banned
			isBanned := sel.AccessLevel == 0 && !sel.Validated
			if isBanned {
				// Unban: restore to regular user level and validate
				st.pendingChanges["validated"] = true
				st.pendingChanges["level"] = st.e.ServerCfg.RegularUserLevel
				st.statusMessage = fmt.Sprintf("|10Un-ban marked for update (level %d, validated).|07", st.e.ServerCfg.RegularUserLevel)
			} else {
				// Ban: set level 0 and unvalidated
				st.pendingChanges["validated"] = false
				st.pendingChanges["level"] = 0
				st.statusMessage = "|01Ban marked for update (level 0, unvalidated).|07"
			}
		}
		refresh = true
	case '9':
		// Toggle delete user (soft delete)
		sel := st.users[st.selectedIndex]
		if sel.ID == 1 {
			st.statusMessage = "|01Cannot delete User #1!|07"
		} else {
			cur := sel.DeletedUser
			if staged, ok := st.pendingChanges["deleted"].(bool); ok {
				cur = staged
			}
			newDeleted := !cur
			if newDeleted != sel.DeletedUser {
				st.pendingChanges["deleted"] = newDeleted
				if newDeleted {
					st.statusMessage = "|01Delete marked for update (soft delete).|07"
				} else {
					st.statusMessage = "|10Undelete marked for update (restore user).|07"
				}
			} else {
				delete(st.pendingChanges, "deleted")
				st.statusMessage = "|08No change.|07"
			}
		}
		refresh = true
	case 'i', 'I':
		// View selected user's infoforms - interactive menu
		if len(st.pendingChanges) == 0 {
			sel := st.users[st.selectedIndex]
			infoformsMu.Lock()
			ifCfg, ifErr := loadInfoFormConfig(st.e.RootConfigPath)
			infoformsMu.Unlock()

			if ifErr != nil {
				_ = terminalio.WriteProcessedBytes(st.terminal, []byte(ansi.ClearScreen()), st.outputMode)
				wv(st.terminal, "\r\n|04Error loading infoforms config.\r\n", st.outputMode)
				st.e.holdScreen(st.s, st.terminal, st.outputMode, termWidth, termHeight)
			} else {
				_ = browseInfoForms(st.e, st.s, st.terminal, st.outputMode, sel, ifCfg, termWidth, termHeight)
			}
			// Restore full screen layout after infoform viewer cleared the screen
			if err := st.renderHeader(); err != nil {
				return false, true, nil, "", err
			}
			refresh = true
		}
	case '\r', '\n':
		// Enter/Return pressed - do nothing (removed help text display)
	case editor.KeyArrowUp:
		// Block navigation while edits are staged (matches j/k handling) so
		// a subsequent Save can't apply pendingChanges to a different user.
		if len(st.pendingChanges) == 0 {
			st.moveUp()
			refresh = true
		}
	case editor.KeyArrowDown:
		if len(st.pendingChanges) == 0 {
			st.moveDown()
			refresh = true
		}
	case editor.KeyEsc:
		if st.cfg.pendingOnly && len(st.pendingChanges) > 0 {
			st.statusMessage = "|11Unsaved changes! Press [S] to save or [X] to abort.|07"
		} else {
			return false, true, nil, "", nil
		}
	}

	return refresh, false, nil, "", nil
}
