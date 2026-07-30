package menu

import (
	"errors"
	"io"
	"regexp"
	"strings"
	"time"

	"github.com/gliderlabs/ssh"
	"golang.org/x/term"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/editor"
	"github.com/ViSiON-3/vision-3-bbs/internal/file"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
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

// logoffIfDisconnected reports whether err indicates the session has gone
// away (EOF) or idled out — the six places in the lightbar loop that read a
// key or wait on a prompt all map either condition to the same "LOGOFF"
// result.
func logoffIfDisconnected(err error) bool {
	return errors.Is(err, io.EOF) || errors.Is(err, editor.ErrIdleTimeout)
}

// runListFilesLightbar builds a fileLightbar from st's already-loaded
// templates, pagination, and area state, then drives the interactive
// lightbar file browser via run(). It always returns a nil *user.User; the
// command is "LOGOFF" with err set (typically io.EOF) when the session
// disconnected or idled out, or ("", nil) on a normal user-initiated quit.
func runListFilesLightbar(st *fileListState) (*user.User, string, error) {

	// Hide cursor on entry, show on exit.
	_ = terminalio.WriteProcessedBytes(st.terminal, []byte("\x1b[?25l"), st.outputMode)
	defer terminalio.WriteProcessedBytes(st.terminal, []byte("\x1b[?25h"), st.outputMode)

	// Fetch all files for the area.
	allFiles := st.e.FileMgr.GetFilesForArea(st.currentAreaID)

	selectedIndex := 0
	topIndex := 0
	cmdIndex := 0
	ih := getSessionIH(st.s)

	// Build command bar entries (user bar, sysop bar) and the file-row highlight.
	cmdEntries, sysopEntries, userEntries, hiColorSeq, isSysop := buildFileListCmdBar(st.e, st.currentUser, st.cmdBarOptions, st.hiBarOptions)
	showSysopBar := false

	// Determine terminal dimensions.
	termHeight := 24
	termWidth := 80
	if ptyReq, _, ok := st.s.Pty(); ok && ptyReq.Window.Height > 0 {
		termHeight = ptyReq.Window.Height
		if ptyReq.Window.Width > 0 {
			termWidth = ptyReq.Window.Width
		}
	}

	// Count header lines from top template (line count is invariant to page/file counts).
	fconfpath := st.e.resolveFileConferencePath(st.currentUser)
	processedTopSample := ansi.ReplacePipeCodes(processFileListPlaceholders(st.topTemplateBytes, 1, 1, st.totalFiles, fconfpath))
	headerLines := strings.Count(string(processedTopSample), "\n")
	if headerLines < 1 {
		headerLines = 1
	}

	// Reserve rows for the separator, command bar, and optional BOT template.
	// Derive botLineCount from the expanded string (after placeholder + pipe-code
	// processing) so it matches what renderPageIndicator actually renders.
	botContent := strings.TrimRight(string(st.processedBotTemplate), "\r\n")
	botLineCount := 0
	if len(botContent) > 0 {
		expandedBotSample := string(ansi.ReplacePipeCodes(processFileListPlaceholders([]byte(botContent), 1, 1, st.totalFiles, fconfpath)))
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
	sample := strings.ReplaceAll(st.processedMidTemplate, "^MARK", " ")
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

	// fileAreaStartRow is the absolute terminal row where file entries begin.
	fileAreaStartRow := headerLines + 2

	// Layout: separator row, then command bar, then optional BOT.
	cmdBarRow := max(1, termHeight-botLineCount)
	separatorRow := max(1, cmdBarRow-1)

	lb := &fileLightbar{
		e:                    st.e,
		s:                    st.s,
		terminal:             st.terminal,
		userManager:          st.userManager,
		currentUser:          st.currentUser,
		nodeNumber:           st.nodeNumber,
		sessionStartTime:     st.sessionStartTime,
		currentAreaID:        st.currentAreaID,
		currentAreaTag:       st.currentAreaTag,
		area:                 st.area,
		outputMode:           st.outputMode,
		ih:                   ih,
		topTemplateBytes:     st.topTemplateBytes,
		processedMidTemplate: st.processedMidTemplate,
		processedBotTemplate: st.processedBotTemplate,
		filesPerPage:         st.filesPerPage,
		totalFiles:           st.totalFiles,
		totalPages:           st.totalPages,
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

// run drives the lightbar's main input/redraw loop: it renders the file
// list, command bar, and page indicator (redrawing only the regions whose
// state changed since the last iteration), dispatches navigation and command
// hotkeys — including mark, view, download, upload, and the sysop-only edit,
// kill, move, and rename commands — and loops until the user quits or the
// session ends. Return values follow the same convention documented on
// runListFilesLightbar, which constructs lb and calls this method.
func (lb *fileLightbar) run() (*user.User, string, error) {
	// Track previous state for smart refresh.
	frame := &lbFrame{prevSelectedIndex: -1, prevTopIndex: -1, prevCmdIndex: -1, prevPage: -1, needFullRedraw: true}

	for {
		lb.clampSelection()

		if err := lb.refreshFrame(frame); err != nil {
			return nil, "", err
		}

		keyInt, exit, action, err := lb.readKeyOrLogoff()
		if exit {
			return nil, action, err
		}

		key, dispatch, exit, result, action, err := lb.handleNavKey(keyInt)
		if exit {
			return result, action, err
		}
		if !dispatch {
			continue
		}

		// Toggle sysop command bar with *
		if key == "*" && lb.isSysop {
			lb.toggleSysopBar()
			frame.needFullRedraw = true
			continue
		}

		exit, result, action, err = lb.dispatchCommand(key, frame)
		if exit {
			return result, action, err
		}
	}
}
