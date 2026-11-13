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
	// validateOnly is the pending queue for a user below sysop level who
	// holds users.validate (#610): approving is the only action, and only
	// what deciding needs is shown. runUserEditor sets it; the save path
	// enforces the same rule on its own (authorizeUserChanges).
	validateOnly bool
	// limitedEdit and limitedBan are the full list for a user below sysop
	// level who holds users.edit or users.ban (#1045): only accounts below
	// co-sysop level, and only the keys those permissions use. As with
	// validateOnly, the save path enforces the rule itself.
	limitedEdit, limitedBan bool
}

// limited reports whether the editor is in users.edit / users.ban's view.
func (c userEditorConfig) limited() bool { return c.limitedEdit || c.limitedBan }

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
	pageSize := termHeight - 15 // header row, and eight detail lines below the list
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

// visualPipeWidth returns the display width of s: colour codes take no room and
// an escaped "||" takes one column. The editor's strings are ASCII, so the
// output mode makes no difference.
func visualPipeWidth(s string) int {
	return pipeDisplayLen(s, ansi.OutputModeUTF8)
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
	if st.cfg.limited() {
		reachable := make([]*user.User, 0, len(all))
		for _, u := range all {
			if st.e.belowUserCeiling(u) {
				reachable = append(reachable, u)
			}
		}
		return reachable
	}
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
	// A sysop gets the whole editor, as before. Below sysop level the only
	// way in is the pending queue with users.validate, read from the stored
	// record, and then only to approve. The mode is decided from the stored
	// record too, so a sysop demoted since login gets the approve-only view
	// (or nothing), not the full editor.
	sysOpACS := fmt.Sprintf("S%d", e.GetServerConfig().SysOpLevel)
	actor := storedActor(userManager, currentUser)
	if actor == nil || !checkACS(sysOpACS, actor, s, terminal, sessionStartTime) {
		edit, ban := e.mayManageUsers(userManager, currentUser)
		switch {
		case actor != nil && cfg.pendingOnly && e.mayValidateUsers(userManager, currentUser):
			cfg.validateOnly = true
		case actor != nil && !cfg.pendingOnly && (edit || ban):
			// users.edit / users.ban (#1045): accounts below co-sysop
			// level, with only the keys the permissions use.
			cfg.limitedEdit, cfg.limitedBan = edit, ban
		default:
			_ = terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte("\r\n|01Access denied.|07\r\n")), outputMode)
			uiPause(1 * time.Second)
			return nil, "", nil
		}
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
