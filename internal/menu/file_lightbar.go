package menu

import (
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gliderlabs/ssh"
	"github.com/google/uuid"
	"golang.org/x/term"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/editor"
	"github.com/ViSiON-3/vision-3-bbs/internal/file"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
	"github.com/ViSiON-3/vision-3-bbs/internal/ziplab"
)

// fileListPlaceholderRegex matches @FPAGE@, @FTOTAL@, @FCONFPATH@ with optional alignment and width.
// Modifier: | (0x7C) or │ (CP437 0xB3, common in ANSI art) followed by L/R/C — matches message-header format.
// Groups: 1=code (FPAGE|FTOTAL|FCONFPATH), 2=modifier (L|R|C), 3=:N digits, 4=# sequence
var fileListPlaceholderRegex = regexp.MustCompile(`@(FPAGE|FTOTAL|FCONFPATH)(?:[\x7C\xB3]([LRC]))?(?::(\d+)|(#+))?@`)

// processFileListPlaceholders replaces file-list-specific pipe codes and @-placeholders
// with current page, total pages, total file count, and conference path. Use in FILELIST.TOP and FILELIST.BOT.
// Pipe codes: |FPAGE ("Page X of Y"), |FTOTAL (total file count), |FCONFPATH (Conference > File Area).
// Placeholders support alignment modifiers: @FPAGE|R###@, @FTOTAL|C:5@, @FCONFPATH|R############@
// fconfpath is the pre-formatted "Conference > Area" string with pipe codes (from resolveFileConferencePath).
func processFileListPlaceholders(data []byte, currentPage, totalPages, totalFiles int, fconfpath string) []byte {
	s := string(data)
	pageStr := fmt.Sprintf("Page %d of %d", currentPage, totalPages)
	totalStr := strconv.Itoa(totalFiles)

	// Process @-placeholders FIRST so |FPAGE inside @FPAGE|R#####@ isn't consumed by pipe codes.
	// @CODE@ placeholders with optional alignment modifier (|L, |R, |C) and width (:N or ###)
	// For ###: width = total placeholder length (entire token) so replacement preserves ANSI layout.
	// E.g. @FPAGE|R###########@ is 20 cols — output is padded/truncated to 20 visible chars.
	s = fileListPlaceholderRegex.ReplaceAllStringFunc(s, func(match string) string {
		subs := fileListPlaceholderRegex.FindStringSubmatch(match)
		if len(subs) < 2 {
			return match
		}
		code := subs[1]
		modifier := ""
		if len(subs) > 2 {
			modifier = subs[2]
		}
		width := 0
		if len(subs) > 3 && subs[3] != "" {
			width, _ = strconv.Atoi(subs[3])
		} else if len(subs) > 4 && subs[4] != "" {
			// Visual width: entire placeholder length (matches message-header / editor placeholder behavior)
			width = len(match)
		}
		align := ansi.AlignLeft
		if modifier != "" {
			align = ansi.ParseAlignment(modifier)
		}

		var value string
		switch code {
		case "FPAGE":
			value = pageStr
		case "FTOTAL":
			value = totalStr
		case "FCONFPATH":
			value = fconfpath
		default:
			return match
		}
		if width <= 0 {
			return value
		}
		return ansi.ApplyWidthConstraintAligned(value, width, align)
	})

	// Pipe codes AFTER @-placeholders so |FPAGE inside @FPAGE|R#####@ isn't destroyed.
	s = strings.ReplaceAll(s, "|FPAGE", pageStr)
	s = strings.ReplaceAll(s, "|FTOTAL", totalStr)
	s = strings.ReplaceAll(s, "|FCONFPATH", fconfpath)

	return []byte(s)
}

// fileLightbar holds the state for the interactive file-area browser.
// runListFilesLightbar builds one and drives it via run().
type fileLightbar struct {
	e                    *MenuExecutor
	s                    ssh.Session
	terminal             *term.Terminal
	userManager          *user.UserMgr
	currentUser          *user.User
	nodeNumber           int
	sessionStartTime     time.Time
	currentAreaID        int
	currentAreaTag       string
	area                 *file.FileArea
	outputMode           ansi.OutputMode
	ih                   *editor.InputHandler
	topTemplateBytes     []byte
	processedMidTemplate string
	processedBotTemplate []byte
	filesPerPage         int
	totalFiles           int
	totalPages           int
	allFiles             []file.FileRecord
	cmdEntries           []cmdEntry
	sysopEntries         []cmdEntry
	userEntries          []cmdEntry
	hiColorSeq           string
	isSysop              bool
	showSysopBar         bool
	termHeight           int
	termWidth            int
	fconfpath            string
	headerLines          int
	botContent           string
	botLineCount         int
	reservedBottom       int
	visibleRows          int
	ansiRe               *regexp.Regexp
	descPrefixLen        int
	descColWidth         int
	descIndentStr        string
	fileAreaStartRow     int
	cmdBarRow            int
	separatorRow         int
	selectedIndex        int
	topIndex             int
	cmdIndex             int
}

