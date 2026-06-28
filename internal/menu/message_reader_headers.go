package menu

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/editor"
	"github.com/ViSiON-3/vision-3-bbs/internal/message"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// findMessageByMSGID searches for a message in the current area that has the given MSGID.
// Returns the message number (1-based) if found, or 0 if not found.
// Delegates to MessageManager.FindMessageByMSGID which uses a cached index.
func findMessageByMSGID(msgMgr *message.MessageManager, areaID int, msgID string) int {
	return msgMgr.FindMessageByMSGID(areaID, msgID)
}

// extractHeaderNumber extracts the numeric ID from a message header filename.
// Examples: "MSGHDR.1.ans" -> 1, "MSGHDR.15.ans" -> 15
func extractHeaderNumber(filename string) (int, error) {
	// Remove .ans extension and MSGHDR. prefix
	name := strings.TrimSuffix(filename, ".ans")
	name = strings.TrimPrefix(name, "MSGHDR.")

	return strconv.Atoi(name)
}

// discoverMessageHeaders finds all MSGHDR.*.ans template files in the templates/message_headers directory.
// Returns templates sorted by number (1, 2, ..., 14, 15, etc.).
func discoverMessageHeaders(templatesPath string) ([]MessageHeaderTemplate, error) {
	pattern := filepath.Join(templatesPath, "message_headers", "MSGHDR.*.ans")
	files, err := filepath.Glob(pattern)
	if err != nil {
		return nil, fmt.Errorf("failed to glob header templates: %w", err)
	}

	var templates []MessageHeaderTemplate
	for _, file := range files {
		base := filepath.Base(file)

		// Skip MSGHDR.ANS (selection screen, not a template)
		if base == "MSGHDR.ANS" {
			continue
		}

		num, err := extractHeaderNumber(base)
		if err != nil {
			log.Printf("WARN: Skipping malformed header filename: %s (error: %v)", base, err)
			continue
		}

		// Get display name from map, or fall back to generic name
		displayName, ok := headerDisplayNames[num]
		if !ok {
			displayName = fmt.Sprintf("Header Style %d", num)
		}

		templates = append(templates, MessageHeaderTemplate{
			Number:      num,
			Filename:    base,
			DisplayName: displayName,
		})
	}

	// Sort by number
	sort.Slice(templates, func(i, j int) bool {
		return templates[i].Number < templates[j].Number
	})

	return templates, nil
}

