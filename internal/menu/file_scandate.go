package menu

import (
	"errors"
	"io"
	"log/slog"
	"strings"
	"time"
	"unicode"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// fileNewscanCutoff is the date the file newscan treats as the boundary for
// "new": the user's SETFILESCANDATE override when set, otherwise the default
// "since your previous logon".
func fileNewscanCutoff(u *user.User) time.Time {
	if u != nil && u.FileNewscanSince != nil {
		return *u.FileNewscanSince
	}
	return newscanSince(u)
}

// runSetFileScanDate lets the caller choose the cutoff the file newscan uses to
// decide what counts as "new". It is the file-menu counterpart to the message
// menu's Set Scan Date (UPDATENEWSCAN), adapted to the file model: files have no
// per-area read pointers, so this sets one persistent cutoff (User.FileNewscan-
// Since) instead of per-area pointers.
//
// Input: a date (MM/DD/YY), "A" for all files, or "R" to reset to the default
// ("since your previous logon"). Empty or ESC cancels.
func runSetFileScanDate(c *cmdCtx, args string) (*user.User, string, error) {
	e := c.e
	s := c.s
	terminal := c.terminal
	currentUser := c.currentUser
	nodeNumber := c.nodeNumber
	outputMode := c.outputMode

	if currentUser == nil {
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(e.LoadedStrings.ConfNavLoginRequired)), outputMode)
		time.Sleep(1 * time.Second)
		return nil, "", nil
	}

	prompt := e.LoadedStrings.FileScanDatePrompt
	if prompt == "" {
		prompt = "\r\n|07File newscan since |08(|15MM/DD/YY|08, |15A|08=all files, |15R|08=reset to last logon|08)|07: |15"
	}
	terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(prompt)), outputMode)

	input, err := readScanLine(getSessionIH(s), terminal, outputMode, 10)
	if err != nil {
		if errors.Is(err, io.EOF) {
			return nil, "LOGOFF", io.EOF
		}
		if errors.Is(err, errInputAborted) {
			return currentUser, "", nil // ESC: cancel silently
		}
		return nil, "", err
	}

	input = strings.TrimSpace(input)
	if input == "" {
		return currentUser, "", nil // cancel
	}

	var confirm string
	switch unicode.ToUpper(rune(input[0])) {
	case 'A': // all files are new
		zero := time.Time{}
		currentUser.FileNewscanSince = &zero
		confirm = "\r\n|10File newscan set to show all files.|07\r\n"
	case 'R': // reset to the default (since previous logon)
		currentUser.FileNewscanSince = nil
		confirm = "\r\n|10File newscan reset to since your last logon.|07\r\n"
	default:
		t, ok := parseScanDate(input)
		if !ok {
			msg := e.LoadedStrings.ScanInvalidDate
			if msg == "" {
				msg = "\r\n|12Invalid date.|07\r\n"
			}
			terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
			time.Sleep(1 * time.Second)
			return currentUser, "", nil
		}
		cutoff := t
		currentUser.FileNewscanSince = &cutoff
		confirm = "\r\n|10File newscan set to files since " + t.Format("01/02/2006") + ".|07\r\n"
	}

	if err := c.userManager.UpdateUser(currentUser); err != nil {
		slog.Error("failed to save file newscan date", "node", nodeNumber, "handle", currentUser.Handle, "error", err)
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte("\r\n|12Could not save the setting.|07\r\n")), outputMode)
		time.Sleep(1 * time.Second)
		return currentUser, "", nil
	}

	terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(confirm)), outputMode)
	time.Sleep(1 * time.Second)
	slog.Info("user set file newscan date", "node", nodeNumber, "handle", currentUser.Handle, "since", currentUser.FileNewscanSince)
	return currentUser, "", nil
}