func runListFilesLightbar(e *MenuExecutor, s ssh.Session, terminal *term.Terminal,
	userManager *user.UserMgr, currentUser *user.User, nodeNumber int, sessionStartTime time.Time,
	currentAreaID int, currentAreaTag string, area *file.FileArea,
	topTemplateBytes []byte, processedMidTemplate string, processedBotTemplate []byte,
	filesPerPage int, totalFiles int, totalPages int,
	cmdBarOptions []LightbarOption, hiBarOptions []LightbarOption,
	outputMode ansi.OutputMode) (*user.User, string, error) {

	// Hide cursor on entry, show on exit.
	_ = terminalio.WriteProcessedBytes(terminal, []byte("\x1b[?25l"), outputMode)
	defer terminalio.WriteProcessedBytes(terminal, []byte("\x1b[?25h"), outputMode)

	// Fetch all files for the area.
	allFiles := e.FileMgr.GetFilesForArea(currentAreaID)

	selectedIndex := 0
	topIndex := 0
	cmdIndex := 0
	ih := getSessionIH(s)

	// Build command bar entries (user bar, sysop bar) and the file-row highlight.
	cmdEntries, sysopEntries, userEntries, hiColorSeq, isSysop := buildFileListCmdBar(e, currentUser, cmdBarOptions, hiBarOptions)
	showSysopBar := false

	// Determine terminal dimensions.
	termHeight := 24
	termWidth := 80
	if ptyReq, _, ok := s.Pty(); ok && ptyReq.Window.Height > 0 {
		termHeight = ptyReq.Window.Height
		if ptyReq.Window.Width > 0 {
			termWidth = ptyReq.Window.Width
		}
	}

	// Count header lines from top template (line count is invariant to page/file counts).
	fconfpath := e.resolveFileConferencePath(currentUser)
	processedTopSample := ansi.ReplacePipeCodes(processFileListPlaceholders(topTemplateBytes, 1, 1, totalFiles, fconfpath))
	headerLines := strings.Count(string(processedTopSample), "\n")
	if headerLines < 1 {
		headerLines = 1
	}

	// Reserve rows for the separator, command bar, and optional BOT template.
	// Derive botLineCount from the expanded string (after placeholder + pipe-code
	// processing) so it matches what renderPageIndicator actually renders.
	botContent := strings.TrimRight(string(processedBotTemplate), "\r\n")
	botLineCount := 0
	if len(botContent) > 0 {
		expandedBotSample := string(ansi.ReplacePipeCodes(processFileListPlaceholders([]byte(botContent), 1, 1, totalFiles, fconfpath)))
		expandedBotSample = strings.ReplaceAll(expandedBotSample, "^PAGE", "1")
		expandedBotSample = strings.ReplaceAll(expandedBotSample, "^TOTALPAGES", "1")
		botLineCount = len(strings.Split(expandedBotSample, "\n"))
	}
	reservedBottom := 2 // separator + command bar
	if botLineCount > 0 {
		reservedBottom = 2 + botLineCount // separator + command bar + BOT lines
	}
	visibleRows := termHeight - headerLines - reservedBottom - 1
	if visibleRows < 3 {
		visibleRows = 3
	}

	// stripAnsi removes all ANSI escape sequences from a string.
	ansiRe := regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

	// All template placeholders are fixed-width, so the description column
	// width is constant across all file entries. Precompute once for both
	// fileEntryHeight and buildFileEntry.
	sample := strings.ReplaceAll(processedMidTemplate, "^MARK", " ")
	sample = strings.ReplaceAll(sample, "^NUM", "  1")
	sample = strings.ReplaceAll(sample, "^NAME", "            ")
	sample = strings.ReplaceAll(sample, "^DATE", "01/01/00")
	sample = strings.ReplaceAll(sample, "^SIZE", "     ")
	sample = strings.ReplaceAll(sample, "^DESC", "")
	descPrefixLen := len(ansiRe.ReplaceAllString(string(ansi.ReplacePipeCodes([]byte(sample))), ""))
	descColWidth := termWidth - descPrefixLen - 1
	if descColWidth < 20 {
		descColWidth = 20
	}
	descIndentStr := strings.Repeat(" ", descPrefixLen)

	// fileEntryHeight returns the number of screen lines a file at idx takes:
	// first line (metadata + first DIZ line) + continuation DIZ lines.

	// filesVisibleFrom counts how many files fit in visibleRows starting from startIdx,
	// accounting for each entry's variable height (DIZ lines).

	// topIndexForPrevPage walks backward from the current topIndex to find where
	// the previous page should start, filling visibleRows from bottom to top.

	// calculatePageInfo walks all files with variable heights to determine
	// which page topIndex falls on and how many total pages exist.

	// --- Rendering helpers for smart refresh ---

	// fileAreaStartRow is the absolute terminal row where file entries begin.
	fileAreaStartRow := headerLines + 2

	// buildFileEntry produces the ANSI output for a single file entry at the
	// given file index, returning the rendered lines (first line + continuations)
	// already processed through pipe codes.  If highlighted is true the first
	// line uses the highlight color with stripped ANSI (lightbar style).
	// The caller specifies how many screen lines are available (maxLines) so the
	// function can truncate continuation lines to fit.

	// screenRowForFile returns the absolute terminal row a file entry starts on,
	// given the current topIndex.  Returns -1 if the file is not in the viewport.

	// writeFileRow renders a single file entry at the given absolute screen row,
	// clearing the lines it occupies first.

	// renderFileArea redraws only the file list rows using absolute cursor
	// positioning and line-clear (no full screen clear).

	// Layout: separator row, then command bar, then optional BOT.
	cmdBarRow := max(1, termHeight-botLineCount)
	separatorRow := max(1, cmdBarRow-1)

	// renderSeparator draws the separator line above the command bar.
	// Uses CP437 0xFA (·) and 0xC4 (─) to match the header separator style.

	// renderCmdBar redraws only the horizontal command bar.

	// renderPageIndicator redraws only the page/bot indicator row(s).

	// renderTop writes the TOP template with |FPAGE, |FTOTAL, @FPAGE@, @FTOTAL@ substituted.

	// renderFull performs a complete screen redraw (used on first display and
	// after overlay commands that clobber the screen).

	lb := &fileLightbar{
		e:                    e,
		s:                    s,
		terminal:             terminal,
		userManager:          userManager,
		currentUser:          currentUser,
		nodeNumber:           nodeNumber,
		sessionStartTime:     sessionStartTime,
		currentAreaID:        currentAreaID,
		currentAreaTag:       currentAreaTag,
		area:                 area,
		outputMode:           outputMode,
		ih:                   ih,
		topTemplateBytes:     topTemplateBytes,
		processedMidTemplate: processedMidTemplate,
		processedBotTemplate: processedBotTemplate,
		filesPerPage:         filesPerPage,
		totalFiles:           totalFiles,
		totalPages:           totalPages,
		allFiles:             allFiles,
		cmdEntries:           cmdEntries,
		sysopEntries:         sysopEntries,
		userEntries:          userEntries,
		hiColorSeq:           hiColorSeq,
		isSysop:              isSysop,
		showSysopBar:         showSysopBar,
		termHeight:           termHeight,
		termWidth:            termWidth,
		fconfpath:            fconfpath,
		headerLines:          headerLines,
		botContent:           botContent,
		botLineCount:         botLineCount,
		reservedBottom:       reservedBottom,
		visibleRows:          visibleRows,
		ansiRe:               ansiRe,
		descPrefixLen:        descPrefixLen,
		descColWidth:         descColWidth,
		descIndentStr:        descIndentStr,
		fileAreaStartRow:     fileAreaStartRow,
		cmdBarRow:            cmdBarRow,
		separatorRow:         separatorRow,
		selectedIndex:        selectedIndex,
		topIndex:             topIndex,
		cmdIndex:             cmdIndex,
	}
	return lb.run()
}

func (lb *fileLightbar) isFileTagged(fileID uuid.UUID) bool {
	for _, taggedID := range lb.currentUser.TaggedFileIDs {
		if taggedID == fileID {
			return true
		}
	}
	return false
}

func (lb *fileLightbar) stripAnsi(str string) string {
	return lb.ansiRe.ReplaceAllString(str, "")
}

func (lb *fileLightbar) fileEntryHeight(idx int) int {
	if idx < 0 || idx >= len(lb.allFiles) {
		return 1
	}
	dizCount := len(formatDIZLines(lb.allFiles[idx].Description, lb.descColWidth, dizMaxLines))
	if dizCount < 1 {
		return 1
	}
	return dizCount
}

func (lb *fileLightbar) filesVisibleFrom(startIdx int) int {
	usedLines := 0
	count := 0
	for idx := startIdx; idx < len(lb.allFiles) && usedLines < lb.visibleRows; idx++ {
		h := lb.fileEntryHeight(idx)
		if usedLines+1 > lb.visibleRows {
			break
		}
		if usedLines+h > lb.visibleRows {
			h = lb.visibleRows - usedLines // show as many DIZ lines as fit
		}
		usedLines += h
		count++
	}
	return count
}

func (lb *fileLightbar) topIndexForPrevPage() int {
	if lb.topIndex <= 0 {
		return 0
	}
	usedLines := 0
	newTop := lb.topIndex
	for idx := lb.topIndex - 1; idx >= 0; idx-- {
		h := lb.fileEntryHeight(idx)
		if usedLines+h > lb.visibleRows {
			break
		}
		usedLines += h
		newTop = idx
	}
	return newTop
}

