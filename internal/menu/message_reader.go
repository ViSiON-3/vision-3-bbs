package menu

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"log"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/editor"
	"github.com/ViSiON-3/vision-3-bbs/internal/message"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
	"github.com/gliderlabs/ssh"
	"golang.org/x/term"
)

// Default message header style if user hasn't selected one
const defaultMsgHdrStyle = 5

// Message reader navigation options (Pascal's 10-option bar)
var msgReaderOptions = []MsgLightbarOption{
	{Label: " Next ", HotKey: 'N'},
	{Label: " Reply ", HotKey: 'R'},
	{Label: " Prev ", HotKey: 'S'},
	{Label: " Thread ", HotKey: 'T'},
	{Label: " Post ", HotKey: 'P'},
	{Label: " Jump ", HotKey: 'J'},
	{Label: " Mail ", HotKey: 'M'},
	{Label: " List ", HotKey: 'L'},
	{Label: " Quit ", HotKey: 'Q'},
}

// msgReaderDeleteOption is appended to the lightbar for sysop/co-sysop users only.
// LoColor 4 (dark red) distinguishes it from regular options.
var msgReaderDeleteOption = MsgLightbarOption{Label: " Delete ", HotKey: 'D', LoColor: 4}

// msgOwnershipFilter reports whether a message may be shown to the current user.
// A nil filter means no filtering (all non-deleted messages are visible); a
// non-nil filter is used for areas like PRIVMAIL where a user may only see their
// own messages, regardless of how navigation arrives at a message number.
type msgOwnershipFilter func(*message.DisplayMessage) bool

