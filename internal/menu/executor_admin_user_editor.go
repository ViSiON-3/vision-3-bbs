package menu

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/editor"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
	"github.com/gliderlabs/ssh"
	"golang.org/x/term"
)

// userEditorConfig parameterizes runUserEditor for its two entry points: the
// full user editor (runAdminListUsers) and the pending-validation queue
// (runValidateUser). Those two flows were previously ~800 lines of near-identical
// duplicated code; pendingOnly captures every behavioral difference between them.
type userEditorConfig struct {
	title        string // header title line (pipe-coded)
	emptyMessage string // shown when no users match (pipe-coded, no trailing reset)
	logLabel     string // optional startup debug label ("" = no log line)
	pendingOnly  bool   // restrict to users awaiting validation + queue behavior
}

// userEditorLayout is the fixed screen geometry of the admin user editor,
// derived once from the (already-resolved) terminal height.
type userEditorLayout struct {
	pageSize       int
	titleRow       int
	sepTopRow      int
	headerRow      int
	listStartRow   int
	sepMidRow      int
	detailStartRow int
	statusRow      int
	actionRow      int
}

// computeUserEditorLayout derives the row layout and page size for the admin
// user editor from a resolved terminal height. Callers are responsible for
// substituting a default before calling this (e.g. when the terminal reports
// height <= 0).
func computeUserEditorLayout(termHeight int) userEditorLayout {
	pageSize := termHeight - 14 // Reduced by 1 to account for header row
	if pageSize < 3 {
		pageSize = 3
	}
	if pageSize > 12 {
		pageSize = 12
	}

	titleRow := 1
	sepTopRow := 2
	headerRow := 3    // Column header labels
	listStartRow := 4 // First user row (after header)
	sepMidRow := listStartRow + pageSize
	detailStartRow := sepMidRow + 1
	statusRow := termHeight - 1
	actionRow := termHeight

	return userEditorLayout{
		pageSize:       pageSize,
		titleRow:       titleRow,
		sepTopRow:      sepTopRow,
		headerRow:      headerRow,
		listStartRow:   listStartRow,
		sepMidRow:      sepMidRow,
		detailStartRow: detailStartRow,
		statusRow:      statusRow,
		actionRow:      actionRow,
	}
}

// visualPipeWidth returns the display width of s, treating |NN pipe color
// codes as zero-width.
func visualPipeWidth(s string) int {
	width := 0
	i := 0
	for i < len(s) {
		if s[i] == '|' && i+2 < len(s) && s[i+1] >= '0' && s[i+1] <= '9' && s[i+2] >= '0' && s[i+2] <= '9' {
			i += 3 // Skip pipe code
		} else {
			width++
			i++
		}
	}
	return width
}

// userEditorState holds the dependencies and mutable state for a single
// runUserEditor session. It replaces the closure-captured locals the loop
// body used to mutate directly; users and pendingChanges in particular are
// reassigned (not just mutated) inside the loop, which requires them to live
// as struct fields rather than closed-over locals.
type userEditorState struct {
	e           *MenuExecutor
	s           ssh.Session
	terminal    *term.Terminal
	ih          *editor.InputHandler
	userManager *user.UserMgr
	currentUser *user.User
	nodeNumber  int
	outputMode  ansi.OutputMode
	cfg         userEditorConfig
	layout      userEditorLayout

	users              []*user.User
	selectedIndex      int
	topIndex           int
	pendingChanges     map[string]interface{}
	originalTimestamps map[int]time.Time
	statusMessage      string
	adminCursorHidden  bool
}

// loadEditorUsers loads the full user list, filtered down to users awaiting
// validation when cfg.pendingOnly is set.
func (st *userEditorState) loadEditorUsers() []*user.User {
	all := sortedUsersByID(st.userManager.GetAllUsers())
	if !st.cfg.pendingOnly {
		return all
	}
	pending := make([]*user.User, 0)
	for _, u := range all {
		if isPendingValidationUser(u) {
			pending = append(pending, u)
		}
	}
	return pending
}

// moveUp moves the selection up one row, scrolling topIndex if needed.
func (st *userEditorState) moveUp() {
	if st.selectedIndex > 0 {
		st.selectedIndex--
		if st.selectedIndex < st.topIndex {
			st.topIndex = st.selectedIndex
		}
	}
}