func (lb *fileLightbar) calculatePageInfo() (currentPage int, totalPagesCalc int) {
	if len(lb.allFiles) == 0 {
		return 1, 1
	}
	page := 0
	idx := 0
	foundCurrent := false
	for idx < len(lb.allFiles) {
		page++
		usedLines := 0
		pageStart := idx
		for idx < len(lb.allFiles) && usedLines < lb.visibleRows {
			h := lb.fileEntryHeight(idx)
			if usedLines+1 > lb.visibleRows {
				break
			}
			if usedLines+h > lb.visibleRows {
				h = lb.visibleRows - usedLines
			}
			usedLines += h
			idx++
		}
		if !foundCurrent && lb.topIndex >= pageStart && lb.topIndex < idx {
			currentPage = page
			foundCurrent = true
		}
	}
	if !foundCurrent {
		currentPage = page
	}
	totalPagesCalc = page
	return currentPage, totalPagesCalc
}

func (lb *fileLightbar) clampSelection() {
	if len(lb.allFiles) == 0 {
		lb.selectedIndex = 0
		lb.topIndex = 0
		return
	}
	if lb.selectedIndex < 0 {
		lb.selectedIndex = 0
	}
	if lb.selectedIndex >= len(lb.allFiles) {
		lb.selectedIndex = len(lb.allFiles) - 1
	}
	if lb.selectedIndex < lb.topIndex {
		lb.topIndex = lb.selectedIndex
	}
	// Scroll down: advance topIndex until selectedIndex fits within the
	// visible screen area, accounting for multi-line file entries.
	// We keep advancing until the selected entry either fits at full
	// height or is at the very top of the viewport (so large entries
	// like 21-line ANS art are always shown from the top, not crammed
	// at the bottom with only a few lines visible).
	for lb.topIndex <= lb.selectedIndex {
		usedLines := 0
		fits := false
		for idx := lb.topIndex; idx < len(lb.allFiles) && usedLines < lb.visibleRows; idx++ {
			h := lb.fileEntryHeight(idx)
			if usedLines+1 > lb.visibleRows {
				break // can't fit even the first line
			}
			fullH := h
			if usedLines+h > lb.visibleRows {
				h = lb.visibleRows - usedLines
			}
			usedLines += h
			if idx == lb.selectedIndex {
				// Fits fully, or entry is already at top of viewport
				// (can't scroll any higher without losing the selection).
				if h == fullH || idx == lb.topIndex {
					fits = true
				}
				break
			}
		}
		if fits || lb.topIndex >= lb.selectedIndex {
			break
		}
		lb.topIndex++
	}
	if lb.topIndex < 0 {
		lb.topIndex = 0
	}
}

func (lb *fileLightbar) buildFileEntry(idx int, highlighted bool, maxLines int) []string {
	if idx < 0 || idx >= len(lb.allFiles) {
		return nil
	}
	fileRec := lb.allFiles[idx]

	fileNumStr := fmt.Sprintf("%3d", idx+1)
	name := fileRec.Filename
	if r := []rune(name); len(r) > 12 {
		name = string(r[:12])
	}
	fileNameStr := fmt.Sprintf("%-12s", name)
	dateStr := fileRec.UploadedAt.Format("01/02/06")
	sizeStr := fmt.Sprintf("%5s", compactFileSize(fileRec.Size))

	markStr := " "
	if lb.isFileTagged(fileRec.ID) {
		markStr = "*"
	}

	line := lb.processedMidTemplate
	line = strings.ReplaceAll(line, "^MARK", markStr)
	line = strings.ReplaceAll(line, "^NUM", fileNumStr)
	line = strings.ReplaceAll(line, "^NAME", fileNameStr)
	line = strings.ReplaceAll(line, "^DATE", dateStr)
	line = strings.ReplaceAll(line, "^SIZE", sizeStr)

	// Build prefix for highlight rendering (ANSI-stripped).
	prefixLine := strings.ReplaceAll(line, "^DESC", "")
	processedPrefix := string(ansi.ReplacePipeCodes([]byte(prefixLine)))

	dizLines := formatDIZLines(fileRec.Description, lb.descColWidth, dizMaxLines)

	firstDesc := ""
	if len(dizLines) > 0 {
		firstDesc = dizLines[0]
	}

	var contLines []string
	for i := 1; i < len(dizLines); i++ {
		contLines = append(contLines, dizLines[i])
	}

	if len(contLines) > maxLines-1 {
		contLines = contLines[:maxLines-1]
	}

	var result []string

	if highlighted {
		plainPrefix := lb.stripAnsi(processedPrefix)
		plainDesc := lb.stripAnsi(firstDesc)
		rowText := plainPrefix + plainDesc
		visLen := ansi.VisibleLength(rowText)
		if visLen < lb.termWidth {
			rowText += strings.Repeat(" ", lb.termWidth-visLen)
		}
		result = append(result, lb.hiColorSeq+rowText+"\x1b[0m")
	} else {
		fullLine := strings.ReplaceAll(line, "^DESC", firstDesc)
		processed := string(ansi.ReplacePipeCodes([]byte(fullLine)))
		result = append(result, processed)
	}

	for _, cl := range contLines {
		result = append(result, string(ansi.ReplacePipeCodes([]byte("|07"+lb.descIndentStr+cl))))
	}

	return result
}

func (lb *fileLightbar) screenRowForFile(fileIdx int) (startRow int, height int) {
	if fileIdx < lb.topIndex {
		return -1, 0
	}
	row := lb.fileAreaStartRow
	for idx := lb.topIndex; idx < len(lb.allFiles) && (row-lb.fileAreaStartRow) < lb.visibleRows; idx++ {
		h := lb.fileEntryHeight(idx)
		remaining := lb.visibleRows - (row - lb.fileAreaStartRow)
		if h > remaining {
			h = remaining
		}
		if remaining < 1 {
			break
		}
		if idx == fileIdx {
			return row, h
		}
		row += h
	}
	return -1, 0
}

func (lb *fileLightbar) writeFileRow(screenRow int, fileIdx int, highlighted bool, height int) error {
	lines := lb.buildFileEntry(fileIdx, highlighted, height)
	for i, ln := range lines {
		r := screenRow + i
		if err := terminalio.WriteProcessedBytes(lb.terminal, []byte(ansi.MoveCursor(r, 1)+"\x1b[2K"), lb.outputMode); err != nil {
			return err
		}
		if highlighted && i == 0 {
			// First line already has raw ANSI from buildFileEntry; write directly.
			if err := terminalio.WriteProcessedBytes(lb.terminal, []byte(ln), lb.outputMode); err != nil {
				return err
			}
		} else if i == 0 {
			if err := writeProcessedStringWithManualEncoding(lb.terminal, []byte(ln), lb.outputMode); err != nil {
				return err
			}
		} else {
			if err := terminalio.WriteProcessedBytes(lb.terminal, []byte(ln), lb.outputMode); err != nil {
				return err
			}
		}
	}
	// Clear any remaining lines in this entry's allocated height.
	for i := len(lines); i < height; i++ {
		r := screenRow + i
		if err := terminalio.WriteProcessedBytes(lb.terminal, []byte(ansi.MoveCursor(r, 1)+"\x1b[2K"), lb.outputMode); err != nil {
			return err
		}
	}
	return nil
}

