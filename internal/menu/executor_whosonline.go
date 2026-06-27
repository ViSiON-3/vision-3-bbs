package menu

import (
	"bytes"
	"errors"
	"fmt"
	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio" // <-- Added import
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
	"io"
	"log"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func replaceWhoOnlineToken(line, token, value string) string {
	return lastCallerATTokenRegex.ReplaceAllStringFunc(line, func(match string) string {
		parts := lastCallerATTokenRegex.FindStringSubmatch(match)
		if len(parts) < 2 {
			return match
		}
		code := strings.ToUpper(parts[1])
		if code != token {
			return match
		}
		if len(parts) > 2 && parts[2] != "" {
			if width, err := strconv.Atoi(parts[2]); err == nil {
				return formatLastCallerATWidth(value, width, false)
			}
		}
		return value
	})
}

// runLoginWhosOnline prompts the user with a YES/NO lightbar asking if they want
// to view users on other nodes. If YES, it displays the Who's Online screen.
// Skips the prompt entirely if no other visible users are on other nodes.
func runLoginWhosOnline(c *cmdCtx, args string) (*user.User, string, error) {
	e := c.e
	s := c.s
	terminal := c.terminal
	userManager := c.userManager
	currentUser := c.currentUser
	nodeNumber := c.nodeNumber
	sessionStartTime := c.sessionStartTime
	outputMode := c.outputMode
	termWidth := c.termWidth
	termHeight := c.termHeight

	log.Printf("DEBUG: Node %d: Running WHOISONLINE from login sequence", nodeNumber)

	// Check if there are any other visible users on other nodes before prompting
	if e.SessionRegistry != nil {
		sessions := e.SessionRegistry.ListActive()
		otherVisible := 0
		for _, sess := range sessions {
			sess.Mutex.RLock()
			sessNodeID := sess.NodeID
			sessInvisible := sess.Invisible
			sessUser := sess.User
			sess.Mutex.RUnlock()

			if sessUser == nil {
				continue // skip pre-auth sessions
			}
			if sessNodeID == nodeNumber {
				continue
			}
			if sessInvisible && !e.isCoSysOpOrAbove(currentUser) {
				continue
			}
			otherVisible++
		}
		if otherVisible == 0 {
			log.Printf("DEBUG: Node %d: No other visible users online, skipping WHOISONLINE prompt", nodeNumber)
			return currentUser, "", nil
		}
	}

	// Prompt the user with a YES/NO lightbar
	result, err := e.PromptYesNo(s, terminal, "|07View users on other nodes?", outputMode, nodeNumber, termWidth, termHeight, false)
	if err != nil {
		if errors.Is(err, io.EOF) {
			return nil, "LOGOFF", io.EOF
		}
		log.Printf("ERROR: Node %d: Failed during WHOISONLINE YES/NO prompt: %v", nodeNumber, err)
		return currentUser, "", nil // Non-fatal, skip
	}

	if !result {
		log.Printf("DEBUG: Node %d: User declined to view who's online", nodeNumber)
		// Write a newline to move past the prompt
		terminalio.WriteProcessedBytes(terminal, []byte("\r\n"), outputMode)
		return currentUser, "", nil
	}

	// User said YES - show the who's online display
	terminalio.WriteProcessedBytes(terminal, []byte("\r\n"), outputMode)
	return runWhoIsOnline(&cmdCtx{e: e, s: s, terminal: terminal, userManager: userManager, currentUser: currentUser, nodeNumber: nodeNumber, sessionStartTime: sessionStartTime, outputMode: outputMode, termWidth: termWidth, termHeight: termHeight}, args)
}

func runWhoIsOnline(c *cmdCtx, args string) (*user.User, string, error) {
	e := c.e
	s := c.s
	terminal := c.terminal
	currentUser := c.currentUser
	nodeNumber := c.nodeNumber
	outputMode := c.outputMode
	termWidth := c.termWidth
	termHeight := c.termHeight

	log.Printf("DEBUG: Node %d: Running WHOISONLINE", nodeNumber)

	topPath := filepath.Join(e.MenuSetPath, "templates", "WHOONLN.TOP")
	midPath := filepath.Join(e.MenuSetPath, "templates", "WHOONLN.MID")
	botPath := filepath.Join(e.MenuSetPath, "templates", "WHOONLN.BOT")

	topBytes, errTop := readTemplateFile(topPath)
	midBytes, errMid := readTemplateFile(midPath)
	botBytes, errBot := readTemplateFile(botPath)

	if errTop != nil || errMid != nil || errBot != nil {
		log.Printf("ERROR: Node %d: Failed to load WHOONLN templates: TOP(%v), MID(%v), BOT(%v)", nodeNumber, errTop, errMid, errBot)
		msg := "\r\n|01Error loading Who's Online templates.|07\r\n"
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
		time.Sleep(1 * time.Second)
		return nil, "", fmt.Errorf("failed loading WHOONLN templates")
	}

	topBytes = stripSauceMetadata(topBytes)
	midBytes = stripSauceMetadata(midBytes)
	botBytes = stripSauceMetadata(botBytes)
	topBytes = normalizePipeCodeDelimiters(topBytes)
	midBytes = normalizePipeCodeDelimiters(midBytes)
	botBytes = normalizePipeCodeDelimiters(botBytes)

	processedTop := string(ansi.ReplacePipeCodes(topBytes))
	processedMid := string(ansi.ReplacePipeCodes(midBytes))
	processedBot := string(ansi.ReplacePipeCodes(botBytes))

	if e.SessionRegistry == nil {
		log.Printf("ERROR: Node %d: SessionRegistry is nil", nodeNumber)
		msg := "\r\n|01Who's Online is unavailable.|07\r\n"
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
		time.Sleep(1 * time.Second)
		return currentUser, "", nil
	}

	sessions := e.SessionRegistry.ListActive()

	var buf bytes.Buffer
	buf.WriteString(processedTop)
	if !strings.HasSuffix(processedTop, "\r\n") && !strings.HasSuffix(processedTop, "\n") {
		buf.WriteString("\r\n")
	}

	now := time.Now()
	visibleCount := 0
	for _, sess := range sessions {
		// Read all volatile session fields under a single lock to prevent data races.
		sess.Mutex.RLock()
		sessInvisible := sess.Invisible
		sessNodeID := sess.NodeID
		sessStartTime := sess.StartTime
		userName := "Logging In..."
		userLevel := 0
		groupLocation := ""
		if sess.User != nil {
			userName = sess.User.Handle
			userLevel = sess.User.AccessLevel
			groupLocation = sess.User.GroupLocation
		}
		activity := sess.Activity
		if activity == "" {
			activity = sess.CurrentMenu
		}
		lastActivity := sess.LastActivity
		sess.Mutex.RUnlock()

		// Skip invisible sessions for non-CoSysOp viewers
		if sessInvisible && !e.isCoSysOpOrAbove(currentUser) {
			continue
		}
		visibleCount++
		line := processedMid

		nodeStr := strconv.Itoa(sessNodeID)
		line = replaceWhoOnlineToken(line, "ND", nodeStr)
		line = replaceWhoOnlineToken(line, "UN", userName)

		// Level and Group/Location tokens
		line = replaceWhoOnlineToken(line, "LV", strconv.Itoa(userLevel))
		if groupLocation == "" {
			groupLocation = "---"
		}
		line = replaceWhoOnlineToken(line, "GL", groupLocation)

		if activity == "" {
			activity = "---"
		}
		line = replaceWhoOnlineToken(line, "LO", activity)

		safeStart := sessStartTime
		if safeStart.IsZero() {
			safeStart = now
		}
		dur := now.Sub(safeStart)
		hours := int(dur.Hours())
		mins := int(dur.Minutes()) % 60
		timeOn := fmt.Sprintf("%d:%02d", hours, mins)
		line = replaceWhoOnlineToken(line, "TO", timeOn)

		safeLast := lastActivity
		if safeLast.IsZero() {
			safeLast = now
		}
		idle := now.Sub(safeLast)
		idleMins := int(idle.Minutes())
		idleSecs := int(idle.Seconds()) % 60
		idleStr := fmt.Sprintf("%d:%02d", idleMins, idleSecs)
		line = replaceWhoOnlineToken(line, "ID", idleStr)

		buf.WriteString(line)
		if !strings.HasSuffix(line, "\r\n") && !strings.HasSuffix(line, "\n") {
			buf.WriteString("\r\n")
		}
	}

	processedBot = replaceWhoOnlineToken(processedBot, "NODECT", strconv.Itoa(visibleCount))
	buf.WriteString(processedBot)
	if !strings.HasSuffix(processedBot, "\r\n") && !strings.HasSuffix(processedBot, "\n") {
		buf.WriteString("\r\n")
	}

	terminalio.WriteProcessedBytes(terminal, buf.Bytes(), outputMode)

	pausePrompt := e.LoadedStrings.PauseString
	if pausePrompt == "" {
		pausePrompt = "\r\n|07Press |15[ENTER]|07 to continue... "
	}
	err := writeCenteredPausePrompt(s, terminal, pausePrompt, outputMode, termWidth, termHeight)
	if err != nil {
		if errors.Is(err, io.EOF) {
			log.Printf("INFO: Node %d: User disconnected during WHOISONLINE pause.", nodeNumber)
			return nil, "LOGOFF", io.EOF
		}
		log.Printf("ERROR: Node %d: Failed during WHOISONLINE pause: %v", nodeNumber, err)
		return nil, "", err
	}

	return currentUser, "", nil
}
