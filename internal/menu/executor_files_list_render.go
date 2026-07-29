package menu

import (
	"bytes"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"golang.org/x/term"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// loadFileListTemplates loads and processes the FILELIST.TOP/MID/BOT templates
// for the classic and lightbar file listing UIs. top and bot are returned as
// raw (common-token-substituted, but not yet ANSI/pipe-code processed) bytes
// since callers re-process them per page with processFileListPlaceholders.
// mid is returned fully processed since it needs no further per-page work.
func (e *MenuExecutor) loadFileListTemplates(currentUser *user.User, nodeNumber int, terminal *term.Terminal, outputMode ansi.OutputMode) (top []byte, mid string, bot []byte, err error) {
	topTemplatePath := filepath.Join(e.MenuSetPath, "templates", "FILELIST.TOP")
	midTemplatePath := filepath.Join(e.MenuSetPath, "templates", "FILELIST.MID")
	botTemplatePath := filepath.Join(e.MenuSetPath, "templates", "FILELIST.BOT")

	topTemplateBytes, errTop := readTemplateFile(topTemplatePath)
	midTemplateBytes, errMid := readTemplateFile(midTemplatePath)
	botTemplateBytes, errBot := readTemplateFile(botTemplatePath)
	if errBot != nil {
		if os.IsNotExist(errBot) {
			botTemplateBytes = nil
		} else {
			slog.Error("failed to load FILELIST.BOT template", "node", nodeNumber, "error", errBot)
			msg := "\r\n|01Error loading File List screen templates.|07\r\n"
			terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
			time.Sleep(1 * time.Second)
			return nil, "", nil, fmt.Errorf("failed loading FILELIST templates")
		}
	}

	if errTop != nil || errMid != nil {
		slog.Error("failed to load FILELIST template files", "node", nodeNumber, "topError", errTop, "midError", errMid)
		msg := "\r\n|01Error loading File List screen templates.|07\r\n"
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
		time.Sleep(1 * time.Second)
		return nil, "", nil, fmt.Errorf("failed loading FILELIST templates")
	}

	// Apply common pipe tokens (|CFAN, |UH, etc.) before colour-code processing.
	topTemplateBytes = e.applyCommonTemplateTokens(topTemplateBytes, currentUser, nodeNumber)
	midTemplateBytes = e.applyCommonTemplateTokens(midTemplateBytes, currentUser, nodeNumber)
	botTemplateBytes = e.applyCommonTemplateTokens(botTemplateBytes, currentUser, nodeNumber)

	processedMidTemplate := string(ansi.ReplacePipeCodes(midTemplateBytes))

	return topTemplateBytes, processedMidTemplate, botTemplateBytes, nil
}

// computeFilePagination returns the number of files that fit on one page of
// the file listing, given the raw (pre-ANSI) top/bottom templates and the
// terminal geometry. termWidth is accepted for symmetry with the caller's
// terminal-geometry pair but is not used by the line-based formula.
func computeFilePagination(termWidth, termHeight int, top []byte, bot []byte) int {
	processedTopTemplate := ansi.ReplacePipeCodes(top)
	processedBotTemplate := ansi.ReplacePipeCodes(bot)

	// Estimate lines used by header, footer, prompt
	headerLines := bytes.Count(processedTopTemplate, []byte("\n")) + 1
	footerLines := bytes.Count(processedBotTemplate, []byte("\n")) + 1
	// TODO: Make prompt configurable and count its lines accurately
	promptLines := 2 // Estimate 2 lines for prompt + input line
	fixedLines := headerLines + footerLines + promptLines
	filesPerPage := termHeight - fixedLines
	if filesPerPage < 1 {
		filesPerPage = 1 // Ensure at least 1 file can be shown
	}
	return filesPerPage
}

// renderFileListPage draws one page of the classic file listing: clears the
// screen, writes the top template, the file rows, the bottom template, and
// the command prompt. Write failures are logged but do not abort the draw
// (matching the original inline loop body); the returned error is reserved
// for future use and is always nil today.
func (st *fileListState) renderFileListPage() error {
	// 4.1 Clear Screen
	writeErr := terminalio.WriteProcessedBytes(st.terminal, []byte(ansi.ClearScreen()), st.outputMode)
	if writeErr != nil {
		slog.Error("failed clearing screen for LISTFILES", "node", st.nodeNumber, "error", writeErr)
	}

	// 4.2 Display Top Template (process @FCONFPATH@, @FTOTAL@, @FPAGE@ placeholders per page)
	topRendered := ansi.ReplacePipeCodes(processFileListPlaceholders(st.topTemplateBytes, st.currentPage, st.totalPages, st.totalFiles, st.fconfpath))
	wErr := terminalio.WriteProcessedBytes(st.terminal, topRendered, st.outputMode)
	if wErr != nil {
		slog.Error("failed writing LISTFILES top template", "node", st.nodeNumber, "error", wErr)
	}
	wErr = terminalio.WriteProcessedBytes(st.terminal, []byte("\r\n"), st.outputMode)
	if wErr != nil {
		slog.Error("failed writing CRLF after LISTFILES top template", "node", st.nodeNumber, "error", wErr)
	}

	// 4.3 Display Files on Current Page (using MID template)
	if len(st.filesOnPage) == 0 {
		// Display "No files in this area" message
		// TODO: Use a configurable string?
		noFilesMsg := "\r\n|07   No files in this area.   \r\n"
		terminalio.WriteProcessedBytes(st.terminal, ansi.ReplacePipeCodes([]byte(noFilesMsg)), st.outputMode)
	} else {
		for i, fileRec := range st.filesOnPage {
			line := st.processedMidTemplate
			fileNumOnPage := (st.currentPage-1)*st.filesPerPage + i + 1

			fileNumStr := strconv.Itoa(fileNumOnPage)
			fileNameStr := ""
			if fileColumnEnabled(st.currentUser, "name", st.extendedMode) {
				fileNameStr = fileRec.Filename
				if len(fileNameStr) > 12 {
					fileNameStr = fileNameStr[:12]
				}
				fileNameStr = fmt.Sprintf("%-12s", fileNameStr)
			} else {
				fileNameStr = strings.Repeat(" ", 12)
			}
			dateStr := ""
			if fileColumnEnabled(st.currentUser, "date", st.extendedMode) {
				dateStr = fileRec.UploadedAt.Format("01/02/06")
			} else {
				dateStr = strings.Repeat(" ", 8)
			}
			sizeStr := ""
			if fileColumnEnabled(st.currentUser, "size", st.extendedMode) {
				sizeStr = fmt.Sprintf("%5s", fmt.Sprintf("%dk", fileRec.Size/1024))
			} else {
				sizeStr = strings.Repeat(" ", 5)
			}

			markStr := " "
			if st.currentUser.TaggedFileIDs != nil {
				for _, taggedID := range st.currentUser.TaggedFileIDs {
					if taggedID == fileRec.ID {
						markStr = "*"
						break
					}
				}
			}

			var dizLines []string
			firstDesc := ""
			if fileColumnEnabled(st.currentUser, "description", st.extendedMode) {
				dizLines = formatDIZLines(fileRec.Description, dizMaxWidth, dizMaxLines)
				if len(dizLines) > 0 {
					firstDesc = dizLines[0]
				}
			}

			line = strings.ReplaceAll(line, "^MARK", markStr)
			line = strings.ReplaceAll(line, "^NUM", fileNumStr)
			line = strings.ReplaceAll(line, "^NAME", fileNameStr)
			line = strings.ReplaceAll(line, "^DATE", dateStr)
			line = strings.ReplaceAll(line, "^SIZE", sizeStr)
			line = strings.ReplaceAll(line, "^DESC", firstDesc)

			wErr = writeProcessedStringWithManualEncoding(st.terminal, []byte(line), st.outputMode)
			if wErr != nil {
				slog.Error("failed writing file list line", "node", st.nodeNumber, "line", i, "error", wErr)
			}
			wErr = terminalio.WriteProcessedBytes(st.terminal, []byte("\r\n"), st.outputMode)
			if wErr != nil {
				slog.Error("failed writing CRLF after file list line", "node", st.nodeNumber, "line", i, "error", wErr)
			}

			prefixLine := st.processedMidTemplate
			prefixLine = strings.ReplaceAll(prefixLine, "^MARK", " ")
			prefixLine = strings.ReplaceAll(prefixLine, "^NUM", "   ")
			prefixLine = strings.ReplaceAll(prefixLine, "^NAME", strings.Repeat(" ", 12))
			prefixLine = strings.ReplaceAll(prefixLine, "^DATE", strings.Repeat(" ", 8))
			prefixLine = strings.ReplaceAll(prefixLine, "^SIZE", strings.Repeat(" ", 5))
			prefixLine = strings.ReplaceAll(prefixLine, "^DESC", "")
			processedPrefix := string(ansi.ReplacePipeCodes([]byte(prefixLine)))
			prefixLen := ansi.VisibleLength(processedPrefix)
			descIndent := strings.Repeat(" ", prefixLen)
			for j := 1; j < len(dizLines); j++ {
				contLine := "|07" + descIndent + dizLines[j]
				wErr = writeProcessedStringWithManualEncoding(st.terminal, ansi.ReplacePipeCodes([]byte(contLine)), st.outputMode)
				if wErr != nil {
					break
				}
				_ = terminalio.WriteProcessedBytes(st.terminal, []byte("\r\n"), st.outputMode)
			}

		}
	}

	// 4.4 Display Bottom Template (with pagination info)
	botRendered := processFileListPlaceholders(st.botTemplateBytes, st.currentPage, st.totalPages, st.totalFiles, st.fconfpath)
	bottomLine := string(ansi.ReplacePipeCodes(botRendered))
	bottomLine = strings.ReplaceAll(bottomLine, "^PAGE", strconv.Itoa(st.currentPage))
	bottomLine = strings.ReplaceAll(bottomLine, "^TOTALPAGES", strconv.Itoa(st.totalPages))
	wErr = terminalio.WriteProcessedBytes(st.terminal, []byte(bottomLine), st.outputMode)
	if wErr != nil {
		slog.Error("failed writing LISTFILES bottom template", "node", st.nodeNumber, "error", wErr)
		// Handle error
	}

	// 4.5 Display Prompt (Use a standard file list prompt or configure one)
	// TODO: Use configurable prompt string
	prompt := "\r\n|07File Cmd (|15N|07=Next, |15P|07=Prev, |15#|07=Mark, |15V|07=View, |15D|07=Download, |15U|07=Upload, |15Q|07=Quit): |15"
	terminalio.WriteProcessedBytes(st.terminal, ansi.ReplacePipeCodes([]byte(prompt)), st.outputMode)

	return nil
}