// runMessageReader is the core message reading loop matching Pascal's Scanboard + Readcurbul.
// It displays messages using MSGHDR.<n> templates with DataFile substitution,
// shows a 10-option lightbar for navigation, and handles single-key input.
// When msgFilter is non-nil, messages it rejects are skipped (in the direction of
// the last navigation command) and never rendered.
func runMessageReader(e *MenuExecutor, s ssh.Session, terminal *term.Terminal,
	userManager *user.UserMgr, currentUser *user.User, nodeNumber int,
	sessionStartTime time.Time, outputMode ansi.OutputMode,
	startMsg int, totalMsgCount int, isNewScan bool,
	termWidth int, termHeight int, msgFilter msgOwnershipFilter) (*user.User, string, error) {

	currentAreaID := currentUser.CurrentMessageAreaID
	currentAreaTag := currentUser.CurrentMessageAreaTag

	// Get conference and area names for display
	confName := "Local"
	areaName := currentAreaTag
	if currentUser.CurrentMsgConferenceID != 0 && e.ConferenceMgr != nil {
		if conf, found := e.ConferenceMgr.GetByID(currentUser.CurrentMsgConferenceID); found {
			confName = conf.Name
		}
	}
	if area, found := e.MessageMgr.GetAreaByID(currentAreaID); found {
		areaName = area.Name
	}

	// Determine message header style
	hdrStyle := currentUser.MsgHdr
	if hdrStyle < 1 || hdrStyle > 14 {
		hdrStyle = defaultMsgHdrStyle
	}

	// Load the MSGHDR template file
	hdrTemplatePath := filepath.Join(e.MenuSetPath, "templates", "message_headers",
		fmt.Sprintf("MSGHDR.%d.ans", hdrStyle))
	hdrTemplateBytes, hdrErr := ansi.GetAnsiFileContent(hdrTemplatePath)
	if hdrErr != nil {
		log.Printf("ERROR: Node %d: Failed to load MSGHDR.%d.ans: %v", nodeNumber, hdrStyle, hdrErr)
		// Fallback to style 2 (simple text format)
		hdrTemplatePath = filepath.Join(e.MenuSetPath, "templates", "message_headers", "MSGHDR.2.ans")
		hdrTemplateBytes, hdrErr = ansi.GetAnsiFileContent(hdrTemplatePath)
		if hdrErr != nil {
			terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(e.LoadedStrings.MsgHdrLoadError)), outputMode)
			time.Sleep(1 * time.Second)
			return nil, "", fmt.Errorf("failed loading MSGHDR templates")
		}
	}

	// Trim trailing empty lines from header template to prevent scrolling.
	// ANSI templates are often padded to 24/25 rows, but the message reader
	// handles body/footer positioning separately, so trailing blank lines
	// just push header content off the top of the screen.
	hdrTemplateBytes = bytes.TrimRight(hdrTemplateBytes, "\r\n ")

	// Terminal dimensions are now passed as parameters to use user's adjusted preferences
	// Default to 80x24 if not provided
	if termWidth <= 0 {
		termWidth = 80
	}
	if termHeight <= 0 {
		termHeight = 24
	}

	// Use the session-scoped InputHandler so this reader loop, any editor
	// invocations, and the lightbar menus all share a single goroutine reading
	// from the SSH session — no keystrokes are lost when control transfers.
	sessionIH := getSessionIH(s)
	reader := bufio.NewReader(sessionIH)

	// Lightbar colors from theme
	hiColor := e.Theme.YesNoHighlightColor
	loColor := 9 // Bright blue unselected items
	boundsColor := 1

	// Build option set: append Delete option for sysop/co-sysop users
	cfg := e.GetServerConfig()
	isSysop := currentUser.AccessLevel >= cfg.CoSysOpLevel
	activeOptions := make([]MsgLightbarOption, len(msgReaderOptions))
	copy(activeOptions, msgReaderOptions)
	if isSysop {
		activeOptions = append(activeOptions, msgReaderDeleteOption)
	}

	currentMsgNum := startMsg
	quitNewscan := false
	// navDir is the direction (+1/-1) of the last navigation command; it steers
	// the msgFilter skip so "previous" keeps moving backward past hidden messages.
	navDir := 1

	// When a msgFilter is active (e.g. PRIVMAIL), next/prev navigation must move
	// between visible messages only. These helpers locate the adjacent visible
	// message so the reader can show the usual first/last prompt at the edges
	// instead of falling off the end and dropping back to the menu.
	msgVisible := func(n int) bool {
		if n < 1 || n > totalMsgCount {
			return false
		}
		m, err := e.MessageMgr.GetMessage(currentAreaID, n)
		if err != nil || m.IsDeleted {
			return false
		}
		return msgFilter == nil || msgFilter(m)
	}
	nextVisible := func(from int) int {
		for n := from + 1; n <= totalMsgCount; n++ {
			if msgVisible(n) {
				return n
			}
		}
		return 0
	}
	prevVisible := func(from int) int {
		for n := from - 1; n >= 1; n-- {
			if msgVisible(n) {
				return n
			}
		}
		return 0
	}

