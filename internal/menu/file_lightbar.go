package menu

import (
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"regexp"
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
			// case-insensitive filesystem. A stat error other than "not exist"
			// (e.g. a permission/IO fault) means we cannot prove the target is
			// safe to overwrite, so refuse rather than risk a clobber.
			newInfo, statErr := os.Stat(newPath)
			switch {
			case statErr == nil:
				oldInfo, oldStatErr := os.Stat(oldPath)
				if oldStatErr != nil || !os.SameFile(oldInfo, newInfo) {
					log.Printf("ERROR: Node %d: Rename target %s already exists on disk.", lb.nodeNumber, newPath)
					_ = terminalio.WriteProcessedBytes(lb.terminal, ansi.ReplacePipeCodes([]byte("\r\n|01A file with that name already exists.|07\r\n")), lb.outputMode)
					time.Sleep(1 * time.Second)
					needFullRedraw = true
					continue
				}
			case !errors.Is(statErr, os.ErrNotExist):
				log.Printf("ERROR: Node %d: Cannot stat rename target %s: %v", lb.nodeNumber, newPath, statErr)
				_ = terminalio.WriteProcessedBytes(lb.terminal, ansi.ReplacePipeCodes([]byte("\r\n|01Rename failed.|07\r\n")), lb.outputMode)
				time.Sleep(1 * time.Second)
				needFullRedraw = true
				continue
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
