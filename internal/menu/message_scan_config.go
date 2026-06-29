package menu

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/editor"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// runNewscanConfig allows users to tag/untag message areas for their personal newscan.
func runNewscanConfig(c *cmdCtx, args string) (*user.User, string, error) {
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

	if currentUser == nil {
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(e.LoadedStrings.ScanConfigLoginRequired)), outputMode)
		time.Sleep(1 * time.Second)
		return nil, "", nil
	}

	sessionIH := getSessionIH(s)

	// Get all accessible message areas
	allAreas := e.MessageMgr.ListAreas()
	if len(allAreas) == 0 {
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(e.LoadedStrings.ScanNoAreasAvailable)), outputMode)
		time.Sleep(1 * time.Second)
		return nil, "", nil
	}

	// Build conference lookup maps for name and position.
	confNameMap := make(map[int]string)
	confPosMap := make(map[int]int)
	if e.ConferenceMgr != nil {
		for _, conf := range e.ConferenceMgr.ListConferences() {
			confNameMap[conf.ID] = conf.Name
			confPosMap[conf.ID] = conf.Position
		}
	}

	// Sort areas by conference Position, then by area Position within each conference.
	// Missing or zero-position conferences sort after known ones.
	sort.Slice(allAreas, func(i, j int) bool {
		ai, aj := allAreas[i], allAreas[j]
		ci, oki := confPosMap[ai.ConferenceID]
		cj, okj := confPosMap[aj.ConferenceID]
		if !oki || ci <= 0 {
			ci = 1<<31 - 1 // sort unknown/unset last
		}
		if !okj || cj <= 0 {
			cj = 1<<31 - 1
		}
		if ci != cj {
			return ci < cj
		}
		// Break conference-position ties by ConferenceID to prevent interleaving
		if ai.ConferenceID != aj.ConferenceID {
			return ai.ConferenceID < aj.ConferenceID
		}
		if ai.Position != aj.Position {
			return ai.Position < aj.Position
		}
		return ai.ID < aj.ID
	})

	// Build accessible areas list
	var accessibleAreas []areaListItem

	// Build area list with conference headers
	currentConfID := -1
	for _, area := range allAreas {
		// Check read access
		if !checkACS(area.ACSRead, currentUser, s, terminal, sessionStartTime) {
			continue
		}

		// Add conference header if changed
		if area.ConferenceID != currentConfID {
			currentConfID = area.ConferenceID
			confName := confNameMap[area.ConferenceID]
			if confName == "" {
				confName = "General"
			}
			accessibleAreas = append(accessibleAreas, areaListItem{
				confName: confName,
				isHeader: true,
			})
		}

		accessibleAreas = append(accessibleAreas, areaListItem{
			area:     area,
			confName: confNameMap[area.ConferenceID],
			isHeader: false,
		})
	}

	if len(accessibleAreas) == 0 {
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(e.LoadedStrings.ScanNoAccessibleAreas)), outputMode)
		time.Sleep(1 * time.Second)
		return nil, "", nil
	}

	// Create tagged map for quick lookups
	taggedMap := make(map[string]bool)
	for _, tag := range currentUser.TaggedMessageAreaTags {
		taggedMap[tag] = true
	}

	// UI state
	currentIdx := 0
	// Skip to first non-header
	for currentIdx < len(accessibleAreas) && accessibleAreas[currentIdx].isHeader {
		currentIdx++
	}

	// Get terminal dimensions: prefer passed params, then user prefs, then defaults
	if termHeight <= 0 {
		termHeight = currentUser.ScreenHeight
	}
	if termHeight <= 0 {
		termHeight = 24
	}
	if termWidth <= 0 {
		termWidth = currentUser.ScreenWidth
	}
	if termWidth <= 0 {
		termWidth = 80
	}

	// Calculate horizontal centering
	// Content width: prefix(3) + name(40) + " [" (2) + icon(1) + "]" (1) + space(1) = 48 chars
	contentWidth := 48
	leftPadding := (termWidth - contentWidth) / 2
	if leftPadding < 0 {
		leftPadding = 0
	}

	// Calculate viewport dimensions
	headerLines := 6  // ANSI art header (subscribe.ans is 6 lines)
	spacingLines := 2 // Blank line after header, blank line before footer
	footerLines := 1  // Command line at bottom
	availableRows := termHeight - headerLines - spacingLines - footerLines
	if availableRows < 5 {
		availableRows = 5
	}

	viewportOffset := 0
	previousIdx := -1
	previousViewportOffset := 0

	// Helper to pad string to width
	padRight := func(s string, width int) string {
		if len(s) >= width {
			return s
		}
		return s + strings.Repeat(" ", width-len(s))
	}

	// Format a single area line to match the menu layout
	formatAreaLine := func(item areaListItem, selected bool, tagged bool) string {
		paddingStr := strings.Repeat(" ", leftPadding)

		if item.isHeader {
			// Conference header - cyan color with one space before name
			return fmt.Sprintf("%s\x1b[0;96m %s\x1b[0m", paddingStr, item.confName)
		}

		prefix := "   "
		if selected {
			prefix = " > "
		}

		statusIcon := " "
		if tagged {
			statusIcon = "\xFB" // CP437 checkmark (√)
		}

		// Use plain grey for unselected, dark cyan bg + white text for selected
		colorSeq := "\x1b[37m" // Plain grey (ANSI 37)
		if selected {
			colorSeq = "\x1b[97;46m" // Bright white text on cyan background
		}

		// Truncate area name if too long
		areaName := item.area.Name
		if len(areaName) > 40 {
			areaName = areaName[:37] + "..."
		}

		var builder strings.Builder
		builder.WriteString(paddingStr) // Add left padding for centering
		builder.WriteString(colorSeq)
		builder.WriteString(prefix)
		builder.WriteString(padRight(areaName, 40))
		builder.WriteString(" [")

		if tagged {
			builder.WriteString("\x1b[96m") // Bright cyan for checkmark
			builder.WriteString(statusIcon)
			builder.WriteString(colorSeq)
		} else {
			builder.WriteString(statusIcon)
		}

		builder.WriteString("]")
		builder.WriteString(" ")
		builder.WriteString("\x1b[0m")

		return builder.String()
	}

	// Adjust viewport to ensure currentIdx is visible
	adjustViewport := func() {
		if currentIdx < viewportOffset {
			viewportOffset = currentIdx
			// Include conference header if present
			if currentIdx > 0 && accessibleAreas[currentIdx-1].isHeader {
				viewportOffset = currentIdx - 1
			}
		}
		if currentIdx >= viewportOffset+availableRows {
			viewportOffset = currentIdx - availableRows + 1
		}
		if viewportOffset < 0 {
			viewportOffset = 0
		}
		maxOffset := len(accessibleAreas) - availableRows
		if maxOffset < 0 {
			maxOffset = 0
		}
		if viewportOffset > maxOffset {
			viewportOffset = maxOffset
		}
	}

	// Draw only the viewport items
	drawItems := func() {
		// Position cursor after header + blank line
		itemStartLine := headerLines + 2 // +1 for header, +1 for blank line
		terminalio.WriteProcessedBytes(terminal, []byte(fmt.Sprintf("\x1b[%d;1H", itemStartLine)), outputMode)

		endIdx := viewportOffset + availableRows
		if endIdx > len(accessibleAreas) {
			endIdx = len(accessibleAreas)
		}

		lineNum := 0
		for i := viewportOffset; i < endIdx && lineNum < availableRows; i++ {
			item := accessibleAreas[i]
			selected := (i == currentIdx)
			tagged := false
			if !item.isHeader && item.area != nil {
				tagged = taggedMap[item.area.Tag]
			}

			// Clear line and draw
			terminalio.WriteProcessedBytes(terminal, []byte("\x1b[2K"), outputMode)
			line := formatAreaLine(item, selected, tagged)
			terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(line+"\r\n")), outputMode)
			lineNum++
		}

		// Clear remaining lines in viewport
		for lineNum < availableRows {
			terminalio.WriteProcessedBytes(terminal, []byte("\x1b[2K\r\n"), outputMode)
			lineNum++
		}
	}

	// Smart redraw - only updates changed lines
	smartRedraw := func() {
		if previousViewportOffset != viewportOffset {
			// Viewport changed - full redraw of items
			drawItems()
		} else if previousIdx >= viewportOffset && previousIdx < viewportOffset+availableRows &&
			currentIdx >= viewportOffset && currentIdx < viewportOffset+availableRows {
			// Same viewport, just selection changed - redraw two lines
			// Redraw previous selection
			itemStartLine := headerLines + 2
			prevLineNum := previousIdx - viewportOffset
			terminalio.WriteProcessedBytes(terminal, []byte(fmt.Sprintf("\x1b[%d;1H", itemStartLine+prevLineNum)), outputMode)
			terminalio.WriteProcessedBytes(terminal, []byte("\x1b[2K"), outputMode)
			item := accessibleAreas[previousIdx]
			tagged := false
			if !item.isHeader && item.area != nil {
				tagged = taggedMap[item.area.Tag]
			}
			line := formatAreaLine(item, false, tagged)
			terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(line)), outputMode)

			// Redraw current selection
			currLineNum := currentIdx - viewportOffset
			terminalio.WriteProcessedBytes(terminal, []byte(fmt.Sprintf("\x1b[%d;1H", itemStartLine+currLineNum)), outputMode)
			terminalio.WriteProcessedBytes(terminal, []byte("\x1b[2K"), outputMode)
			item = accessibleAreas[currentIdx]
			tagged = false
			if !item.isHeader && item.area != nil {
				tagged = taggedMap[item.area.Tag]
			}
			line = formatAreaLine(item, true, tagged)
			terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(line)), outputMode)
		} else {
			// Full redraw needed
			drawItems()
		}

		previousIdx = currentIdx
		previousViewportOffset = viewportOffset
	}

	// Initial screen draw
	terminalio.WriteProcessedBytes(terminal, []byte(ansi.ClearScreen()), outputMode)

	// Try to display ANSI header
	ansPath := filepath.Join(e.MenuSetPath, "ansi", "NEWSCAN.ANS")
	headerContent, ansErr := ansi.GetAnsiFileContent(ansPath)
	if ansErr == nil {
		// For CP437 mode, write raw bytes directly to avoid UTF-8 false positives
		if outputMode == ansi.OutputModeCP437 {
			terminal.Write(headerContent)
		} else {
			terminalio.WriteProcessedBytes(terminal, headerContent, outputMode)
		}
	} else {
		// Fallback to text header
		header := "|15Newscan Configuration|07\r\n" +
			"|08" + strings.Repeat("-", 40) + "|07\r\n" +
			"|07Tag areas to scan for new messages|07\r\n"
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(header)), outputMode)
	}

	// Find next selectable index (skip headers)
	findNextSelectable := func(startIdx int, direction int) int {
		idx := startIdx
		for {
			idx += direction
			if idx < 0 || idx >= len(accessibleAreas) {
				return startIdx // Can't move
			}
			if !accessibleAreas[idx].isHeader {
				return idx
			}
		}
	}

	// Hide cursor for cleaner display
	terminalio.WriteProcessedBytes(terminal, []byte("\x1b[?25l"), outputMode)

	// Ensure cursor is restored when we exit
	defer terminalio.WriteProcessedBytes(terminal, []byte("\x1b[?25h"), outputMode)

	adjustViewport()
	drawItems()

	// Draw footer at bottom with centering (with blank line above it)
	footerLine := termHeight // Footer on last line, blank line naturally above it
	terminalio.WriteProcessedBytes(terminal, []byte(fmt.Sprintf("\x1b[%d;1H", footerLine)), outputMode)

	// Match footer style: Cyan command + Yellow colon + White description
	footerText := "\x1b[36mSPACE\x1b[93m:\x1b[37mToggle  " +
		"\x1b[36mA\x1b[93m:\x1b[37mAll  " +
		"\x1b[36mN\x1b[93m:\x1b[37mNone  " +
		"\x1b[36mESC\x1b[93m:\x1b[37mExit\x1b[0m"

	// Center the footer (approximate visible length without ANSI codes)
	footerVisibleLen := 34 // "SPACE:Toggle  A:All  N:None  ESC:Exit"
	footerPadding := (termWidth - footerVisibleLen) / 2
	if footerPadding > 0 {
		terminalio.WriteProcessedBytes(terminal, []byte(strings.Repeat(" ", footerPadding)), outputMode)
	}
	terminalio.WriteProcessedBytes(terminal, []byte(footerText), outputMode)

	for {
		key, err := sessionIH.ReadKey()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil, "LOGOFF", io.EOF
			}
			return nil, "", err
		}

		switch key {
		case editor.KeyArrowUp, editor.KeyCtrlE: // Up arrow
			newIdx := findNextSelectable(currentIdx, -1)
			if newIdx != currentIdx {
				currentIdx = newIdx
				adjustViewport()
				smartRedraw()
			}

		case editor.KeyArrowDown, editor.KeyCtrlX: // Down arrow
			newIdx := findNextSelectable(currentIdx, 1)
			if newIdx != currentIdx {
				currentIdx = newIdx
				adjustViewport()
				smartRedraw()
			}

		case editor.KeyPageUp, editor.KeyCtrlR: // Page Up
			moved := 0
			newIdx := currentIdx
			for moved < availableRows && newIdx > 0 {
				testIdx := findNextSelectable(newIdx, -1)
				if testIdx == newIdx {
					break
				}
				newIdx = testIdx
				moved++
			}
			if newIdx != currentIdx {
				currentIdx = newIdx
				adjustViewport()
				drawItems()
				previousIdx = currentIdx
				previousViewportOffset = viewportOffset
			}

		case editor.KeyPageDown, editor.KeyCtrlC: // Page Down
			moved := 0
			newIdx := currentIdx
			for moved < availableRows && newIdx < len(accessibleAreas)-1 {
				testIdx := findNextSelectable(newIdx, 1)
				if testIdx == newIdx {
					break
				}
				newIdx = testIdx
				moved++
			}
			if newIdx != currentIdx {
				currentIdx = newIdx
				adjustViewport()
				drawItems()
				previousIdx = currentIdx
				previousViewportOffset = viewportOffset
			}

		case ' ', editor.KeyEnter: // Space or Enter - toggle
			if !accessibleAreas[currentIdx].isHeader {
				area := accessibleAreas[currentIdx].area
				if taggedMap[area.Tag] {
					// Untag
					delete(taggedMap, area.Tag)
				} else {
					// Tag
					taggedMap[area.Tag] = true
				}
				// Redraw just current line
				itemStartLine := headerLines + 2
				currLineNum := currentIdx - viewportOffset
				terminalio.WriteProcessedBytes(terminal, []byte(fmt.Sprintf("\x1b[%d;1H", itemStartLine+currLineNum)), outputMode)
				terminalio.WriteProcessedBytes(terminal, []byte("\x1b[2K"), outputMode)
				line := formatAreaLine(accessibleAreas[currentIdx], true, taggedMap[area.Tag])
				terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(line)), outputMode)
			}

		case 'A', 'a': // Tag all
			for _, item := range accessibleAreas {
				if !item.isHeader {
					taggedMap[item.area.Tag] = true
				}
			}
			drawItems()

		case 'N', 'n': // Untag all
			taggedMap = make(map[string]bool)
			drawItems()

		case editor.KeyEsc, 'Q', 'q': // ESC or Q - exit
			// Save tagged areas to user
			var taggedTags []string
			for tag := range taggedMap {
				taggedTags = append(taggedTags, tag)
			}
			currentUser.TaggedMessageAreaTags = taggedTags

			terminalio.WriteProcessedBytes(terminal, []byte(ansi.ClearScreen()), outputMode)
			if err := userManager.UpdateUser(currentUser); err != nil {
				slog.Error("failed to save newscan config", "node", nodeNumber, "error", err)
				terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(e.LoadedStrings.ScanConfigError)), outputMode)
			} else {
				msg := fmt.Sprintf(e.LoadedStrings.ScanConfigSaved, len(taggedTags))
				terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
			}
			time.Sleep(1 * time.Second)
			return currentUser, "", nil
		}
	}
}