// runGetHeaderType allows the user to select a message header style (unlimited templates via lightbar).
// Discovers all MSGHDR.*.ans templates dynamically and presents them in a lightbar menu.
func runGetHeaderType(c *cmdCtx, args string) (*user.User, string, error) {
	e := c.e
	s := c.s
	terminal := c.terminal
	userManager := c.userManager
	currentUser := c.currentUser
	nodeNumber := c.nodeNumber
	outputMode := c.outputMode
	termWidth := c.termWidth
	termHeight := c.termHeight

	if currentUser == nil {
		return nil, "", nil
	}

	// Load lightbar options from MSGHDR.BAR
	options, err := loadLightbarOptions("MSGHDR", e)
	if err != nil {
		log.Printf("ERROR: Node %d: Failed to load MSGHDR.BAR: %v", nodeNumber, err)
		msg := "\r\n|01Error loading MSGHDR.BAR!|07\r\n"
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
		time.Sleep(1 * time.Second)
		return nil, "", nil
	}

	if len(options) == 0 {
		log.Printf("ERROR: Node %d: No options in MSGHDR.BAR", nodeNumber)
		msg := "\r\n|01No message header options found!|07\r\n"
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
		time.Sleep(1 * time.Second)
		return nil, "", nil
	}

	// Verify template files exist and extract template numbers
	templatesPath := filepath.Join(e.MenuSetPath, "templates", "message_headers")
	var validOptions []LightbarOption
	for _, opt := range options {
		// Parse template number from return value (not hotkey, since 10+ use letters)
		templateNum, parseErr := strconv.Atoi(opt.ReturnValue)
		if parseErr != nil {
			log.Printf("WARN: Invalid return value in MSGHDR.BAR: %s", opt.ReturnValue)
			continue
		}

		// Verify template file exists
		templateFile := filepath.Join(templatesPath, fmt.Sprintf("MSGHDR.%d.ans", templateNum))
		if _, statErr := os.Stat(templateFile); statErr != nil {
			log.Printf("WARN: Template file not found: MSGHDR.%d.ans", templateNum)
			continue
		}

		validOptions = append(validOptions, opt)
	}

	if len(validOptions) == 0 {
		log.Printf("ERROR: Node %d: No valid message header templates found", nodeNumber)
		msg := "\r\n|01No valid message header templates!|07\r\n"
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
		time.Sleep(1 * time.Second)
		return nil, "", nil
	}

	options = validOptions
	log.Printf("INFO: Node %d: Loaded %d message header options from BAR file", nodeNumber, len(options))

	// Display the header selection ANSI screen
	selectionPath := filepath.Join(e.MenuSetPath, "templates", "message_headers", "MSGHDR.ANS")
	selectionBytes, err := ansi.GetAnsiFileContent(selectionPath)
	if err != nil {
		log.Printf("ERROR: Node %d: Failed to load MSGHDR.ANS: %v", nodeNumber, err)
		msg := "\r\n|01MSGHDR.ANS not found! Please notify SysOp.|07\r\n"
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
		time.Sleep(1 * time.Second)
		return nil, "", nil
	}

	// Hide cursor during lightbar selection
	terminalio.WriteProcessedBytes(terminal, []byte("\x1b[?25l"), outputMode)
	// Ensure cursor is restored on exit
	defer terminalio.WriteProcessedBytes(terminal, []byte("\x1b[?25h"), outputMode)

	// Use direct key decoding from the session-scoped input handler so arrow/home/end
	// behavior is consistent with other lightbars.
	sessionIH := getSessionIH(s)

	selectedIndex := 0 // Currently highlighted option

	// Helper to draw one lightbar option using BAR file attributes
	drawOption := func(idx int, highlighted bool) {
		if idx < 0 || idx >= len(options) {
			return
		}

		opt := options[idx]

		// Extract template number from hotkey
		templateNum, _ := strconv.Atoi(opt.ReturnValue)

		// Position cursor using BAR file coordinates
		terminalio.WriteProcessedBytes(terminal, []byte(fmt.Sprintf("\x1b[%d;%dH", opt.Y, opt.X)), outputMode)

		if highlighted {
			// Highlighted: use highlight color from BAR file
			colorSeq := colorCodeToAnsi(opt.HighlightColor)
			terminalio.WriteProcessedBytes(terminal, []byte(colorSeq), outputMode)

			// Draw text padded to 39 columns
			displayText := fmt.Sprintf("[%-2d] - %-30s", templateNum, opt.Text)
			if len(displayText) > 39 {
				displayText = displayText[:39]
			} else if len(displayText) < 39 {
				displayText = fmt.Sprintf("%-39s", displayText)
			}
			terminalio.WriteProcessedBytes(terminal, []byte(displayText), outputMode)
		} else {
			// Inactive: white for brackets/number, bright blue for name
			bracketPart := fmt.Sprintf("[%-2d] - ", templateNum)
			namePart := fmt.Sprintf("%-30s", opt.Text)

			// Ensure total is 39 columns
			totalText := bracketPart + namePart
			if len(totalText) > 39 {
				totalText = totalText[:39]
			} else if len(totalText) < 39 {
				totalText = fmt.Sprintf("%-39s", totalText)
			}

			// White for bracket part
			terminalio.WriteProcessedBytes(terminal, []byte("\x1b[37m"+bracketPart), outputMode)
			// Bright blue for name part (pad remaining space)
			remaining := totalText[len(bracketPart):]
			terminalio.WriteProcessedBytes(terminal, []byte("\x1b[1;34m"+remaining), outputMode)
		}

		// Reset color
		terminalio.WriteProcessedBytes(terminal, []byte("\x1b[0m"), outputMode)
	}

	// Helper to redraw all options
	redrawAll := func() {
		terminalio.WriteProcessedBytes(terminal, []byte(ansi.ClearScreen()), outputMode)
		// For CP437 mode, write raw bytes directly to avoid UTF-8 false positives
		processedSelBytes := ansi.ReplacePipeCodes(selectionBytes)
		if outputMode == ansi.OutputModeCP437 {
			terminal.Write(processedSelBytes)
		} else {
			terminalio.WriteProcessedBytes(terminal, processedSelBytes, outputMode)
		}

		// Draw all options from BAR file
		for i := 0; i < len(options); i++ {
			drawOption(i, i == selectedIndex)
		}

		// Show navigation hint at bottom (row 24), centered
		const hintY = 24 // Fixed row for footer
		// Use CP437 arrow characters: \x18=↑, \x19=↓
		hint := "|08Use |14\x18|08/|14\x19|08 arrows, |14ENTER|08 to preview, |14SPACE|08 to select, |14Q|08 to quit|07"
		// Calculate visible text length (without pipe codes) for centering
		visibleHint := "Use \x18/\x19 arrows, ENTER to preview, SPACE to select, Q to quit"
		hintX := (80 - len(visibleHint)) / 2
		if hintX < 1 {
			hintX = 1
		}
		terminalio.WriteProcessedBytes(terminal, []byte(fmt.Sprintf("\x1b[%d;%dH", hintY, hintX)), outputMode)
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(hint)), outputMode)
	}

	// Initial draw
	redrawAll()

	// Main selection loop
	for {
		key, readErr := sessionIH.ReadKey()
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return nil, "LOGOFF", io.EOF
			}
			return nil, "", nil
		}

		// Handle arrow-key navigation
		if key == editor.KeyArrowUp || key == editor.KeyCtrlE {
			if selectedIndex > 0 {
				selectedIndex--
				drawOption(selectedIndex+1, false)
				drawOption(selectedIndex, true)
			}
			continue
		}
		if key == editor.KeyArrowDown || key == editor.KeyCtrlX {
			if selectedIndex < len(options)-1 {
				selectedIndex++
				drawOption(selectedIndex-1, false)
				drawOption(selectedIndex, true)
			}
			continue
		}

		// Handle Q to quit
		if key == 'q' || key == 'Q' {
			break
		}

		// Handle ENTER to preview
		if key == editor.KeyEnter {
			opt := options[selectedIndex]
			templateNum, _ := strconv.Atoi(opt.ReturnValue)
			hdrPath := filepath.Join(e.MenuSetPath, "templates", "message_headers", fmt.Sprintf("MSGHDR.%d.ans", templateNum))

			// Preview with sample data
			hdrBytes, readErr := ansi.GetAnsiFileContent(hdrPath)
			if readErr != nil {
				log.Printf("ERROR: Node %d: Failed to read header file MSGHDR.%d.ans: %v", nodeNumber, templateNum, readErr)
				continue
			}
			hdrBytes = bytes.TrimRight(hdrBytes, "\r\n ")

			sampleSubs := map[byte]string{
				'B': "GENERAL",
				'T': "ViSiON/3 Rocks!",
				'F': currentUser.Handle,
				'S': "Everybody",
				'U': currentUser.PrivateNote,
				'M': "LOCAL",
				'L': strconv.Itoa(currentUser.AccessLevel),
				'#': "1",
				'N': "42",
				'C': "[1/42]",
				'V': "1 of 42",
				'D': config.NowIn(e.ServerCfg.Timezone).Format("01/02/06"),
				'W': config.NowIn(e.ServerCfg.Timezone).Format("3:04 pm"),
				'P': "None",
				'E': "0",
				'O': "",
				'A': "",
				'Z': "GENERAL > General Discussion",
				'X': "GENERAL > General Discussion [1/42]",
				'K': strconv.Itoa(nodeNumber),
			}
			sampleAutoWidths := buildAutoWidths(sampleSubs, 42, 80)

			processedPreview := processTemplate(hdrBytes, sampleSubs, sampleAutoWidths)
			terminalio.WriteProcessedBytes(terminal, []byte(ansi.ClearScreen()), outputMode)
			// For CP437 mode, write raw bytes directly to avoid UTF-8 false positives
			if outputMode == ansi.OutputModeCP437 {
				terminal.Write(processedPreview)
			} else {
				terminalio.WriteProcessedBytes(terminal, processedPreview, outputMode)
			}

			// Ask "Pick this header?" - centered at row 14
			pickPrompt := "|08P|07i|15ck |08t|07h|15is |08h|07e|15ader? "
			// Full prompt includes: "Pick this header?    Nah    Yeah    " (approximately 40 chars)
			fullPromptWidth := 40 // Prompt text + spacing + options
			promptX := (80 - fullPromptWidth) / 2
			if promptX < 1 {
				promptX = 1
			}
			terminalio.WriteProcessedBytes(terminal, []byte(fmt.Sprintf("\x1b[14;%dH", promptX)), outputMode)

			pickYes, pickErr := e.PromptYesNo(s, terminal, pickPrompt, outputMode, nodeNumber, termWidth, termHeight, false)
			if pickErr != nil {
				if errors.Is(pickErr, io.EOF) {
					return nil, "LOGOFF", io.EOF
				}
				redrawAll()
				continue
			}

			if pickYes {
				// User selected this header - save preference
				currentUser.MsgHdr = templateNum
				if saveErr := userManager.UpdateUser(currentUser); saveErr != nil {
					log.Printf("ERROR: Node %d: Failed to save user after header selection: %v", nodeNumber, saveErr)
				}
				log.Printf("INFO: Node %d: User %s selected header style %d", nodeNumber, currentUser.Handle, templateNum)
				break
			}

			// User declined - redraw menu
			redrawAll()
			continue
		}

		// Handle SPACE to select immediately (without preview)
		if key == ' ' {
			opt := options[selectedIndex]
			templateNum, _ := strconv.Atoi(opt.ReturnValue)
			currentUser.MsgHdr = templateNum
			if saveErr := userManager.UpdateUser(currentUser); saveErr != nil {
				log.Printf("ERROR: Node %d: Failed to save user after header selection: %v", nodeNumber, saveErr)
			}
			log.Printf("INFO: Node %d: User %s selected header style %d", nodeNumber, currentUser.Handle, templateNum)
			break
		}

		// Handle numeric hotkeys (1-9) for direct selection
		if key >= '1' && key <= '9' {
			digit := key - '0'
			// Find option with this template number
			for i, opt := range options {
				templateNum, _ := strconv.Atoi(opt.ReturnValue)
				if templateNum == digit {
					oldIndex := selectedIndex
					selectedIndex = i
					drawOption(oldIndex, false)
					drawOption(selectedIndex, true)
					break
				}
			}
			continue
		}
	}

	return nil, "", nil
}
