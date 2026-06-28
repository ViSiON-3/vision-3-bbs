package menu

import (
	"fmt"
	"strings"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
)

// fileLightbar rendering: file rows, separator, command bar, page indicator.
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