func (lb *fileLightbar) renderFileArea() error {
	if err := terminalio.WriteProcessedBytes(lb.terminal, []byte(ansi.MoveCursor(lb.fileAreaStartRow, 1)), lb.outputMode); err != nil {
		return err
	}

	linesUsed := 0
	if len(lb.allFiles) == 0 {
		msg := "|07   No files in this area."
		if err := terminalio.WriteProcessedBytes(lb.terminal, []byte("\x1b[2K"), lb.outputMode); err != nil {
			return err
		}
		if err := terminalio.WriteProcessedBytes(lb.terminal, ansi.ReplacePipeCodes([]byte(msg)), lb.outputMode); err != nil {
			return err
		}
		linesUsed = 1
	} else {
		for idx := lb.topIndex; idx < len(lb.allFiles) && linesUsed < lb.visibleRows; idx++ {
			h := lb.fileEntryHeight(idx)
			remaining := lb.visibleRows - linesUsed
			if h > remaining {
				h = remaining
			}
			if remaining < 1 {
				break
			}
			row := lb.fileAreaStartRow + linesUsed
			if err := lb.writeFileRow(row, idx, idx == lb.selectedIndex, h); err != nil {
				return err
			}
			linesUsed += h
		}
	}

	// Clear unused rows.
	for linesUsed < lb.visibleRows {
		r := lb.fileAreaStartRow + linesUsed
		if err := terminalio.WriteProcessedBytes(lb.terminal, []byte(ansi.MoveCursor(r, 1)+"\x1b[2K"), lb.outputMode); err != nil {
			return err
		}
		linesUsed++
	}
	return nil
}

func (lb *fileLightbar) renderSeparator() error {
	dashes := lb.termWidth - 2
	if dashes < 0 {
		dashes = 0
	}
	sep := "\xfa" + strings.Repeat("\xc4", dashes) + "\xfa"
	sepLine := ansi.MoveCursor(lb.separatorRow, 1) + "\x1b[2K" + string(ansi.ReplacePipeCodes([]byte("|08"+sep+"|07")))
	return terminalio.WriteProcessedBytes(lb.terminal, []byte(sepLine), lb.outputMode)
}

func (lb *fileLightbar) renderCmdBar() error {
	if err := terminalio.WriteProcessedBytes(lb.terminal, []byte(ansi.MoveCursor(lb.cmdBarRow, 1)+"\x1b[2K"), lb.outputMode); err != nil {
		return err
	}
	barWidth := 0
	for ci, ent := range lb.cmdEntries {
		barWidth += len(ent.label) + 2
		if ci < len(lb.cmdEntries)-1 {
			barWidth += 2
		}
	}
	pad := (lb.termWidth - barWidth) / 2
	if pad < 0 {
		pad = 0
	}
	cmdBar := strings.Repeat(" ", pad)
	for ci, ent := range lb.cmdEntries {
		if ci == lb.cmdIndex {
			cmdBar += ent.highlightColor + " " + ent.label + " " + "\x1b[0m"
		} else {
			cmdBar += ent.regularColor + " " + ent.label + " " + "\x1b[0m"
		}
		if ci < len(lb.cmdEntries)-1 {
			cmdBar += "  "
		}
	}
	return terminalio.WriteProcessedBytes(lb.terminal, []byte(cmdBar), lb.outputMode)
}

func (lb *fileLightbar) renderPageIndicator() error {
	currentPage, calcTotalPages := lb.calculatePageInfo()

	if len(lb.botContent) > 0 {
		// Replace ^PAGE/^TOTALPAGES (legacy) and |FPAGE/|FTOTAL/@FPAGE@/@FTOTAL@ (new).
		pageStr := string(processFileListPlaceholders([]byte(lb.botContent), currentPage, calcTotalPages, len(lb.allFiles), lb.fconfpath))
		pageStr = strings.ReplaceAll(pageStr, "^PAGE", fmt.Sprintf("%d", currentPage))
		pageStr = strings.ReplaceAll(pageStr, "^TOTALPAGES", fmt.Sprintf("%d", calcTotalPages))
		processedPage := string(ansi.ReplacePipeCodes([]byte(pageStr)))
		botLineSlice := strings.Split(processedPage, "\n")
		for i, botLine := range botLineSlice {
			botLine = strings.TrimRight(botLine, "\r")
			plainLen := len(lb.stripAnsi(botLine))
			linePad := (lb.termWidth - plainLen) / 2
			if linePad < 0 {
				linePad = 0
			}
			row := lb.termHeight - len(botLineSlice) + 1 + i
			if row < 1 {
				row = 1
			}
			centered := ansi.MoveCursor(row, 1) + "\x1b[2K" + strings.Repeat(" ", linePad) + botLine
			if err := terminalio.WriteProcessedBytes(lb.terminal, []byte(centered), lb.outputMode); err != nil {
				return err
			}
		}
	}
	return nil
}

func (lb *fileLightbar) renderTop() error {
	if err := terminalio.WriteProcessedBytes(lb.terminal, []byte(ansi.MoveCursor(1, 1)), lb.outputMode); err != nil {
		return err
	}
	curPage, calcTotalPages := lb.calculatePageInfo()
	processed := ansi.ReplacePipeCodes(processFileListPlaceholders(lb.topTemplateBytes, curPage, calcTotalPages, len(lb.allFiles), lb.fconfpath))
	return terminalio.WriteProcessedBytes(lb.terminal, processed, lb.outputMode)
}

func (lb *fileLightbar) renderFull() error {
	if err := terminalio.WriteProcessedBytes(lb.terminal, []byte(ansi.ClearScreen()), lb.outputMode); err != nil {
		return err
	}
	if err := lb.renderTop(); err != nil {
		return err
	}
	if err := lb.renderFileArea(); err != nil {
		return err
	}
	if err := lb.renderSeparator(); err != nil {
		return err
	}
	if err := lb.renderCmdBar(); err != nil {
		return err
	}
	return lb.renderPageIndicator()
}

