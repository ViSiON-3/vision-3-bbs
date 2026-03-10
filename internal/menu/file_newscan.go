package menu

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/gliderlabs/ssh"
	"golang.org/x/term"

	"github.com/stlalpha/vision3/internal/ansi"
	"github.com/stlalpha/vision3/internal/editor"
	"github.com/stlalpha/vision3/internal/file"
	"github.com/stlalpha/vision3/internal/terminalio"
	"github.com/stlalpha/vision3/internal/user"
)

// runFileNewscan scans file areas for files uploaded since the user's last login.
func runFileNewscan(e *MenuExecutor, s ssh.Session, terminal *term.Terminal,
	userManager *user.UserMgr, currentUser *user.User, nodeNumber int,
	sessionStartTime time.Time, args string, outputMode ansi.OutputMode,
	termWidth int, termHeight int) (*user.User, string, error) {

	if currentUser == nil {
		return currentUser, "", nil
	}

	log.Printf("INFO: Node %d: FILE_NEWSCAN for user %s (last login: %s, args: %q)",
		nodeNumber, currentUser.Handle, currentUser.LastLogin.Format(time.RFC3339), args)

	since := currentUser.LastLogin

	// Determine which areas to scan
	var areas []file.FileArea
	if strings.EqualFold(args, "CURRENT") {
		area, ok := e.FileMgr.GetAreaByID(currentUser.CurrentFileAreaID)
		if ok {
			areas = []file.FileArea{*area}
		}
	} else {
		// Scan all accessible areas
		for _, area := range e.FileMgr.ListAreas() {
			if checkACS(area.ACSList, currentUser, s, terminal, sessionStartTime) {
				areas = append(areas, area)
			}
		}
	}

	// Load templates (FILESCAN.TOP, FILESCAN.MID, FILESCAN.BOT, FILESCAN.AREA)
	// Fall back to hardcoded defaults if files don't exist.
	scanLine := string(bytes.Repeat([]byte{0xC4}, 63)) // CP437 horizontal line ─
	defaultTop := []byte("|15File Newscan |07- new files since |11@DATE@|07\r\n|08" + scanLine + "|07\r\n")
	defaultMid := []byte("|15^NAME |07^DATE ^SIZE |03^DESC\r\n")
	defaultBot := []byte("|08" + scanLine + "|07\r\n")
	defaultArea := []byte("\r\n|11@AREA@ |07(@COUNT@ new)\r\n")

	topBytes, err := readTemplateFile(filepath.Join(e.MenuSetPath, "templates", "FILESCAN.TOP"))
	if err != nil {
		topBytes = defaultTop
	}
	midBytes, err := readTemplateFile(filepath.Join(e.MenuSetPath, "templates", "FILESCAN.MID"))
	if err != nil {
		midBytes = defaultMid
	}
	botBytes, err := readTemplateFile(filepath.Join(e.MenuSetPath, "templates", "FILESCAN.BOT"))
	if err != nil {
		botBytes = defaultBot
	}
	areaHdrBytes, err := readTemplateFile(filepath.Join(e.MenuSetPath, "templates", "FILESCAN.AREA"))
	if err != nil {
		areaHdrBytes = defaultArea
	}

	// Apply common tokens to templates
	topBytes = e.applyCommonTemplateTokens(topBytes, currentUser, nodeNumber)
	midTemplate := string(ansi.ReplacePipeCodes(midBytes))
	botRendered := ansi.ReplacePipeCodes(e.applyCommonTemplateTokens(botBytes, currentUser, nodeNumber))

	// Replace date placeholder in header
	topRendered := strings.ReplaceAll(string(topBytes), "@DATE@", since.Format("01/02/2006"))
	terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(topRendered)), outputMode)

	totalNew := 0
	lineCount := 0
	pausePrompt := e.LoadedStrings.PauseString
	pageLines := termHeight - 4
	if pageLines < 5 {
		pageLines = 5
	}

	for _, area := range areas {
		newFiles := e.FileMgr.GetFilesNewerThan(area.ID, since)
		if len(newFiles) == 0 {
			continue
		}

		// Area header from template
		areaHdr := string(areaHdrBytes)
		areaHdr = strings.ReplaceAll(areaHdr, "@AREA@", area.Name)
		areaHdr = strings.ReplaceAll(areaHdr, "@COUNT@", fmt.Sprintf("%d", len(newFiles)))
		terminalio.WriteProcessedBytes(terminal,
			ansi.ReplacePipeCodes([]byte(areaHdr)), outputMode)
		lineCount += 2

		for _, f := range newFiles {
			desc := f.Description
			if idx := strings.IndexAny(desc, "\r\n"); idx >= 0 {
				desc = desc[:idx]
			}
			desc = strings.TrimSpace(desc)
			maxDesc := termWidth - 40
			if maxDesc < 10 {
				maxDesc = 10
			}
			if len(desc) > maxDesc {
				desc = desc[:maxDesc-3] + "..."
			}

			fname := f.Filename
			if len(fname) > 12 {
				fname = fname[:12]
			}

			line := midTemplate
			line = strings.ReplaceAll(line, "^NAME", fmt.Sprintf("%-12s", fname))
			line = strings.ReplaceAll(line, "^DATE", f.UploadedAt.Format("01/02/06"))
			line = strings.ReplaceAll(line, "^SIZE", fmt.Sprintf("%5dk", (f.Size+1023)/1024))
			line = strings.ReplaceAll(line, "^DESC", desc)

			terminalio.WriteProcessedBytes(terminal, []byte(line), outputMode)
			lineCount++
			totalNew++

			if lineCount >= pageLines {
				lineCount = 0
				err := writeCenteredPausePrompt(s, terminal, pausePrompt, outputMode, termWidth, termHeight)
				if err != nil {
					if err == io.EOF {
						return currentUser, "", nil
					}
					return currentUser, "", err
				}
			}
		}
	}

	// Footer
	terminalio.WriteProcessedBytes(terminal, botRendered, outputMode)

	// Summary
	if totalNew == 0 {
		terminalio.WriteProcessedBytes(terminal,
			ansi.ReplacePipeCodes([]byte(e.LoadedStrings.FileNewscanNoNew)), outputMode)
	} else {
		msg := fmt.Sprintf(e.LoadedStrings.FileNewscanComplete, totalNew)
		terminalio.WriteProcessedBytes(terminal,
			ansi.ReplacePipeCodes([]byte(msg)), outputMode)
	}

	_ = writeCenteredPausePrompt(s, terminal, pausePrompt, outputMode, termWidth, termHeight)

	return currentUser, "", nil
}