readerLoop:
	for {
		if currentMsgNum > totalMsgCount || currentMsgNum < 1 {
			break readerLoop
		}

		// Load current message
		currentMsg, msgErr := e.MessageMgr.GetMessage(currentAreaID, currentMsgNum)
		if msgErr != nil {
			log.Printf("ERROR: Node %d: Failed to read message %d in area %d: %v",
				nodeNumber, currentMsgNum, currentAreaID, msgErr)
			// Try next message
			currentMsgNum++
			continue
		}

		// Skip deleted messages
		if currentMsg.IsDeleted {
			currentMsgNum++
			continue
		}

		// Ownership boundary (e.g. PRIVMAIL): never render a message the filter
		// rejects, regardless of how navigation reached this number. Skip in the
		// direction of the last navigation command so prev/next stay intuitive.
		if msgFilter != nil && !msgFilter(currentMsg) {
			currentMsgNum += navDir
			continue
		}

		// Build Pascal-style substitution map
		templateUsesUserNote := bytes.Contains(hdrTemplateBytes, []byte("|U")) ||
			bytes.Contains(hdrTemplateBytes, []byte("@U@")) ||
			bytes.Contains(hdrTemplateBytes, []byte("@U:")) ||
			bytes.Contains(hdrTemplateBytes, []byte("@U#")) ||
			bytes.Contains(hdrTemplateBytes, []byte("@U*"))
		replyCount := 0
		if e.MessageMgr != nil {
			if count, err := e.MessageMgr.GetThreadReplyCount(currentAreaID, currentMsg.MsgNum, currentMsg.Subject); err != nil {
				log.Printf("WARN: Failed to get reply count for area %d msg %d: %v", currentAreaID, currentMsg.MsgNum, err)
			} else {
				replyCount = count
			}
		}
		substitutions := buildMsgSubstitutions(currentMsg, currentAreaTag, currentMsgNum, totalMsgCount, currentUser.AccessLevel, !templateUsesUserNote, replyCount, confName, areaName, e.MessageMgr, currentAreaID, userManager, nodeNumber, e.V3NetStatus)
		// For CP437 terminals, convert UTF-8 substitution values to CP437 bytes so that
		// multi-byte runes (e.g. Cyrillic from FTN UTF-8 messages) render as '?' instead
		// of raw UTF-8 bytes that display as CP437 box-drawing characters.
		if outputMode == ansi.OutputModeCP437 {
			substitutions = convertSubsToCP437(substitutions)
		}
		autoWidths := buildAutoWidths(substitutions, totalMsgCount, termWidth)

		// Process template with substitutions (auto-detects @CODE@ or |X format)
		processedHeader := processTemplate(hdrTemplateBytes, substitutions, autoWidths)

		// Process message body and pre-format all lines
		area, _ := e.MessageMgr.GetAreaByID(currentAreaID)
		includeOrigin := area != nil && (area.AreaType == "echomail" || area.AreaType == "netmail") &&
			currentMsg.OrigAddr != "" && !hasOriginLine(currentMsg.Body)
		formattedBody := formatMessageBody(currentMsg.Body, currentMsg.OrigAddr, includeOrigin)

		// Convert pipe codes to ANSI sequences
		processedBodyBytes := ansi.ReplacePipeCodes([]byte(formattedBody))
		processedBodyStr := string(processedBodyBytes)

		var wrappedBodyLines []string

		// Check if message contains ANSI art using improved detection
		hasAnsiArt := detectAnsiArtInMessage(processedBodyStr)

		if hasAnsiArt {
			// Render ANSI art into virtual buffer with NO AUTO-WRAPPING
			// Cursor positioning is relative to buffer (0,0), not terminal screen
			// Text that exceeds buffer width is clipped, not wrapped
			wrappedBodyLines = RenderANSIArtToLines(processedBodyStr, termWidth, 500)

			// Convert CP437 bytes to UTF-8 for modern terminals
			for i, line := range wrappedBodyLines {
				wrappedBodyLines[i] = string(ansi.CP437BytesToUTF8([]byte(line)))
			}
		} else {
			// Regular text message - use normal wrapping
			wrappedBodyLines = wrapAnsiString(processedBodyStr, termWidth)
		}

		// Calculate available body height
		// Find the actual bottom row of the header using ANSI cursor tracking
		headerEndRow := findHeaderEndRow(processedHeader)
		bodyStartRow := headerEndRow + 1 // Start body on next row after header
		barLines := 2                    // Horizontal line + lightbar
		bodyAvailHeight := termHeight - bodyStartRow - barLines
		if bodyAvailHeight < 1 {
			bodyAvailHeight = 5
		}

		// Initialize scroll state for this message
		scrollOffset := 0
		totalBodyLines := len(wrappedBodyLines)
		needsRedraw := true
		needsBodyRedraw := false

		drawBody := func() {
			// Reset to grey before drawing body to prevent inherited colors (e.g. cyan
			// from the |11 reading suffix) from bleeding into plain body text lines.
			terminalio.WriteProcessedBytes(terminal, []byte("\x1b[0;37m"), outputMode)
			// Display visible portion of message body using explicit cursor positioning
			for i := 0; i < bodyAvailHeight; i++ {
				lineNum := bodyStartRow + i
				// Position cursor at specific line
				terminalio.WriteProcessedBytes(terminal, []byte(ansi.MoveCursor(lineNum, 1)), outputMode)
				// Clear line
				terminalio.WriteProcessedBytes(terminal, []byte("\x1b[K"), outputMode)
				// Display line if available
				lineIdx := scrollOffset + i
				if lineIdx < totalBodyLines {
					terminalio.WriteProcessedBytes(terminal, []byte(wrappedBodyLines[lineIdx]), outputMode)
				}
			}
		}

		// Update lastread when first displaying message
		if lrErr := e.MessageMgr.SetLastRead(currentAreaID, currentUser.Handle, currentMsgNum); lrErr != nil {
			log.Printf("ERROR: Node %d: Failed to update last read: %v", nodeNumber, lrErr)
		}

		// Inner loop for scrolling and command handling
	scrollLoop:
		for {
			var selectedKey rune // Declare here so it's available in all code paths

			// Redraw screen if needed
			if needsRedraw {
				// Clear screen and display header.
				// CP437 mode: write raw bytes — substitutions were pre-converted and the
				// ANSI art bytes must reach the terminal unchanged.
				// Other modes (UTF-8): use WriteProcessedBytes so CP437 art bytes in the
				// MSGHDR template are converted to Unicode before reaching the terminal.
				terminalio.WriteProcessedBytes(terminal, []byte(ansi.ClearScreen()), outputMode)
				if outputMode == ansi.OutputModeCP437 {
					terminal.Write(processedHeader)
				} else {
					terminalio.WriteProcessedBytes(terminal, processedHeader, outputMode)
				}

				drawBody()

				// Display footer: lightbar only
				var suffixText string
				if isNewScan {
					suffixText = e.LoadedStrings.MsgNewScanSuffix
				} else {
					suffixText = e.LoadedStrings.MsgReadingSuffix
				}

				// Draw horizontal line above footer (CP437 character 196)
				terminalio.WriteProcessedBytes(terminal, []byte(ansi.MoveCursor(termHeight-1, 1)), outputMode)
				horizontalLine := "|08" + strings.Repeat("\xC4", termWidth-1) + "|07" // CP437 horizontal line character
				terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(horizontalLine)), outputMode)

				// Position cursor at last row for lightbar
				terminalio.WriteProcessedBytes(terminal, []byte(ansi.MoveCursor(termHeight, 1)), outputMode)

				// Draw the lightbar menu
				drawMsgLightbarStatic(terminal, activeOptions, outputMode, hiColor, loColor, suffixText, 0, true, boundsColor)

				needsRedraw = false
				needsBodyRedraw = false
			} else if needsBodyRedraw {
				// Only redraw body area to avoid refreshing header/footer while scrolling
				drawBody()
				needsBodyRedraw = false
			}

			// Read key directly from the shared session input handler so arrow/page
			// keys are decoded consistently across reader/list/header lightbars.
			key, keyErr := sessionIH.ReadKey()
			if keyErr != nil {
				if errors.Is(keyErr, io.EOF) {
					return nil, "LOGOFF", io.EOF
				}
				continue
			}

			// Handle scrolling keys first
			switch key {
			case editor.KeyEsc: // ESC key - quit reader
				quitNewscan = true
				break readerLoop

			case editor.KeyArrowUp, editor.KeyCtrlE: // Up arrow - scroll up one line
				if scrollOffset > 0 {
					scrollOffset--
					needsBodyRedraw = true
				}
				continue

			case editor.KeyArrowDown, editor.KeyCtrlX: // Down arrow - scroll down one line
				if totalBodyLines > bodyAvailHeight && scrollOffset < totalBodyLines-bodyAvailHeight {
					scrollOffset++
					needsBodyRedraw = true
				}
				continue

			case editor.KeyPageUp, editor.KeyCtrlR: // Page Up
				pageSize := bodyAvailHeight - 2
				if pageSize < 5 {
					pageSize = 5
				}
				scrollOffset -= pageSize
				if scrollOffset < 0 {
					scrollOffset = 0
				}
				needsBodyRedraw = true
				continue

			case editor.KeyPageDown, editor.KeyCtrlC: // Page Down
				pageSize := bodyAvailHeight - 2
				if pageSize < 5 {
					pageSize = 5
				}
				scrollOffset += pageSize
				maxScroll := totalBodyLines - bodyAvailHeight
				if maxScroll < 0 {
					maxScroll = 0
				}
				if scrollOffset > maxScroll {
					scrollOffset = maxScroll
				}
				needsBodyRedraw = true
				continue

			case editor.KeyArrowLeft, editor.KeyArrowRight, editor.KeyCtrlS, editor.KeyCtrlD: // Left/Right arrow - activate interactive lightbar
				var suffixText string
				if isNewScan {
					suffixText = e.LoadedStrings.MsgNewScanSuffix
				} else {
					suffixText = e.LoadedStrings.MsgReadingSuffix
				}

				terminalio.WriteProcessedBytes(terminal, []byte(ansi.MoveCursor(termHeight, 1)), outputMode)

				// Determine initial direction based on which arrow was pressed
				initialDir := 0
				switch key {
				case editor.KeyArrowLeft, editor.KeyCtrlS:
					initialDir = -1 // Left arrow
				case editor.KeyArrowRight, editor.KeyCtrlD:
					initialDir = 1 // Right arrow
				}

				selKey, lbErr := runMsgLightbar(reader, terminal, activeOptions, outputMode, hiColor, loColor, suffixText, initialDir, true, boundsColor)
				if lbErr != nil {
					if errors.Is(lbErr, io.EOF) {
						return nil, "LOGOFF", io.EOF
					}
					log.Printf("ERROR: Node %d: Lightbar error: %v", nodeNumber, lbErr)
					break readerLoop
				}
				selectedKey = rune(selKey)
				// Don't continue here - fall through to handle the selected command
			}

			if selectedKey == 0 {
				// If not a scrolling key, show the lightbar for command selection
				// First handle simple single-key commands that bypass the lightbar
				if key >= 32 && key <= 126 {
					singleKey := rune(key)
					// Check if it's a direct command key
					switch unicode.ToUpper(singleKey) {
					case 'N', 'R', 'S', 'T', 'P', 'J', 'M', 'L', 'Q', 'D', '?':
						selectedKey = unicode.ToUpper(singleKey)
					default:
						// Not a recognized command, show lightbar
						var suffixText string
						if isNewScan {
							suffixText = e.LoadedStrings.MsgNewScanSuffix
						} else {
							suffixText = e.LoadedStrings.MsgReadingSuffix
						}

						// Position cursor at last row for lightbar
						terminalio.WriteProcessedBytes(terminal, []byte(ansi.MoveCursor(termHeight, 1)), outputMode)

						selKey, lbErr := runMsgLightbar(reader, terminal, activeOptions, outputMode, hiColor, loColor, suffixText, 0, true, boundsColor)
						if lbErr != nil {
							if errors.Is(lbErr, io.EOF) {
								return nil, "LOGOFF", io.EOF
							}
							log.Printf("ERROR: Node %d: Lightbar error: %v", nodeNumber, lbErr)
							break readerLoop
						}
						selectedKey = rune(selKey)
					}
				} else if key == editor.KeyEnter {
					selectedKey = 'N' // Enter = Next
				} else if key == editor.KeyEsc {
					selectedKey = 'Q' // ESC = Quit
				} else {
					// Multi-byte sequence that wasn't handled as scrolling - show lightbar
					var suffixText string
					if isNewScan {
						suffixText = e.LoadedStrings.MsgNewScanSuffix
					} else {
						suffixText = e.LoadedStrings.MsgReadingSuffix
					}

					terminalio.WriteProcessedBytes(terminal, []byte(ansi.MoveCursor(termHeight, 1)), outputMode)

					selKey, lbErr := runMsgLightbar(reader, terminal, activeOptions, outputMode, hiColor, loColor, suffixText, 0, true, boundsColor)
					if lbErr != nil {
						if errors.Is(lbErr, io.EOF) {
							return nil, "LOGOFF", io.EOF
						}
						log.Printf("ERROR: Node %d: Lightbar error: %v", nodeNumber, lbErr)
						break readerLoop
					}
					selectedKey = rune(selKey)
				}
			}

			// Handle command from lightbar or direct key
			if selectedKey == 0 {
				continue
			}

			// Now handle message navigation commands
			// Handle the selected command
			switch selectedKey {
			case 'N': // Next message
				navDir = 1
				if msgFilter != nil {
					if nxt := nextVisible(currentMsgNum); nxt > 0 {
						currentMsgNum = nxt
						break scrollLoop
					}
					terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(e.LoadedStrings.MsgEndOfMessages)), outputMode)
					time.Sleep(500 * time.Millisecond)
					break readerLoop
				}
				if currentMsgNum < totalMsgCount {
					currentMsgNum++
					break scrollLoop // Exit scroll loop to load next message
				} else {
					terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(e.LoadedStrings.MsgEndOfMessages)), outputMode)
					time.Sleep(500 * time.Millisecond)
					break readerLoop
				}

			case 'R': // Reply
				replyResult := handleReply(e, s, sessionIH, terminal, userManager, currentUser, nodeNumber,
					outputMode, currentMsg, currentAreaID, &totalMsgCount, &currentMsgNum, confName, areaName)
				if replyResult == "LOGOFF" {
					return nil, "LOGOFF", io.EOF
				}
				// Redraw message after reply
				needsRedraw = true
				continue

			case 'P': // Post new message
				terminalio.WriteProcessedBytes(terminal, []byte("\r\n"), outputMode)
				_, _, _ = runComposeMessageWithIH(e, s, sessionIH, terminal, userManager, currentUser, nodeNumber,
					sessionStartTime, "", outputMode, termWidth, termHeight)
				// Refresh total count
				newTotal, _ := e.MessageMgr.GetMessageCountForArea(currentAreaID)
				if newTotal > 0 {
					totalMsgCount = newTotal
				}
				// Redraw message after posting
				needsRedraw = true
				continue

			case 'S': // Prev - go back one message
				navDir = -1
				if msgFilter != nil {
					if prv := prevVisible(currentMsgNum); prv > 0 {
						currentMsgNum = prv
						break scrollLoop
					}
					// Already at the first visible message: stay put.
					terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(e.LoadedStrings.MsgFirstMessage)), outputMode)
					time.Sleep(500 * time.Millisecond)
					needsRedraw = true
					continue
				}
				if currentMsgNum > 1 {
					currentMsgNum--
					break scrollLoop
				} else {
					terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(e.LoadedStrings.MsgFirstMessage)), outputMode)
					time.Sleep(500 * time.Millisecond)
					needsRedraw = true
					continue
				}

			case 'T': // Thread
				navDir = 1
				handleThread(reader, e, terminal, outputMode, currentAreaID,
					&currentMsgNum, totalMsgCount, currentMsg.Subject)
				// Exit scroll loop to load new message if thread changed it
				break scrollLoop

			case 'J': // Jump to message number
				navDir = 1
				handleJump(reader, terminal, outputMode, &currentMsgNum, totalMsgCount, e.LoadedStrings.MsgJumpPrompt, e.LoadedStrings.MsgInvalidMsgNum)
				// Exit scroll loop to load new message
				break scrollLoop

			case 'M': // Mail reply (deferred)
				terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(e.LoadedStrings.MsgMailReplyDeferred)), outputMode)
				time.Sleep(1 * time.Second)
				needsRedraw = true
				continue

			case 'L': // List messages in current area
				updatedUser, nextAction, listErr := runListMsgsFiltered(&cmdCtx{e: e, s: s, terminal: terminal, userManager: userManager, currentUser: currentUser, nodeNumber: nodeNumber, sessionStartTime: sessionStartTime, outputMode: outputMode, termWidth: termWidth, termHeight: termHeight}, "", msgFilter)
				if updatedUser != nil {
					currentUser = updatedUser
				}
				if listErr != nil || nextAction == "LOGOFF" {
					return currentUser, "LOGOFF", listErr
				}
				needsRedraw = true
				continue

			case 'D': // Delete message (sysop/co-sysop only)
				if !isSysop {
					continue // Ignore if triggered without access
				}
				confirmed, confErr := promptSingleChar(reader, terminal,
					"\r\n |04Delete this message? [Y/N] |07", outputMode)
				if confErr != nil || confirmed != 'Y' {
					needsRedraw = true
					continue
				}
				if delErr := e.MessageMgr.DeleteMessage(currentAreaID, currentMsg.MsgNum); delErr != nil {
					log.Printf("ERROR: Node %d: delete message %d area %d: %v",
						nodeNumber, currentMsg.MsgNum, currentAreaID, delErr)
					terminalio.WriteProcessedBytes(terminal,
						ansi.ReplacePipeCodes([]byte("\r\n|01Error deleting message.|07\r\n")), outputMode)
					time.Sleep(1 * time.Second)
					needsRedraw = true
					continue
				}
				// Pack the base (physically remove deleted messages and
				// renumber) then rebuild reply threading chains.
				if packErr := e.MessageMgr.PackAndLinkArea(currentAreaID); packErr != nil {
					log.Printf("ERROR: Node %d: pack/link area %d after delete: %v",
						nodeNumber, currentAreaID, packErr)
				}
				// Refresh total count after pack renumbered messages
				newTotal, _ := e.MessageMgr.GetMessageCountForArea(currentAreaID)
				if newTotal > 0 {
					totalMsgCount = newTotal
				}
				// After pack, the deleted slot is gone. The message that
				// was at currentMsgNum+1 is now at currentMsgNum, so keep
				// the same position. If we deleted the last message, clamp.
				if currentMsgNum > totalMsgCount {
					currentMsgNum = totalMsgCount
				}
				if totalMsgCount <= 0 {
					break readerLoop
				}
				break scrollLoop

			case 'Q': // Quit
				quitNewscan = true
				break readerLoop

			case '?': // Help
				displayReaderHelp(terminal, outputMode, isSysop)
				needsRedraw = true
				continue

			default:
				continue
			}
		}
	}

	// Update lastread on exit
	if currentMsgNum >= 1 && currentMsgNum <= totalMsgCount {
		if lrErr := e.MessageMgr.SetLastRead(currentAreaID, currentUser.Handle, currentMsgNum); lrErr != nil {
			log.Printf("ERROR: Node %d: Failed to update last read on exit: %v", nodeNumber, lrErr)
		}
	}

	if quitNewscan {
		return nil, "QUIT_NEWSCAN", nil
	}
	return nil, "", nil
}

var quotePrefixRe = regexp.MustCompile(`^[A-Za-z0-9]{1,3}>`)

// MessageHeaderTemplate represents a discovered message header template file.
type MessageHeaderTemplate struct {
	Number      int    // Template number (e.g., 1, 2, 15)
	Filename    string // Full filename (e.g., "MSGHDR.15.ans")
	DisplayName string // Human-readable name (e.g., "Header Style 15")
}

// headerDisplayNames maps template numbers to their actual display names.
var headerDisplayNames = map[int]string{
	1:  "Generic Blue Box",
	2:  "Extremely Simple Message Header",
	3:  "LiQUiD Blue Box Header",
	4:  "Generic TCS Header",
	5:  "Gray/White ViSiON/2 Header",
	6:  "Cool V/2 Quick Header!",
	7:  "ViSiON-X Header",
	8:  "Bouncer Neato Header",
	9:  "Celerity Header",
	10: "PC Express Header",
	11: "Extreme Header",
	12: "LSD Header",
	13: "Renegade Header",
	14: "ViSiON-X Grey/White Header",
}
