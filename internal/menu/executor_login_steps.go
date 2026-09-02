package menu

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// runNewMailScan checks for new private mail and displays a count to the user.
func runNewMailScan(c *cmdCtx, args string) (*user.User, string, error) {
	e := c.e
	terminal := c.terminal
	currentUser := c.currentUser
	nodeNumber := c.nodeNumber
	outputMode := c.outputMode

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
		msg := e.LoadedStrings.ExecNoNewMail
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
		mailMsg := fmt.Sprintf(e.LoadedStrings.ExecNewMailCount, newMailCount)
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(mailMsg)), outputMode)
	} else {
		msg := e.LoadedStrings.ExecNoNewMail
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

	err := e.displayFile(terminal, filename, outputMode, c.termHeight)
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

	// Verify script exists
	if _, err := os.Stat(scriptPath); os.IsNotExist(err) {
		slog.Warn("login door script not found", "node", nodeNumber, "path", scriptPath)
		return currentUser, "", nil
	}

	// Execute the script with node number as argument
	cmd := exec.Command(scriptPath, strconv.Itoa(nodeNumber))
	cmd.Stdin = s
	cmd.Stdout = s
	cmd.Stderr = s.Stderr()

	if err := cmd.Run(); err != nil {
		slog.Warn("login door script exited with error", "node", nodeNumber, "path", scriptPath, "error", err)
		// Non-fatal - continue login sequence
	}

	return currentUser, "", nil
}