// fileAreaListItem represents an item in the file newscan config list (area or conference header)
type fileAreaListItem struct {
	area     *file.FileArea
	confName string
	isHeader bool
}

// runFileNewscanConfig allows users to tag/untag file areas for their personal file newscan.
func runFileNewscanConfig(e *MenuExecutor, s ssh.Session, terminal *term.Terminal,
	userManager *user.UserMgr, currentUser *user.User, nodeNumber int,
	sessionStartTime time.Time, args string, outputMode ansi.OutputMode, termWidth int, termHeight int) (*user.User, string, error) {

	if currentUser == nil {
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(e.LoadedStrings.ScanConfigLoginRequired)), outputMode)
		time.Sleep(1 * time.Second)
		return nil, "", nil
	}

	sessionIH := getSessionIH(s)

	allAreas := e.FileMgr.ListAreas()
	if len(allAreas) == 0 {
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(e.LoadedStrings.ScanNoAreasAvailable)), outputMode)
		time.Sleep(1 * time.Second)
		return nil, "", nil
	}

	confNameMap := make(map[int]string)
	confPosMap := make(map[int]int)
	if e.ConferenceMgr != nil {
		for _, conf := range e.ConferenceMgr.ListConferences() {
			confNameMap[conf.ID] = conf.Name
			confPosMap[conf.ID] = conf.Position
		}
	}

	sort.Slice(allAreas, func(i, j int) bool {
		ai, aj := allAreas[i], allAreas[j]
		ci, oki := confPosMap[ai.ConferenceID]
		cj, okj := confPosMap[aj.ConferenceID]
		if !oki || ci <= 0 { ci = 1<<31 - 1 }
		if !okj || cj <= 0 { cj = 1<<31 - 1 }
		if ci != cj { return ci < cj }
		if ai.ConferenceID != aj.ConferenceID { return ai.ConferenceID < aj.ConferenceID }
		return ai.ID < aj.ID
	})

	var accessibleAreas []fileAreaListItem
	currentConfID := -1
	for i := range allAreas {
		area := &allAreas[i]
		if !checkACS(area.ACSList, currentUser, s, terminal, sessionStartTime) { continue }
		if area.ConferenceID != currentConfID {
			currentConfID = area.ConferenceID
			confName := confNameMap[area.ConferenceID]
			if confName == "" { confName = "General" }
			accessibleAreas = append(accessibleAreas, fileAreaListItem{confName: confName, isHeader: true})
		}
		accessibleAreas = append(accessibleAreas, fileAreaListItem{area: area, confName: confNameMap[area.ConferenceID]})
	}

	if len(accessibleAreas) == 0 {
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(e.LoadedStrings.ScanNoAccessibleAreas)), outputMode)
		time.Sleep(1 * time.Second)
		return nil, "", nil
	}

	taggedMap := make(map[string]bool)
	for _, tag := range currentUser.TaggedFileAreaTags {
		taggedMap[tag] = true
	}

	currentIdx := 0
	for currentIdx < len(accessibleAreas) && accessibleAreas[currentIdx].isHeader { currentIdx++ }

	if termHeight <= 0 { termHeight = currentUser.ScreenHeight }
	if termHeight <= 0 { termHeight = 24 }
	if termWidth <= 0 { termWidth = currentUser.ScreenWidth }
	if termWidth <= 0 { termWidth = 80 }

	contentWidth := 48
	leftPadding := (termWidth - contentWidth) / 2
	if leftPadding < 0 { leftPadding = 0 }

	headerLines := 6
	availableRows := termHeight - headerLines - 3
	if availableRows < 5 { availableRows = 5 }

	viewportOffset := 0
	previousIdx := -1
	previousViewportOffset := 0

	padRight := func(s string, width int) string {
		if len(s) >= width { return s }
		return s + strings.Repeat(" ", width-len(s))
	}

	formatAreaLine := func(item fileAreaListItem, selected bool, tagged bool) string {
		paddingStr := strings.Repeat(" ", leftPadding)
		if item.isHeader {
			return fmt.Sprintf("%s\x1b[0;96m %s\x1b[0m", paddingStr, item.confName)
		}
		prefix := "   "
		if selected { prefix = " > " }
		statusIcon := " "
		if tagged { statusIcon = "\xFB" }
		colorSeq := "\x1b[37m"
		if selected { colorSeq = "\x1b[97;46m" }
		areaName := item.area.Name
		if len(areaName) > 40 { areaName = areaName[:37] + "..." }
		var b strings.Builder
		b.WriteString(paddingStr)
		b.WriteString(colorSeq)
		b.WriteString(prefix)
		b.WriteString(padRight(areaName, 40))
		b.WriteString(" [")
		if tagged { b.WriteString("\x1b[96m"); b.WriteString(statusIcon); b.WriteString(colorSeq) } else { b.WriteString(statusIcon) }
		b.WriteString("] \x1b[0m")
		return b.String()
	}

	adjustViewport := func() {
		if currentIdx < viewportOffset {
			viewportOffset = currentIdx
			if currentIdx > 0 && accessibleAreas[currentIdx-1].isHeader { viewportOffset = currentIdx - 1 }
		}
		if currentIdx >= viewportOffset+availableRows { viewportOffset = currentIdx - availableRows + 1 }
		if viewportOffset < 0 { viewportOffset = 0 }
		maxOffset := len(accessibleAreas) - availableRows
		if maxOffset < 0 { maxOffset = 0 }
		if viewportOffset > maxOffset { viewportOffset = maxOffset }
	}

	drawItems := func() {
		itemStartLine := headerLines + 2
		terminalio.WriteProcessedBytes(terminal, []byte(fmt.Sprintf("\x1b[%d;1H", itemStartLine)), outputMode)
		endIdx := viewportOffset + availableRows
		if endIdx > len(accessibleAreas) { endIdx = len(accessibleAreas) }
		lineNum := 0
		for i := viewportOffset; i < endIdx && lineNum < availableRows; i++ {
			item := accessibleAreas[i]
			tagged := false
			if !item.isHeader && item.area != nil { tagged = taggedMap[item.area.Tag] }
			terminalio.WriteProcessedBytes(terminal, []byte("\x1b[2K"), outputMode)
			line := formatAreaLine(item, i == currentIdx, tagged)
			terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(line+"\r\n")), outputMode)
			lineNum++
		}
		for lineNum < availableRows {
			terminalio.WriteProcessedBytes(terminal, []byte("\x1b[2K\r\n"), outputMode)
			lineNum++
		}
	}

	smartRedraw := func() {
		if previousViewportOffset != viewportOffset {
			drawItems()
		} else if previousIdx >= viewportOffset && previousIdx < viewportOffset+availableRows &&
			currentIdx >= viewportOffset && currentIdx < viewportOffset+availableRows {
			itemStartLine := headerLines + 2
			for _, idx := range []int{previousIdx, currentIdx} {
				ln := idx - viewportOffset
				terminalio.WriteProcessedBytes(terminal, []byte(fmt.Sprintf("\x1b[%d;1H\x1b[2K", itemStartLine+ln)), outputMode)
				item := accessibleAreas[idx]
				tagged := false
				if !item.isHeader && item.area != nil { tagged = taggedMap[item.area.Tag] }
				line := formatAreaLine(item, idx == currentIdx, tagged)
				terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(line)), outputMode)
			}
		} else {
			drawItems()
		}
		previousIdx = currentIdx
		previousViewportOffset = viewportOffset
	}

	terminalio.WriteProcessedBytes(terminal, []byte(ansi.ClearScreen()), outputMode)
	ansPath := filepath.Join(e.MenuSetPath, "ansi", "FILESCAN.ANS")
	headerContent, ansErr := ansi.GetAnsiFileContent(ansPath)
	if ansErr == nil {
		if outputMode == ansi.OutputModeCP437 {
			terminal.Write(headerContent)
		} else {
			terminalio.WriteProcessedBytes(terminal, headerContent, outputMode)
		}
	} else {
		header := e.LoadedStrings.FileNewscanConfigHeader
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(header)), outputMode)
	}

	findNextSelectable := func(startIdx int, direction int) int {
		idx := startIdx
		for { idx += direction; if idx < 0 || idx >= len(accessibleAreas) { return startIdx }; if !accessibleAreas[idx].isHeader { return idx } }
	}

	terminalio.WriteProcessedBytes(terminal, []byte("\x1b[?25l"), outputMode)
	defer terminalio.WriteProcessedBytes(terminal, []byte("\x1b[?25h"), outputMode)

	adjustViewport()
	drawItems()

	footerLine := termHeight
	terminalio.WriteProcessedBytes(terminal, []byte(fmt.Sprintf("\x1b[%d;1H", footerLine)), outputMode)
	footerText := "\x1b[36mSPACE\x1b[93m:\x1b[37mToggle  \x1b[36mA\x1b[93m:\x1b[37mAll  \x1b[36mN\x1b[93m:\x1b[37mNone  \x1b[36mESC\x1b[93m:\x1b[37mExit\x1b[0m"
	footerPadding := (termWidth - 34) / 2
	if footerPadding > 0 { terminalio.WriteProcessedBytes(terminal, []byte(strings.Repeat(" ", footerPadding)), outputMode) }
	terminalio.WriteProcessedBytes(terminal, []byte(footerText), outputMode)

	for {
		key, err := sessionIH.ReadKey()
		if err != nil {
			if errors.Is(err, io.EOF) { return nil, "LOGOFF", io.EOF }
			return nil, "", err
		}
		switch key {
		case editor.KeyArrowUp, editor.KeyCtrlE:
			if n := findNextSelectable(currentIdx, -1); n != currentIdx { currentIdx = n; adjustViewport(); smartRedraw() }
		case editor.KeyArrowDown, editor.KeyCtrlX:
			if n := findNextSelectable(currentIdx, 1); n != currentIdx { currentIdx = n; adjustViewport(); smartRedraw() }
		case editor.KeyPageUp, editor.KeyCtrlR:
			n := currentIdx; for m := 0; m < availableRows && n > 0; m++ { t := findNextSelectable(n, -1); if t == n { break }; n = t }
			if n != currentIdx { currentIdx = n; adjustViewport(); drawItems(); previousIdx = currentIdx; previousViewportOffset = viewportOffset }
		case editor.KeyPageDown, editor.KeyCtrlC:
			n := currentIdx; for m := 0; m < availableRows && n < len(accessibleAreas)-1; m++ { t := findNextSelectable(n, 1); if t == n { break }; n = t }
			if n != currentIdx { currentIdx = n; adjustViewport(); drawItems(); previousIdx = currentIdx; previousViewportOffset = viewportOffset }
		case ' ', editor.KeyEnter:
			if !accessibleAreas[currentIdx].isHeader {
				area := accessibleAreas[currentIdx].area
				if taggedMap[area.Tag] { delete(taggedMap, area.Tag) } else { taggedMap[area.Tag] = true }
				ln := currentIdx - viewportOffset
				terminalio.WriteProcessedBytes(terminal, []byte(fmt.Sprintf("\x1b[%d;1H\x1b[2K", headerLines+2+ln)), outputMode)
				line := formatAreaLine(accessibleAreas[currentIdx], true, taggedMap[area.Tag])
				terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(line)), outputMode)
			}
		case 'A', 'a':
			for _, item := range accessibleAreas { if !item.isHeader { taggedMap[item.area.Tag] = true } }
			drawItems()
		case 'N', 'n':
			taggedMap = make(map[string]bool); drawItems()
		case editor.KeyEsc, 'Q', 'q':
			var taggedTags []string
			for tag := range taggedMap { taggedTags = append(taggedTags, tag) }
			currentUser.TaggedFileAreaTags = taggedTags
			terminalio.WriteProcessedBytes(terminal, []byte(ansi.ClearScreen()), outputMode)
			if err := userManager.UpdateUser(currentUser); err != nil {
				log.Printf("ERROR: Node %d: Failed to save file newscan config: %v", nodeNumber, err)
				terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(e.LoadedStrings.ScanConfigError)), outputMode)
			} else {
				msg := fmt.Sprintf(e.LoadedStrings.FileNewscanConfigSaved, len(taggedTags))
				terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
			}
			time.Sleep(1 * time.Second)
			return currentUser, "", nil
		}
	}
}
