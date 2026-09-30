package menu

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// runNewMailScan checks for new private mail and displays a count to the user.
func runNewMailScan(c *cmdCtx, args string) (*user.User, string, error) {
	e := c.e
	s := c.s
	terminal := c.terminal
	currentUser := c.currentUser
	nodeNumber := c.nodeNumber
	outputMode := c.outputMode
	termWidth := c.termWidth
	termHeight := c.termHeight

	if currentUser == nil {
		return nil, "", nil
	}

	slog.Debug("running NMAILSCAN", "node", nodeNumber, "handle", currentUser.Handle)

	if e.MessageMgr == nil {
		slog.Warn("MessageMgr not available for NMAILSCAN", "node", nodeNumber)
		return currentUser, "", nil
	}

	// Get PRIVMAIL area
	privmailArea, exists := e.MessageMgr.GetAreaByTag("PRIVMAIL")
	if !exists {
		slog.Debug("PRIVMAIL area not configured, skipping mail scan", "node", nodeNumber)
		return currentUser, "", nil
	}

	// Get JAM base for PRIVMAIL area
	base, err := e.MessageMgr.GetBase(privmailArea.ID)
	if err != nil {
		slog.Warn("JAM base not open for PRIVMAIL area", "node", nodeNumber, "error", err)
		return currentUser, "", nil
	}
	defer func() {
		if cerr := base.Close(); cerr != nil {
			slog.Warn("closing JAM base", "error", cerr)
		}
	}()

	// Get total message count
	totalMessages, err := e.MessageMgr.GetMessageCountForArea(privmailArea.ID)
	if err != nil {
		slog.Warn("failed to get message count for PRIVMAIL", "node", nodeNumber, "error", err)
		return currentUser, "", nil
	}

	if totalMessages == 0 {
		msg := e.Strings().ExecNoNewMail
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
		return currentUser, "", nil
	}

	// Get lastread pointer for this user
	lastRead, err := e.MessageMgr.GetLastRead(privmailArea.ID, currentUser.Handle)
	if err != nil {
		slog.Warn("failed to get lastread for PRIVMAIL", "node", nodeNumber, "error", err)
		lastRead = 0
	}

	// Count unread private messages addressed to this user
	newMailCount := 0
	for msgNum := lastRead + 1; msgNum <= totalMessages; msgNum++ {
		msg, readErr := base.ReadMessage(msgNum)
		if readErr != nil {
			continue
		}
		if msg.IsDeleted() {
			continue
		}
		if msg.IsPrivate() && strings.EqualFold(msg.To, currentUser.Handle) {
			newMailCount++
		}
	}

	if newMailCount > 0 {
		mailMsg := fmt.Sprintf(e.Strings().ExecNewMailCount, newMailCount)
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(mailMsg)), outputMode)

		// Offer to read it now rather than making the caller find the mail menu.
		// Saying yes drops them straight into the private-mail reader, where they
		// can reply to or skip each message; no leaves the count as the notice.
		readNow, err := e.PromptYesNo(s, terminal, "|07Read it now? @", outputMode, nodeNumber, termWidth, termHeight, true)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil, "LOGOFF", io.EOF
			}
			slog.Warn("read-now prompt failed", "node", nodeNumber, "handle", currentUser.Handle, "error", err)
			return currentUser, "", nil
		}
		if readNow {
			return runReadPrivateMail(c, "")
		}
	} else {
		msg := e.Strings().ExecNoNewMail
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
	}

	return currentUser, "", nil
}

