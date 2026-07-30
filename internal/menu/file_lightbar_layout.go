package menu

import (
	"regexp"
	"strings"

	"github.com/gliderlabs/ssh"
	"github.com/google/uuid"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
)

// fileLightbar geometry, paging, and selection helpers.

// resolveTermSize returns the terminal height and width for s, falling back
// to the default 80x24 when no PTY window size is reported.
func resolveTermSize(s ssh.Session) (termHeight, termWidth int) {
	termHeight = 24
	termWidth = 80
	if ptyReq, _, ok := s.Pty(); ok && ptyReq.Window.Height > 0 {
		termHeight = ptyReq.Window.Height
		if ptyReq.Window.Width > 0 {
			termWidth = ptyReq.Window.Width
		}
	}
	return termHeight, termWidth
}

// computeVerticalLayout derives the lightbar's fixed vertical geometry from
// the terminal height and the top/bot templates: how many lines the top
// template's header renders, how many lines the (already placeholder- and
// pipe-code-expanded) BOT template occupies, how many rows are reserved for
// the separator/command bar/BOT, how many rows remain for the file list, and
// the absolute rows where the file area, separator, and command bar begin.
func computeVerticalLayout(termHeight int, topTemplateBytes []byte, processedBotTemplate []byte, totalFiles int, fconfpath string) (headerLines int, botContent string, botLineCount int, reservedBottom int, visibleRows int, fileAreaStartRow int, cmdBarRow int, separatorRow int) {
	// Count header lines from top template (line count is invariant to page/file counts).
	processedTopSample := ansi.ReplacePipeCodes(processFileListPlaceholders(topTemplateBytes, 1, 1, totalFiles, fconfpath))
	headerLines = strings.Count(string(processedTopSample), "\n")
	if headerLines < 1 {
		headerLines = 1
	}

	// Reserve rows for the separator, command bar, and optional BOT template.
	// Derive botLineCount from the expanded string (after placeholder + pipe-code
	// processing) so it matches what renderPageIndicator actually renders.
	botContent = strings.TrimRight(string(processedBotTemplate), "\r\n")
	botLineCount = 0
	if len(botContent) > 0 {
		expandedBotSample := string(ansi.ReplacePipeCodes(processFileListPlaceholders([]byte(botContent), 1, 1, totalFiles, fconfpath)))
		expandedBotSample = strings.ReplaceAll(expandedBotSample, "^PAGE", "1")
		expandedBotSample = strings.ReplaceAll(expandedBotSample, "^TOTALPAGES", "1")
		botLineCount = len(strings.Split(expandedBotSample, "\n"))
	}
	reservedBottom = 2 // separator + command bar
	if botLineCount > 0 {
		reservedBottom = 2 + botLineCount // separator + command bar + BOT lines
	}
	visibleRows = termHeight - headerLines - reservedBottom - 1
	if visibleRows < 3 {
		visibleRows = 3
	}

	// fileAreaStartRow is the absolute terminal row where file entries begin.
	fileAreaStartRow = headerLines + 2

	// Layout: separator row, then command bar, then optional BOT.
	cmdBarRow = max(1, termHeight-botLineCount)
	separatorRow = max(1, cmdBarRow-1)

	return headerLines, botContent, botLineCount, reservedBottom, visibleRows, fileAreaStartRow, cmdBarRow, separatorRow
}

// computeDescMetrics precomputes the fixed-width description-column geometry
// shared by fileEntryHeight and buildFileEntry: every mid-template
// placeholder except ^DESC is fixed width, so the prefix length (and
// therefore how much room is left for the description) is constant across
// all file entries.
func computeDescMetrics(processedMidTemplate string, termWidth int) (ansiRe *regexp.Regexp, descPrefixLen int, descColWidth int, descIndentStr string) {
	// stripAnsi removes all ANSI escape sequences from a string.
	ansiRe = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

	sample := strings.ReplaceAll(processedMidTemplate, "^MARK", " ")
	sample = strings.ReplaceAll(sample, "^NUM", "  1")
	sample = strings.ReplaceAll(sample, "^NAME", "            ")
	sample = strings.ReplaceAll(sample, "^DATE", "01/01/00")
	sample = strings.ReplaceAll(sample, "^SIZE", "     ")
	sample = strings.ReplaceAll(sample, "^DESC", "")
	descPrefixLen = len(ansiRe.ReplaceAllString(string(ansi.ReplacePipeCodes([]byte(sample))), ""))
	descColWidth = termWidth - descPrefixLen - 1
	if descColWidth < 20 {
		descColWidth = 20
	}
	descIndentStr = strings.Repeat(" ", descPrefixLen)
	return ansiRe, descPrefixLen, descColWidth, descIndentStr
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

// refreshFileList refetches allFiles for the current area and clamps
// selectedIndex back onto the list if the file that was selected no longer
// exists (e.g. after a download, upload, kill, or move changed the count).
func (lb *fileLightbar) refreshFileList() {
	lb.allFiles = lb.e.FileMgr.GetFilesForArea(lb.currentAreaID)
	if lb.selectedIndex >= len(lb.allFiles) && len(lb.allFiles) > 0 {
		lb.selectedIndex = len(lb.allFiles) - 1
	}
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