// moveDown moves the selection down one row, scrolling topIndex if needed.
func (st *userEditorState) moveDown() {
	if st.selectedIndex < len(st.users)-1 {
		st.selectedIndex++
		if st.selectedIndex >= st.topIndex+st.layout.pageSize {
			st.topIndex = st.selectedIndex - st.layout.pageSize + 1
		}
	}
}

// runUserEditor implements the shared interactive user editor used by both the
// admin user browser and the pending-validation queue. See userEditorConfig.
func runUserEditor(c *cmdCtx, cfg userEditorConfig) (*user.User, string, error) {
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

	if cfg.logLabel != "" {
		slog.Debug("running command", "node", nodeNumber, "label", cfg.logLabel)
	}

	adminCursorHidden := e.hideCursorIfNeeded(terminal, outputMode, cursorHideContextDefault)
	if adminCursorHidden {
		defer e.showCursorIfHidden(terminal, outputMode, adminCursorHidden)
	}

	if currentUser == nil || userManager == nil {
		return nil, "", nil
	}
	sysOpACS := fmt.Sprintf("S%d", e.ServerCfg.SysOpLevel)
	if !checkACS(sysOpACS, currentUser, s, terminal, sessionStartTime) {
		_ = terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte("\r\n|01Access denied.|07\r\n")), outputMode)
		time.Sleep(1 * time.Second)
		return nil, "", nil
	}

	st := &userEditorState{
		e:                 e,
		s:                 s,
		terminal:          terminal,
		ih:                getSessionIH(s),
		userManager:       userManager,
		currentUser:       currentUser,
		nodeNumber:        nodeNumber,
		outputMode:        outputMode,
		cfg:               cfg,
		adminCursorHidden: adminCursorHidden,
	}

	st.users = st.loadEditorUsers()
	if len(st.users) == 0 {
		_ = terminalio.WriteProcessedBytes(st.terminal, []byte(ansi.ClearScreen()), st.outputMode)
		_ = terminalio.WriteProcessedBytes(st.terminal, ansi.ReplacePipeCodes([]byte("\r\n"+st.cfg.emptyMessage+"|07")), st.outputMode)
		if pauseErr := e.loginPausePrompt(s, terminal, nodeNumber, outputMode, termWidth, termHeight); pauseErr != nil {
			if errors.Is(pauseErr, io.EOF) {
				return nil, "LOGOFF", io.EOF
			}
			return nil, "", pauseErr
		}
		return nil, "", nil
	}

	if termHeight <= 0 {
		termHeight = 24
		if ptyReq, _, ok := s.Pty(); ok && ptyReq.Window.Height > 0 {
			termHeight = ptyReq.Window.Height
		}
	}
	st.layout = computeUserEditorLayout(termHeight)

	st.pendingChanges = make(map[string]interface{})
	// Track original UpdatedAt timestamps for optimistic locking (indexed by user ID)
	st.originalTimestamps = make(map[int]time.Time)
	for _, u := range st.users {
		if u != nil {
			st.originalTimestamps[u.ID] = u.UpdatedAt
		}
	}

	if err := st.renderHeader(); err != nil {
		return nil, "", err
	}
	if err := st.renderList(); err != nil {
		return nil, "", err
	}
	if err := st.renderActionBar(); err != nil {
		return nil, "", err
	}
	if err := st.renderDetails(""); err != nil {
		return nil, "", err
	}

	for {
		key, err := st.ih.ReadKey()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil, "LOGOFF", io.EOF
			}
			return nil, "", err
		}

		st.statusMessage = ""

		refresh, exit, result, action, keyErr := st.handleEditorKey(key, termWidth, termHeight)
		if exit {
			return result, action, keyErr
		}

		if refresh {
			if err := st.renderList(); err != nil {
				return nil, "", err
			}
			if err := st.renderDetails(st.statusMessage); err != nil {
				return nil, "", err
			}
		} else if st.statusMessage != "" {
			if err := st.renderDetails(st.statusMessage); err != nil {
				return nil, "", err
			}
		}
	}
}
