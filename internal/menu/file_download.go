package menu

import (
	"fmt"
	"log"
	"time"

	"github.com/gliderlabs/ssh"
	"golang.org/x/term"

	"github.com/stlalpha/vision3/internal/ansi"
	"github.com/stlalpha/vision3/internal/terminalio"
	"github.com/stlalpha/vision3/internal/user"
)

// runClearBatch empties the user's tagged-file batch queue.
func runClearBatch(e *MenuExecutor, s ssh.Session, terminal *term.Terminal, userManager *user.UserMgr, currentUser *user.User, nodeNumber int, sessionStartTime time.Time, args string, outputMode ansi.OutputMode, termWidth int, termHeight int) (*user.User, string, error) {
	if currentUser == nil {
		return nil, "", nil
	}

	if len(currentUser.TaggedFileIDs) == 0 {
		msg := "|07Batch queue is already empty.\r\n"
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
		time.Sleep(1 * time.Second)
		return currentUser, "", nil
	}

	count := len(currentUser.TaggedFileIDs)
	currentUser.TaggedFileIDs = nil

	if err := userManager.UpdateUser(currentUser); err != nil {
		log.Printf("ERROR: Node %d: Failed to update user after clearing batch queue: %v", nodeNumber, err)
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte("|01Error saving user data.\r\n")), outputMode)
		time.Sleep(1 * time.Second)
		return currentUser, "", nil
	}

	msg := fmt.Sprintf("|15Cleared %d file(s) from batch queue.\r\n", count)
	terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
	log.Printf("INFO: Node %d: User %s cleared %d file(s) from batch queue", nodeNumber, currentUser.Handle, count)
	time.Sleep(1 * time.Second)

	return currentUser, "", nil
}
