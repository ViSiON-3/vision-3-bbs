package menu

import (
	"fmt"
	"io"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/toast"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

func runPage(c *cmdCtx, args string) (*user.User, string, error) {
	e := c.e
	s := c.s
	terminal := c.terminal
	currentUser := c.currentUser
	nodeNumber := c.nodeNumber
	outputMode := c.outputMode

	if currentUser == nil {
		return nil, "", nil
	}

	handle := currentUser.Handle

	// Show online nodes
	sessions := e.activeSessions()
	e.writeString(terminal, outputMode, e.Strings().PageOnlineNodesHeader)
	for _, sess := range sessions {
		sess.Mutex.RLock()
		sessHandle := "Unknown"
		if sess.User != nil {
			sessHandle = sess.User.Handle
		}
		sessNodeID := sess.NodeID
		sessInvisible := sess.Invisible
		sess.Mutex.RUnlock()

		if sessNodeID == nodeNumber {
			continue // Skip self
		}
		// Skip invisible sessions for non-CoSysOp viewers
		if sessInvisible && !e.isCoSysOpOrAbove(currentUser) {
			continue
		}
		e.writeStringf(terminal, outputMode, e.Strings().PageNodeListEntry, sessNodeID, sessHandle)
	}

	// Prompt for target node
	e.writeString(terminal, outputMode, e.Strings().PageWhichNodePrompt)
	nodeInput, err := readLineFromSessionIH(s, terminal)
	if err != nil {
		if err == io.EOF {
			return nil, "LOGOFF", io.EOF
		}
		return nil, "", err
	}
	nodeInput = strings.TrimSpace(nodeInput)
	if strings.ToUpper(nodeInput) == "Q" || nodeInput == "" {
		return nil, "", nil
	}

	targetNodeID, err := strconv.Atoi(nodeInput)
	if err != nil {
		e.writeString(terminal, outputMode, e.Strings().PageInvalidNode)
		uiPause(500 * time.Millisecond)
		return nil, "", nil
	}

	if targetNodeID == nodeNumber {
		e.writeString(terminal, outputMode, e.Strings().PageSelfError)
		uiPause(500 * time.Millisecond)
		return nil, "", nil
	}

	targetSession := e.nodeSession(targetNodeID)
	if targetSession == nil {
		e.writeString(terminal, outputMode, e.Strings().PageNodeOffline)
		uiPause(500 * time.Millisecond)
		return nil, "", nil
	}
	targetSession.Mutex.RLock()
	targetInvisible := targetSession.Invisible
	targetSession.Mutex.RUnlock()
	if targetInvisible && !e.isCoSysOpOrAbove(currentUser) {
		e.writeString(terminal, outputMode, e.Strings().PageNodeOffline)
		uiPause(500 * time.Millisecond)
		return nil, "", nil
	}

	// Prompt for message
	e.writeString(terminal, outputMode, e.Strings().PageMessagePrompt)
	msgInput, err := readLineFromSessionIH(s, terminal)
	if err != nil {
		if err == io.EOF {
			return nil, "LOGOFF", io.EOF
		}
		return nil, "", err
	}
	msgInput = strings.TrimSpace(msgInput)
	if msgInput == "" {
		e.writeString(terminal, outputMode, e.Strings().PageCancelled)
		uiPause(500 * time.Millisecond)
		return nil, "", nil
	}

	// A logged-in target's page is kept for their account, so it survives them
	// leaving before they see it; one still at login only has this session.
	pageMsg := fmt.Sprintf(e.Strings().PageMessageFormat, handle, msgInput)
	targetSession.Mutex.RLock()
	targetUser := targetSession.User
	targetSession.Mutex.RUnlock()
	if targetUser != nil {
		_ = e.SessionRegistry.SendToast(e.toastStore(), targetUser.ID, toast.Toast{Text: pageMsg})
	} else {
		targetSession.AddPage(pageMsg)
	}

	slog.Info("paged node", "node", nodeNumber, "handle", handle, "target", targetNodeID, "chars", len(msgInput))
	e.writeStringf(terminal, outputMode, e.Strings().PageSent, targetNodeID)
	uiPause(500 * time.Millisecond)

	return nil, "", nil
}