// runLoginDisplayFile displays an ANSI file during the login sequence.
// The filename is passed via the args parameter (from LoginItem.Data).
func runLoginDisplayFile(c *cmdCtx, args string) (*user.User, string, error) {
	e := c.e
	terminal := c.terminal
	currentUser := c.currentUser
	nodeNumber := c.nodeNumber
	outputMode := c.outputMode

	filename := strings.TrimSpace(args)
	if filename == "" {
		slog.Warn("DISPLAYFILE called with no filename", "node", nodeNumber)
		return currentUser, "", nil
	}

	slog.Debug("running DISPLAYFILE", "node", nodeNumber, "file", filename)

	err := e.displayFile(terminal, filename, outputMode, c.termWidth, c.termHeight)
	if err != nil {
		slog.Warn("failed to display file", "node", nodeNumber, "file", filename, "error", err)
		// Non-fatal - continue login sequence even if file is missing
	}

	return currentUser, "", nil
}

// runLoginDoor executes a script/program during the login sequence.
// The script path is passed via the args parameter (from LoginItem.Data).
// The node number is passed as the first argument to the script.
func runLoginDoor(c *cmdCtx, args string) (*user.User, string, error) {
	s := c.s
	currentUser := c.currentUser
	nodeNumber := c.nodeNumber

	scriptPath := strings.TrimSpace(args)
	if scriptPath == "" {
		slog.Warn("RUNDOOR called with no script path", "node", nodeNumber)
		return currentUser, "", nil
	}

	slog.Info("running login door script", "node", nodeNumber, "path", scriptPath)

	if reason := loginDoorUnrunnable(scriptPath); reason != "" {
		slog.Warn("login door script cannot be run", "node", nodeNumber, "path", scriptPath, "reason", reason)
		return currentUser, "", nil
	}

	// Execute the script with node number as argument
	cmd := exec.Command(scriptPath, strconv.Itoa(nodeNumber))
	cmd.Stdout = s
	cmd.Stderr = s.Stderr()
	stdin, err := cmd.StdinPipe()
	if err != nil {
		slog.Warn("login door script stdin pipe failed", "node", nodeNumber, "path", scriptPath, "error", err)
		return currentUser, "", nil
	}

	// The door reads the session directly, so the session's input handler
	// must let go of it first, as the menu door handler does.
	resetSessionIH(s)

	// Set up a read interrupt so the stdin copier can be stopped once the
	// script exits, as the native door handler does. With cmd.Stdin = s,
	// exec's own copier stayed blocked in Read until the next keypress,
	// which it then swallowed, and cmd.Run waited for it. Sessions without
	// SetReadInterrupt keep that behaviour, minus the wait.
	readInterrupt := make(chan struct{})
	hasInterrupt := false
	if ri, ok := s.(interface{ SetReadInterrupt(<-chan struct{}) }); ok {
		ri.SetReadInterrupt(readInterrupt)
		defer ri.SetReadInterrupt(nil)
		hasInterrupt = true
	}

	if err := cmd.Start(); err != nil {
		slog.Warn("login door script failed to start", "node", nodeNumber, "path", scriptPath, "error", err)
		return currentUser, "", nil
	}
	inputDone := make(chan struct{})
	go func() {
		defer close(inputDone)
		// Ends on interrupt, disconnect or a closed pipe; closing stdin then
		// hands a disconnect on to the script as end of input.
		_, _ = io.Copy(stdin, s)
		_ = stdin.Close()
	}()

	if err := cmd.Wait(); err != nil {
		slog.Warn("login door script exited with error", "node", nodeNumber, "path", scriptPath, "error", err)
		// Non-fatal - continue login sequence
	}
	close(readInterrupt)
	if hasInterrupt {
		<-inputDone
	}

	return currentUser, "", nil
}

// loginDoorUnrunnable returns why path cannot be run as a login door, or ""
// when it can: it must stat cleanly (a missing file, a permission error on
// the way to it and the like are all refusals) and be a regular file, which
// outside Windows must also have an execute bit set.
func loginDoorUnrunnable(path string) string {
	info, err := os.Stat(path)
	if err != nil {
		return err.Error()
	}
	if !info.Mode().IsRegular() {
		return "not a regular file"
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o111 == 0 {
		return "not executable"
	}
	return ""
}
