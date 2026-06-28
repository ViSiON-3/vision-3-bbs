package menu

import (
	"fmt"
	"strings"

	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// cmdEntry is one entry in the file-lightbar command bar.
type cmdEntry struct {
	label          string
	hotkey         string
	highlightColor string
	regularColor   string
}

// buildFileListCmdBar builds the command-bar entries for the file lightbar: the
// user bar (from a configured BAR file, or theme-colored defaults), the
// sysop-only bar (toggled with '*'), and a copy of the user bar used to restore
// it. It also returns the file-row highlight color and whether the current user
// is a sysop.
func buildFileListCmdBar(e *MenuExecutor, currentUser *user.User, cmdBarOptions, hiBarOptions []LightbarOption) (cmdEntries, sysopEntries, userEntries []cmdEntry, hiColorSeq string, isSysop bool) {
	if len(cmdBarOptions) > 0 {
		for _, opt := range cmdBarOptions {
			cmdEntries = append(cmdEntries, cmdEntry{
				label:          opt.Text,
				hotkey:         strings.ToLower(opt.HotKey),
				highlightColor: colorCodeToAnsi(opt.HighlightColor),
				regularColor:   colorCodeToAnsi(opt.RegularColor),
			})
		}
	} else {
		// Default entries using theme colors.
		defHi := colorCodeToAnsi(e.Theme.YesNoHighlightColor)
		defLo := colorCodeToAnsi(e.Theme.YesNoRegularColor)
		defaults := []struct {
			label  string
			hotkey string
		}{
			{"Mark", " "},
			{"Info", "i"},
			{"View", "v"},
			{"Download", "d"},
			{"Upload", "u"},
			{"Quit", "q"},
		}
		for _, d := range defaults {
			cmdEntries = append(cmdEntries, cmdEntry{
				label:          d.label,
				hotkey:         d.hotkey,
				highlightColor: defHi,
				regularColor:   defLo,
			})
		}
	}

	// Build sysop-only entries (toggled with the '*' key).
	isSysop = e.isCoSysOpOrAbove(currentUser)
	if isSysop {
		defHiSysop := colorCodeToAnsi(e.Theme.YesNoHighlightColor)
		defLoSysop := colorCodeToAnsi(e.Theme.YesNoRegularColor)
		sysopCmds := []struct {
			label  string
			hotkey string
		}{
			{"Edit", "e"},
			{"Kill", "k"},
			{"Move", "m"},
			{"Rename", "r"},
		}
		for _, d := range sysopCmds {
			sysopEntries = append(sysopEntries, cmdEntry{
				label:          d.label,
				hotkey:         d.hotkey,
				highlightColor: defHiSysop,
				regularColor:   defLoSysop,
			})
		}
	}

	userEntries = make([]cmdEntry, len(cmdEntries))
	copy(userEntries, cmdEntries)

	// File-row highlight color.
	hiColorSeq = colorCodeToAnsi(e.Theme.YesNoHighlightColor)
	if len(hiBarOptions) > 0 {
		hiColorSeq = colorCodeToAnsi(hiBarOptions[0].HighlightColor)
	}
	return cmdEntries, sysopEntries, userEntries, hiColorSeq, isSysop
}

// compactFileSize renders a byte count compactly: bytes (B), kilobytes (k), or
// megabytes (M).
func compactFileSize(size int64) string {
	if size < 1024 {
		return fmt.Sprintf("%dB", size)
	}
	if size < 10*1024*1024 {
		return fmt.Sprintf("%dk", size/1024)
	}
	return fmt.Sprintf("%dM", size/(1024*1024))
}