func (lb *fileLightbar) run() (*user.User, string, error) {
	// Track previous state for smart refresh.
	prevSelectedIndex := -1
	prevTopIndex := -1
	prevCmdIndex := -1
	prevPage := -1
	needFullRedraw := true

	for {
		lb.clampSelection()

		if needFullRedraw {
			if err := lb.renderFull(); err != nil {
				return nil, "", err
			}
			needFullRedraw = false
		} else if lb.topIndex != prevTopIndex {
			// Viewport scrolled — full redraw of all regions to prevent overlap.
			if err := lb.renderTop(); err != nil {
				return nil, "", err
			}
			if err := lb.renderFileArea(); err != nil {
				return nil, "", err
			}
			if err := lb.renderSeparator(); err != nil {
				return nil, "", err
			}
			if err := lb.renderCmdBar(); err != nil {
				return nil, "", err
			}
			if err := lb.renderPageIndicator(); err != nil {
				return nil, "", err
			}
		} else if lb.selectedIndex != prevSelectedIndex {
			// Same viewport, selection changed — redraw old/new rows; redraw TOP if page changed.
			curPage, _ := lb.calculatePageInfo()
			if curPage != prevPage {
				if err := lb.renderTop(); err != nil {
					return nil, "", err
				}
			}
			if prevSelectedIndex >= 0 && prevSelectedIndex < len(lb.allFiles) {
				if row, h := lb.screenRowForFile(prevSelectedIndex); row >= 0 {
					if err := lb.writeFileRow(row, prevSelectedIndex, false, h); err != nil {
						return nil, "", err
					}
				}
			}
			if row, h := lb.screenRowForFile(lb.selectedIndex); row >= 0 {
				if err := lb.writeFileRow(row, lb.selectedIndex, true, h); err != nil {
					return nil, "", err
				}
			}
		}
		if lb.cmdIndex != prevCmdIndex {
			if err := lb.renderCmdBar(); err != nil {
				return nil, "", err
			}
		}

		prevSelectedIndex = lb.selectedIndex
		prevTopIndex = lb.topIndex
		prevCmdIndex = lb.cmdIndex
		prevPage, _ = lb.calculatePageInfo()

		keyInt, err := lb.ih.ReadKey()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil, "LOGOFF", io.EOF
			}
			if errors.Is(err, editor.ErrIdleTimeout) {
				return nil, "LOGOFF", io.EOF
			}
			return nil, "", err
		}

		// Navigation keys — matched directly on integer key codes so that
		// multi-byte escape sequences (PageUp/PageDown etc.) are handled
		// atomically by the InputHandler and can never be split by the
		// 100 ms inter-byte ESC timeout, which previously caused bare ESC
		// to be returned and the lightbar to exit unexpectedly.
		var key string
		switch keyInt {
		case editor.KeyArrowUp: // Up
			lb.selectedIndex--
			continue
		case editor.KeyArrowDown: // Down
			lb.selectedIndex++
			continue
		case editor.KeyArrowRight: // Right — command bar
			lb.cmdIndex++
			if lb.cmdIndex >= len(lb.cmdEntries) {
				lb.cmdIndex = 0
			}
			continue
		case editor.KeyArrowLeft: // Left — command bar
			lb.cmdIndex--
			if lb.cmdIndex < 0 {
				lb.cmdIndex = len(lb.cmdEntries) - 1
			}
			continue
		case editor.KeyPageUp, editor.KeyCtrlR: // Page Up
			newTop := lb.topIndexForPrevPage()
			lb.topIndex = newTop
			lb.selectedIndex = newTop
			continue
		case editor.KeyPageDown: // Page Down
			count := lb.filesVisibleFrom(lb.topIndex)
			nextTop := lb.topIndex + count
			if nextTop >= len(lb.allFiles) {
				if len(lb.allFiles) > 0 {
					lb.selectedIndex = len(lb.allFiles) - 1
				}
			} else {
				lb.topIndex = nextTop
				lb.selectedIndex = nextTop
			}
			continue
		case editor.KeyHome: // Home
			lb.selectedIndex = 0
			continue
		case editor.KeyEnd: // End
			if len(lb.allFiles) > 0 {
				lb.selectedIndex = len(lb.allFiles) - 1
			}
			continue
		case editor.KeyEsc: // Bare Esc
			return nil, "", nil
		case editor.KeyEnter: // Enter: execute selected command bar item
			key = lb.cmdEntries[lb.cmdIndex].hotkey
		default:
			if keyInt >= 32 && keyInt < 127 {
				key = strings.ToLower(string(rune(keyInt)))
			} else {
				continue // Ignore non-printable, non-navigation keys
			}
		}

		// Toggle sysop command bar with *
		if key == "*" && lb.isSysop {
			lb.showSysopBar = !lb.showSysopBar
			if lb.showSysopBar {
				lb.cmdEntries = make([]cmdEntry, len(lb.sysopEntries))
				copy(lb.cmdEntries, lb.sysopEntries)
			} else {
				lb.cmdEntries = make([]cmdEntry, len(lb.userEntries))
				copy(lb.cmdEntries, lb.userEntries)
			}
			lb.cmdIndex = 0
			needFullRedraw = true
			continue
		}

		// Command dispatch (direct hotkeys or Enter-selected command).
		switch key {
		case " ": // Space: toggle mark
			if len(lb.allFiles) > 0 {
				fileID := lb.allFiles[lb.selectedIndex].ID
				found := false
				newTaggedIDs := make([]uuid.UUID, 0, len(lb.currentUser.TaggedFileIDs))
				for _, taggedID := range lb.currentUser.TaggedFileIDs {
					if taggedID == fileID {
						found = true
					} else {
						newTaggedIDs = append(newTaggedIDs, taggedID)
					}
				}
				if !found {
					newTaggedIDs = append(newTaggedIDs, fileID)
				}
				lb.currentUser.TaggedFileIDs = newTaggedIDs
				if err := lb.userManager.UpdateUser(lb.currentUser); err != nil {
					log.Printf("ERROR: Node %d: Failed to save user after tag toggle: %v", lb.nodeNumber, err)
				}
				// Redraw just the toggled row to show/hide the mark.
				if row, h := lb.screenRowForFile(lb.selectedIndex); row >= 0 {
					_ = lb.writeFileRow(row, lb.selectedIndex, true, h)
				}
			}

		case "q":
			return nil, "", nil

		case "i": // Info: show file detail overlay
			if len(lb.allFiles) > 0 {
				sel := lb.allFiles[lb.selectedIndex]
				descLines := formatDIZLines(sel.Description, dizMaxWidth, dizMaxLines)

				_ = terminalio.WriteProcessedBytes(lb.terminal, []byte(ansi.ClearScreen()), lb.outputMode)

				d1 := fmt.Sprintf("|15Filename  : |07%s\r\n", sel.Filename)
				d2 := fmt.Sprintf("|15Size      : |07%s\r\n", compactFileSize(sel.Size))
				_ = terminalio.WriteProcessedBytes(lb.terminal, ansi.ReplacePipeCodes([]byte(d1)), lb.outputMode)
				_ = terminalio.WriteProcessedBytes(lb.terminal, ansi.ReplacePipeCodes([]byte(d2)), lb.outputMode)

				for i, dl := range descLines {
					var dLine string
					if i == 0 {
						dLine = fmt.Sprintf("|15Desc      : |07%s\r\n", dl)
					} else {
						dLine = fmt.Sprintf("|07            %s\r\n", dl)
					}
					_ = terminalio.WriteProcessedBytes(lb.terminal, ansi.ReplacePipeCodes([]byte(dLine)), lb.outputMode)
				}
				if len(descLines) == 0 {
					_ = terminalio.WriteProcessedBytes(lb.terminal, ansi.ReplacePipeCodes([]byte("|15Desc      : |07(none)\r\n")), lb.outputMode)
				}

				d3 := fmt.Sprintf("|15Uploaded  : |07%s\r\n", sel.UploadedAt.Format("01/02/2006 15:04"))
				d4 := fmt.Sprintf("|15Uploader  : |07%s\r\n", sel.UploadedBy)
				d5 := fmt.Sprintf("|15Downloads : |07%d\r\n", sel.DownloadCount)
				_ = terminalio.WriteProcessedBytes(lb.terminal, ansi.ReplacePipeCodes([]byte(d3)), lb.outputMode)
				_ = terminalio.WriteProcessedBytes(lb.terminal, ansi.ReplacePipeCodes([]byte(d4)), lb.outputMode)
				_ = terminalio.WriteProcessedBytes(lb.terminal, ansi.ReplacePipeCodes([]byte(d5)), lb.outputMode)

				_ = terminalio.WriteProcessedBytes(lb.terminal, ansi.ReplacePipeCodes([]byte("\r\n|08Press any key to return...|07")), lb.outputMode)
				if _, waitErr := lb.ih.ReadKey(); waitErr != nil {
					if errors.Is(waitErr, io.EOF) || errors.Is(waitErr, editor.ErrIdleTimeout) {
						return nil, "LOGOFF", io.EOF
					}
					return nil, "", waitErr
				}
				needFullRedraw = true
			}

		case "v":
			if len(lb.allFiles) > 0 {
				sel := &lb.allFiles[lb.selectedIndex]
				filePath, pathErr := lb.e.FileMgr.GetFilePath(sel.ID)
				if pathErr != nil {
					log.Printf("ERROR: Node %d: Failed to get path for file %s: %v", lb.nodeNumber, sel.ID, pathErr)
					continue
				}
				// Show cursor for the viewer.
				_ = terminalio.WriteProcessedBytes(lb.terminal, []byte("\x1b[?25h"), lb.outputMode)
				if lb.e.FileMgr.IsSupportedArchive(sel.Filename) {
					ctx, cancel := lb.e.transferContext(lb.s.Context())
					ziplab.RunZipLabView(ctx, lb.s, lb.terminal, filePath, sel.Filename, lb.outputMode, sessionReadLine(lb.s, lb.terminal), sessionReadKey(lb.s))
					cancel()
				} else {
					tw, th := getTerminalSize(lb.s)
					viewFileByRecord(lb.e, lb.s, lb.terminal, sel, lb.outputMode, tw, th)
				}
				// Hide cursor again.
				_ = terminalio.WriteProcessedBytes(lb.terminal, []byte("\x1b[?25l"), lb.outputMode)
				needFullRedraw = true
			}

		case "d":
			if len(lb.currentUser.TaggedFileIDs) == 0 {
				msg := "\r\n|07No files marked for download. Use |15Space|07 to mark files.|07\r\n"
				_ = terminalio.WriteProcessedBytes(lb.terminal, ansi.ReplacePipeCodes([]byte(msg)), lb.outputMode)
				time.Sleep(1 * time.Second)
				needFullRedraw = true
				continue
			}

			confirmPrompt := fmt.Sprintf("Download %d marked file(s)?", len(lb.currentUser.TaggedFileIDs))
			// Replace the footer lightbar with the confirm prompt instead of printing over the file list.
			clearFooter := ansi.MoveCursor(lb.separatorRow, 1) + "\x1b[2K" + ansi.MoveCursor(lb.cmdBarRow, 1) + "\x1b[2K"
			_ = terminalio.WriteProcessedBytes(lb.terminal, []byte(clearFooter), lb.outputMode)
			_ = terminalio.WriteProcessedBytes(lb.terminal, []byte(ansi.MoveCursor(lb.cmdBarRow, 1)), lb.outputMode)
			_ = terminalio.WriteProcessedBytes(lb.terminal, []byte("\x1b[?25h"), lb.outputMode)

			tw, th := getTerminalSize(lb.s)
			proceed, promptErr := lb.e.PromptYesNo(lb.s, lb.terminal, confirmPrompt, lb.outputMode, lb.nodeNumber, tw, th, false)
			if promptErr != nil {
				if errors.Is(promptErr, io.EOF) {
					return nil, "LOGOFF", io.EOF
				}
				log.Printf("ERROR: Node %d: Error getting download confirmation: %v", lb.nodeNumber, promptErr)
				_ = terminalio.WriteProcessedBytes(lb.terminal, []byte("\x1b[?25l"), lb.outputMode)
				needFullRedraw = true
				continue
			}

			if !proceed {
				_ = terminalio.WriteProcessedBytes(lb.terminal, []byte("\x1b[?25l"), lb.outputMode)
				needFullRedraw = true
				continue
			}

			log.Printf("INFO: Node %d: User %s starting download of %d files.", lb.nodeNumber, lb.currentUser.Handle, len(lb.currentUser.TaggedFileIDs))
			// Clear the screen before the download process begins.
			_ = terminalio.WriteProcessedBytes(lb.terminal, []byte("\x1b[2J\x1b[H"), lb.outputMode)
			_ = terminalio.WriteProcessedBytes(lb.terminal, ansi.ReplacePipeCodes([]byte("|07Preparing download...\r\n")), lb.outputMode)
			time.Sleep(500 * time.Millisecond)

			successCount := 0
			failCount := 0
			filesToDownload := make([]string, 0, len(lb.currentUser.TaggedFileIDs))
			fileIDsToDownload := make([]uuid.UUID, 0, len(lb.currentUser.TaggedFileIDs))

			for _, fileID := range lb.currentUser.TaggedFileIDs {
				fp, pathErr := lb.e.FileMgr.GetFilePath(fileID)
				if pathErr != nil {
					log.Printf("ERROR: Node %d: Failed to get path for file ID %s: %v", lb.nodeNumber, fileID, pathErr)
					failCount++
					continue
				}
				if _, statErr := os.Stat(fp); os.IsNotExist(statErr) {
					log.Printf("ERROR: Node %d: File path %s for ID %s does not exist.", lb.nodeNumber, fp, fileID)
					failCount++
					continue
				} else if statErr != nil {
					log.Printf("ERROR: Node %d: Error stating file path %s for ID %s: %v", lb.nodeNumber, fp, fileID, statErr)
					failCount++
					continue
				}
				filesToDownload = append(filesToDownload, fp)
				fileIDsToDownload = append(fileIDsToDownload, fileID)
			}

			if len(filesToDownload) > 0 {
				log.Printf("INFO: Node %d: Initiating transfer for %d file(s)", lb.nodeNumber, len(filesToDownload))

				// Use protocol selection (respects connection type - SSH vs Telnet)
				proto, protoOK, protoErr := lb.e.selectTransferProtocol(lb.s, lb.terminal, lb.outputMode)
				if protoErr != nil {
					if errors.Is(protoErr, io.EOF) {
						return nil, "LOGOFF", io.EOF
					}
					log.Printf("ERROR: Node %d: Protocol selection error: %v", lb.nodeNumber, protoErr)
					_ = terminalio.WriteProcessedBytes(lb.terminal, ansi.ReplacePipeCodes([]byte("\r\n|01Error: No transfer protocols configured on this system.|07\r\n")), lb.outputMode)
					failCount += len(filesToDownload)
				} else if !protoOK {
					_ = terminalio.WriteProcessedBytes(lb.terminal, ansi.ReplacePipeCodes([]byte("\r\n|07Download cancelled.|07\r\n")), lb.outputMode)
				} else {
					sentCount, sendFails := lb.e.runTransferSend(lb.s, lb.terminal, proto, filesToDownload, fileIDsToDownload, lb.outputMode, lb.nodeNumber)
					successCount = sentCount
					failCount += sendFails
					lb.ih = getSessionIH(lb.s)
				}
				time.Sleep(1 * time.Second)
			} else {
				log.Printf("WARN: Node %d: No valid file paths found for tagged files.", lb.nodeNumber)
				_ = terminalio.WriteProcessedBytes(lb.terminal, ansi.ReplacePipeCodes([]byte("\r\n|01Could not find any of the marked files on the server.|07\r\n")), lb.outputMode)
				// failCount already equals the number of missing files (every
				// tagged ID incremented it in the collection loop above).
			}

			// Clear tags, update download count, and save.
			lb.currentUser.TaggedFileIDs = nil
			lb.currentUser.NumDownloads += successCount
			if saveErr := lb.userManager.UpdateUser(lb.currentUser); saveErr != nil {
				log.Printf("ERROR: Node %d: Failed to save user data after download: %v", lb.nodeNumber, saveErr)
			}

			statusMsg := fmt.Sprintf("|07Download finished. Success: %d, Failed: %d.|07\r\n", successCount, failCount)
			_ = terminalio.WriteProcessedBytes(lb.terminal, ansi.ReplacePipeCodes([]byte(statusMsg)), lb.outputMode)
			time.Sleep(2 * time.Second)

			// Refresh file list.
			lb.allFiles = lb.e.FileMgr.GetFilesForArea(lb.currentAreaID)
			if lb.selectedIndex >= len(lb.allFiles) && len(lb.allFiles) > 0 {
				lb.selectedIndex = len(lb.allFiles) - 1
			}
			_ = terminalio.WriteProcessedBytes(lb.terminal, []byte("\x1b[?25l"), lb.outputMode)
			needFullRedraw = true

		case "u":
			_ = terminalio.WriteProcessedBytes(lb.terminal, []byte("\x1b[?25h"), lb.outputMode)
			uploadErr := lb.e.runUploadFiles(lb.s, lb.terminal, lb.currentUser, lb.userManager, lb.currentAreaID, lb.currentAreaTag, lb.outputMode, lb.nodeNumber, lb.sessionStartTime)
			if uploadErr != nil {
				log.Printf("ERROR: Node %d: Upload error: %v", lb.nodeNumber, uploadErr)
			}
			// runUploadFiles calls resetSessionIH/getSessionIH internally,
			// so the local ih is now stale — refresh it.
			lb.ih = getSessionIH(lb.s)
			// Refresh file list after upload.
			lb.allFiles = lb.e.FileMgr.GetFilesForArea(lb.currentAreaID)
			if lb.selectedIndex >= len(lb.allFiles) && len(lb.allFiles) > 0 {
				lb.selectedIndex = len(lb.allFiles) - 1
			}
			_ = terminalio.WriteProcessedBytes(lb.terminal, []byte("\x1b[?25l"), lb.outputMode)
			needFullRedraw = true

		case "e": // Edit description (sysop only)
			if !lb.isSysop || len(lb.allFiles) == 0 {
				continue
			}
			rec := lb.allFiles[lb.selectedIndex]
			clearFooter := ansi.MoveCursor(lb.separatorRow, 1) + "\x1b[2K" + ansi.MoveCursor(lb.cmdBarRow, 1) + "\x1b[2K"
			_ = terminalio.WriteProcessedBytes(lb.terminal, []byte(clearFooter), lb.outputMode)
			_ = terminalio.WriteProcessedBytes(lb.terminal, []byte(ansi.MoveCursor(lb.cmdBarRow, 1)), lb.outputMode)
			_ = terminalio.WriteProcessedBytes(lb.terminal, []byte("\x1b[?25h"), lb.outputMode)
			_ = terminalio.WriteProcessedBytes(lb.terminal, ansi.ReplacePipeCodes([]byte("|15New description: |07")), lb.outputMode)
			newDesc, readErr := readLineFromSessionIHAllowAbort(lb.s, lb.terminal)
			_ = terminalio.WriteProcessedBytes(lb.terminal, []byte("\x1b[?25l"), lb.outputMode)
			if readErr == nil && newDesc != "" {
				if updErr := lb.e.FileMgr.UpdateFileDescription(rec.ID, newDesc); updErr != nil {
					log.Printf("ERROR: Node %d: Failed to update description for %s: %v", lb.nodeNumber, rec.Filename, updErr)
				} else {
					lb.allFiles = lb.e.FileMgr.GetFilesForArea(lb.currentAreaID)
				}
			}
			needFullRedraw = true

		case "k": // Kill/delete file (sysop only)
			if !lb.isSysop || len(lb.allFiles) == 0 {
				continue
			}
			rec := lb.allFiles[lb.selectedIndex]
			confirmPrompt := fmt.Sprintf("Delete %s from disk?", rec.Filename)
			clearFooter := ansi.MoveCursor(lb.separatorRow, 1) + "\x1b[2K" + ansi.MoveCursor(lb.cmdBarRow, 1) + "\x1b[2K"
			_ = terminalio.WriteProcessedBytes(lb.terminal, []byte(clearFooter), lb.outputMode)
			_ = terminalio.WriteProcessedBytes(lb.terminal, []byte(ansi.MoveCursor(lb.cmdBarRow, 1)), lb.outputMode)
			_ = terminalio.WriteProcessedBytes(lb.terminal, []byte("\x1b[?25h"), lb.outputMode)
			tw, th := getTerminalSize(lb.s)
			proceed, promptErr := lb.e.PromptYesNo(lb.s, lb.terminal, confirmPrompt, lb.outputMode, lb.nodeNumber, tw, th, false)
			_ = terminalio.WriteProcessedBytes(lb.terminal, []byte("\x1b[?25l"), lb.outputMode)
			if promptErr != nil {
				if errors.Is(promptErr, io.EOF) {
					return nil, "LOGOFF", io.EOF
				}
				needFullRedraw = true
				continue
			}
			if proceed {
				if delErr := lb.e.FileMgr.DeleteFileRecord(rec.ID, true); delErr != nil {
					log.Printf("ERROR: Node %d: Failed to delete file %s: %v", lb.nodeNumber, rec.Filename, delErr)
				} else {
					log.Printf("INFO: Node %d: Sysop deleted file '%s' from area %d.", lb.nodeNumber, rec.Filename, lb.currentAreaID)
					// Remove from user's tag list so stale IDs don't reach batch download.
					filtered := lb.currentUser.TaggedFileIDs[:0]
					for _, tid := range lb.currentUser.TaggedFileIDs {
						if tid != rec.ID {
							filtered = append(filtered, tid)
						}
					}
					lb.currentUser.TaggedFileIDs = filtered
					lb.allFiles = lb.e.FileMgr.GetFilesForArea(lb.currentAreaID)
					if lb.selectedIndex >= len(lb.allFiles) && len(lb.allFiles) > 0 {
						lb.selectedIndex = len(lb.allFiles) - 1
					}
				}
			}
			needFullRedraw = true

		case "m": // Move file to another area (sysop only)
			if !lb.isSysop || len(lb.allFiles) == 0 {
				continue
			}
			rec := lb.allFiles[lb.selectedIndex]
			clearFooter := ansi.MoveCursor(lb.separatorRow, 1) + "\x1b[2K" + ansi.MoveCursor(lb.cmdBarRow, 1) + "\x1b[2K"
			_ = terminalio.WriteProcessedBytes(lb.terminal, []byte(clearFooter), lb.outputMode)
			_ = terminalio.WriteProcessedBytes(lb.terminal, []byte(ansi.MoveCursor(lb.cmdBarRow, 1)), lb.outputMode)
			_ = terminalio.WriteProcessedBytes(lb.terminal, []byte("\x1b[?25h"), lb.outputMode)
			_ = terminalio.WriteProcessedBytes(lb.terminal, ansi.ReplacePipeCodes([]byte("|15Move to area (# or tag): |07")), lb.outputMode)
			areaInput, readErr := readLineFromSessionIHAllowAbort(lb.s, lb.terminal)
			_ = terminalio.WriteProcessedBytes(lb.terminal, []byte("\x1b[?25l"), lb.outputMode)
			if readErr != nil || strings.TrimSpace(areaInput) == "" {
				needFullRedraw = true
				continue
			}
			// Resolve area by ID or tag.
			var targetAreaID int
			var targetArea *file.FileArea
			if n, parseErr := fmt.Sscanf(strings.TrimSpace(areaInput), "%d", &targetAreaID); n == 1 && parseErr == nil {
				if a, ok := lb.e.FileMgr.GetAreaByID(targetAreaID); ok {
					targetArea = a
				}
			} else {
				if a, ok := lb.e.FileMgr.GetAreaByTag(strings.TrimSpace(areaInput)); ok {
					targetArea = a
					targetAreaID = a.ID
				}
			}
			if targetArea == nil {
				_ = terminalio.WriteProcessedBytes(lb.terminal, ansi.ReplacePipeCodes([]byte("\r\n|01Area not found.|07\r\n")), lb.outputMode)
				time.Sleep(1 * time.Second)
				needFullRedraw = true
				continue
			}
			confirmPrompt := fmt.Sprintf("Move %s to %s?", rec.Filename, targetArea.Name)
			_ = terminalio.WriteProcessedBytes(lb.terminal, []byte("\x1b[?25h"), lb.outputMode)
			tw, th := getTerminalSize(lb.s)
			proceed, promptErr := lb.e.PromptYesNo(lb.s, lb.terminal, confirmPrompt, lb.outputMode, lb.nodeNumber, tw, th, false)
			_ = terminalio.WriteProcessedBytes(lb.terminal, []byte("\x1b[?25l"), lb.outputMode)
			if promptErr != nil {
				if errors.Is(promptErr, io.EOF) {
					return nil, "LOGOFF", io.EOF
				}
				needFullRedraw = true
				continue
			}
			if proceed {
				if mvErr := lb.e.FileMgr.MoveFileRecord(rec.ID, targetAreaID); mvErr != nil {
					log.Printf("ERROR: Node %d: Failed to move file %s to area %d: %v", lb.nodeNumber, rec.Filename, targetAreaID, mvErr)
				} else {
					log.Printf("INFO: Node %d: Sysop moved file '%s' to area %d (%s).", lb.nodeNumber, rec.Filename, targetAreaID, targetArea.Tag)
					lb.allFiles = lb.e.FileMgr.GetFilesForArea(lb.currentAreaID)
					if lb.selectedIndex >= len(lb.allFiles) && len(lb.allFiles) > 0 {
						lb.selectedIndex = len(lb.allFiles) - 1
					}
				}
			}
			needFullRedraw = true

		case "r": // Rename file on disk (sysop only)
			if !lb.isSysop || len(lb.allFiles) == 0 {
				continue
			}
			rec := lb.allFiles[lb.selectedIndex]
			clearFooter := ansi.MoveCursor(lb.separatorRow, 1) + "\x1b[2K" + ansi.MoveCursor(lb.cmdBarRow, 1) + "\x1b[2K"
			_ = terminalio.WriteProcessedBytes(lb.terminal, []byte(clearFooter), lb.outputMode)
			_ = terminalio.WriteProcessedBytes(lb.terminal, []byte(ansi.MoveCursor(lb.cmdBarRow, 1)), lb.outputMode)
			_ = terminalio.WriteProcessedBytes(lb.terminal, []byte("\x1b[?25h"), lb.outputMode)
			_ = terminalio.WriteProcessedBytes(lb.terminal, ansi.ReplacePipeCodes([]byte("|15New filename: |07")), lb.outputMode)
			newName, readErr := readLineFromSessionIHAllowAbort(lb.s, lb.terminal)
			_ = terminalio.WriteProcessedBytes(lb.terminal, []byte("\x1b[?25l"), lb.outputMode)
			if readErr != nil || strings.TrimSpace(newName) == "" {
				needFullRedraw = true
				continue
			}
			newName = filepath.Base(strings.TrimSpace(newName))
			if newName == "." || newName == ".." {
				_ = terminalio.WriteProcessedBytes(lb.terminal, ansi.ReplacePipeCodes([]byte("\r\n|01Invalid filename.|07\r\n")), lb.outputMode)
				time.Sleep(1 * time.Second)
				needFullRedraw = true
				continue
			}
			// Check for duplicate filename in the current area.
			duplicate := false
			for _, f := range lb.allFiles {
				if strings.EqualFold(f.Filename, newName) && f.ID != rec.ID {
					duplicate = true
					break
				}
			}
			if duplicate {
				_ = terminalio.WriteProcessedBytes(lb.terminal, ansi.ReplacePipeCodes([]byte("\r\n|01Filename already exists in this area.|07\r\n")), lb.outputMode)
				time.Sleep(1 * time.Second)
				needFullRedraw = true
				continue
			}
			oldPath, pathErr := lb.e.FileMgr.GetFilePath(rec.ID)
			if pathErr != nil {
				log.Printf("ERROR: Node %d: Failed to resolve path for %s: %v", lb.nodeNumber, rec.Filename, pathErr)
				needFullRedraw = true
				continue
			}
			newPath := filepath.Join(filepath.Dir(oldPath), newName)
			// Guard against clobbering an untracked file already on disk at the
			// target path (the duplicate check above only consults DB records).
			// os.SameFile permits a case-only rename of the same file on a
			// case-insensitive filesystem.
			if newInfo, statErr := os.Stat(newPath); statErr == nil {
				oldInfo, oldStatErr := os.Stat(oldPath)
				if oldStatErr != nil || !os.SameFile(oldInfo, newInfo) {
					log.Printf("ERROR: Node %d: Rename target %s already exists on disk.", lb.nodeNumber, newPath)
					_ = terminalio.WriteProcessedBytes(lb.terminal, ansi.ReplacePipeCodes([]byte("\r\n|01A file with that name already exists.|07\r\n")), lb.outputMode)
					time.Sleep(1 * time.Second)
					needFullRedraw = true
					continue
				}
			}
			if renErr := os.Rename(oldPath, newPath); renErr != nil {
				log.Printf("ERROR: Node %d: Failed to rename %s to %s: %v", lb.nodeNumber, rec.Filename, newName, renErr)
				_ = terminalio.WriteProcessedBytes(lb.terminal, ansi.ReplacePipeCodes([]byte("\r\n|01Rename failed.|07\r\n")), lb.outputMode)
				time.Sleep(1 * time.Second)
				needFullRedraw = true
				continue
			}
			if updErr := lb.e.FileMgr.UpdateFileRecord(rec.ID, func(r *file.FileRecord) {
				r.Filename = newName
			}); updErr != nil {
				log.Printf("ERROR: Node %d: Failed to update record for %s: %v", lb.nodeNumber, newName, updErr)
				if rollbackErr := os.Rename(newPath, oldPath); rollbackErr != nil {
					log.Printf("ERROR: Node %d: Rollback rename failed for %s: %v (disk/DB inconsistent)", lb.nodeNumber, newName, rollbackErr)
				}
			} else {
				log.Printf("INFO: Node %d: Sysop renamed file '%s' to '%s' in area %d.", lb.nodeNumber, rec.Filename, newName, lb.currentAreaID)
				lb.allFiles = lb.e.FileMgr.GetFilesForArea(lb.currentAreaID)
			}
			needFullRedraw = true
		}
	}
}

const (
	dizMaxWidth = 45
	dizMaxLines = 22
)

// formatDIZLines splits FILE_ID.DIZ content into display-ready lines.
// Each line is truncated to maxWidth visible characters (ANSI-aware).
// Returns at most maxLines lines, with trailing blank lines trimmed.
func formatDIZLines(content string, maxWidth, maxLines int) []string {
	if content == "" {
		return nil
	}

	content = strings.ReplaceAll(content, "\r\n", "\n")
	content = strings.ReplaceAll(content, "\r", "\n")

	rawLines := strings.Split(content, "\n")

	var lines []string
	for _, line := range rawLines {
		if len(lines) >= maxLines {
			break
		}
		line = strings.TrimRight(line, " \t")
		if ansi.VisibleLength(line) > maxWidth {
			line = ansi.TruncateVisible(line, maxWidth)
		}
		lines = append(lines, line)
	}

	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}

	return lines
}
