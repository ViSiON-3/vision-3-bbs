package menu

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/conference"
	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/editor"
	"github.com/ViSiON-3/vision-3-bbs/internal/file"
	"github.com/ViSiON-3/vision-3-bbs/internal/message"
	"github.com/ViSiON-3/vision-3-bbs/internal/session"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
	"github.com/ViSiON-3/vision-3-bbs/internal/transfer"
	"github.com/ViSiON-3/vision-3-bbs/internal/types"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
	"github.com/ViSiON-3/vision-3-bbs/internal/version"
	"github.com/gliderlabs/ssh"
	"golang.org/x/term"
)

// Mutex for protecting access to the oneliners file
var onelinerMutex sync.Mutex
var lastCallerATTokenRegex = regexp.MustCompile(`@([A-Za-z]{2,12})(?::(-?\d+))?@`)

// sessionInputHandlers stores a single *editor.InputHandler per ssh.Session.
// A background goroutine inside InputHandler reads raw bytes from the session
// into a channel; lightbar menus and the full-screen editor both read from that
// channel. This prevents orphaned goroutines from consuming keystrokes after
// the editor exits, which caused the "double key press" bug on return to a menu.
var sessionInputHandlers sync.Map

// getSessionIH returns (creating if necessary) the session-scoped InputHandler
// for s. All callers within the same session share a single goroutine that
// reads from the ssh.Session, so bytes are never lost when control passes
// between the lightbar, message reader, scan, and full-screen editor.
func getSessionIH(s ssh.Session) *editor.InputHandler {
	if v, ok := sessionInputHandlers.Load(s); ok {
		return v.(*editor.InputHandler)
	}
	ih := editor.NewInputHandler(s)
	sessionInputHandlers.Store(s, ih)
	return ih
}

// resetSessionIH stops and removes any session-scoped InputHandler for s.
// Use this before flows that must read from ssh.Session directly (doors/zmodem),
// then recreate via getSessionIH(s) after returning to menu input.
// CloseAndWait is used to ensure the goroutine's deferred setReadInterrupt(nil)
// has run before the door installs its own SetReadInterrupt, preventing the race
// where the handler's cleanup clears the door's interrupt channel.
func resetSessionIH(s ssh.Session) {
	if v, ok := sessionInputHandlers.Load(s); ok {
		if ih, ok := v.(*editor.InputHandler); ok {
			ih.CloseAndWait()
		}
		sessionInputHandlers.Delete(s)
	}
}

type cursorHideContext int

const (
	cursorHideContextDefault cursorHideContext = iota
	cursorHideContextPromptYesNo
)

// shouldHideCursorForSoftwareKeyboard returns true when the cursor should be
// hidden. Default contexts (lightbar menus, admin lists) hide the cursor;
// promptYesNoLightbar keeps it visible so iOS/MuffinTerm software keyboards
// remain active.
func (e *MenuExecutor) shouldHideCursorForSoftwareKeyboard(ctx cursorHideContext) bool {
	switch ctx {
	case cursorHideContextPromptYesNo:
		return false
	default:
		return true
	}
}

func (e *MenuExecutor) hideCursorIfNeeded(terminal *term.Terminal, outputMode ansi.OutputMode, ctx cursorHideContext) bool {
	if !e.shouldHideCursorForSoftwareKeyboard(ctx) {
		return false
	}
	_ = terminalio.WriteProcessedBytes(terminal, []byte("\x1b[?25l"), outputMode)
	return true
}

func (e *MenuExecutor) showCursorIfHidden(terminal *term.Terminal, outputMode ansi.OutputMode, hidden bool) {
	if hidden {
		_ = terminalio.WriteProcessedBytes(terminal, []byte("\x1b[?25h"), outputMode)
	}
}

// holdScreen displays the configured PauseString (centered) and waits for the
// user to press Enter before continuing. Matches Pascal HoldScreen behaviour.
func (e *MenuExecutor) holdScreen(s ssh.Session, terminal *term.Terminal, outputMode ansi.OutputMode, termWidth, termHeight int) {
	pausePrompt := e.LoadedStrings.PauseString
	if pausePrompt == "" {
		pausePrompt = "\r\n|07Press |15[ENTER]|07 to continue... "
	}
	_ = writeCenteredPausePrompt(s, terminal, pausePrompt, outputMode, termWidth, termHeight)
}

// readLineFromSessionIH reads a simple command line from the shared session
// InputHandler so menu input never races with other session readers.
func readLineFromSessionIH(s ssh.Session, terminal *term.Terminal) (string, error) {
	ih := getSessionIH(s)
	line := make([]byte, 0, 64)

	for {
		key, err := ih.ReadKey()
		if err != nil {
			return "", err
		}

		switch key {
		case editor.KeyEnter:
			_, _ = terminal.Write([]byte("\r\n"))
			return string(line), nil
		case editor.KeyBackspace:
			if len(line) > 0 {
				line = line[:len(line)-1]
				_, _ = terminal.Write([]byte("\b \b"))
			}
		default:
			if key >= 32 && key < 127 {
				line = append(line, byte(key))
				_, _ = terminal.Write([]byte{byte(key)})
			}
		}
	}
}

// readLineFromSessionIHAllowAbort reads a simple command line like
// readLineFromSessionIH, but returns errInputAborted when ESC is pressed.
func readLineFromSessionIHAllowAbort(s ssh.Session, terminal *term.Terminal) (string, error) {
	ih := getSessionIH(s)
	line := make([]byte, 0, 64)

	for {
		key, err := ih.ReadKey()
		if err != nil {
			return "", err
		}

		switch key {
		case editor.KeyEnter:
			_, _ = terminal.Write([]byte("\r\n"))
			return string(line), nil
		case editor.KeyBackspace:
			if len(line) > 0 {
				line = line[:len(line)-1]
				_, _ = terminal.Write([]byte("\b \b"))
			}
		case editor.KeyEsc:
			_, _ = terminal.Write([]byte("\r\n"))
			return "", errInputAborted
		default:
			if key >= 32 && key < 127 {
				line = append(line, byte(key))
				_, _ = terminal.Write([]byte{byte(key)})
			}
		}
	}
}

// IPLockoutChecker defines the interface for IP-based authentication lockout.
// This allows the menu system to check and record failed login attempts without
// depending on the specific implementation in main.
type IPLockoutChecker interface {
	IsIPLockedOut(ip string) (bool, time.Time, int)
	RecordFailedLoginAttempt(ip string) bool
	ClearFailedLoginAttempts(ip string)
}

// RunnableFunc defines the signature for functions executable via RUN:
// Returns: authenticatedUser, nextAction (e.g., "GOTO:MENU"), err
// cmdCtx bundles the per-invocation context shared by every RunnableFunc,
// replacing an 11-parameter signature that was repeated across ~140 handlers.
type cmdCtx struct {
	e                *MenuExecutor
	s                ssh.Session
	terminal         *term.Terminal
	userManager      *user.UserMgr
	currentUser      *user.User
	nodeNumber       int
	sessionStartTime time.Time
	outputMode       ansi.OutputMode
	termWidth        int
	termHeight       int
}

type RunnableFunc func(c *cmdCtx, args string) (authenticatedUser *user.User, nextAction string, err error)

// AutoRunTracker definition removed, using the one from types.go

// MenuExecutor handles the loading and execution of ViSiON/2 menus.
type MenuExecutor struct {
	ConfigPath      string                        // DEPRECATED: Use MenuSetPath + "/cfg" or RootConfigPath
	AssetsPath      string                        // DEPRECATED: Use MenuSetPath + "/ansi" or RootAssetsPath
	MenuSetPath     string                        // NEW: Path to the active menu set (e.g., "menus/v3")
	RootConfigPath  string                        // NEW: Path to global configs (e.g., "configs")
	RootAssetsPath  string                        // NEW: Path to global assets (e.g., "assets")
	RunRegistry     map[string]RunnableFunc       // Map RUN: targets to functions (Use local RunnableFunc)
	DoorRegistry    map[string]config.DoorConfig  // Map DOOR: targets to configurations
	OneLiners       []string                      // Loaded oneliners (Consider if these should be menu-set specific)
	LoadedStrings   config.StringsConfig          // Loaded global strings configuration
	Theme           config.ThemeConfig            // Loaded theme configuration
	ServerCfg       config.ServerConfig           // Server configuration (NEW)
	MessageMgr      *message.MessageManager       // <-- ADDED FIELD
	FileMgr         *file.FileManager             // <-- ADDED FIELD: File manager instance
	ConferenceMgr   *conference.ConferenceManager // Conference grouping manager
	IPLockoutCheck  IPLockoutChecker              // IP-based authentication lockout checker
	LoginSequence   []config.LoginItem            // Configurable login sequence from login.json
	SessionRegistry *session.SessionRegistry      // Session registry for who's online
	ChatLeaves      ChatLeafProvider              // V3Net chat leaf provider (nil = local only)
	Protocols       []transfer.ProtocolConfig     // Loaded transfer protocol configurations
	V3NetStatus     V3NetStatusProvider           // V3Net service status (nil if disabled)
	configMu        sync.RWMutex                  // Mutex for thread-safe config updates
}

// NewExecutor creates a new MenuExecutor.
func NewExecutor(menuSetPath, rootConfigPath, rootAssetsPath string, oneLiners []string, doorRegistry map[string]config.DoorConfig, loadedStrings config.StringsConfig, theme config.ThemeConfig, serverCfg config.ServerConfig, msgMgr *message.MessageManager, fileMgr *file.FileManager, confMgr *conference.ConferenceManager, ipLockoutCheck IPLockoutChecker, loginSequence []config.LoginItem, sessionRegistry *session.SessionRegistry, protocols []transfer.ProtocolConfig) *MenuExecutor {

	// Initialize the run registry
	runRegistry := make(map[string]RunnableFunc) // Use local RunnableFunc
	registerPlaceholderRunnables(runRegistry)    // Add placeholder registrations
	registerAppRunnables(runRegistry)            // Add application-specific runnables

	return &MenuExecutor{
		MenuSetPath:     menuSetPath,
		RootConfigPath:  rootConfigPath,
		RootAssetsPath:  rootAssetsPath,
		RunRegistry:     runRegistry,
		DoorRegistry:    doorRegistry,
		OneLiners:       oneLiners,
		LoadedStrings:   loadedStrings,
		Theme:           theme,
		ServerCfg:       serverCfg,
		MessageMgr:      msgMgr,
		FileMgr:         fileMgr,
		ConferenceMgr:   confMgr,
		IPLockoutCheck:  ipLockoutCheck,
		LoginSequence:   loginSequence,
		SessionRegistry: sessionRegistry,
		Protocols:       protocols,
	}
}

// --- Hot Reload Methods ---

// SetDoorRegistry atomically updates the door registry.
func (e *MenuExecutor) SetDoorRegistry(doors map[string]config.DoorConfig) {
	e.configMu.Lock()
	defer e.configMu.Unlock()
	e.DoorRegistry = doors
}

// GetDoorConfig atomically retrieves a door configuration.
func (e *MenuExecutor) GetDoorConfig(name string) (config.DoorConfig, bool) {
	e.configMu.RLock()
	defer e.configMu.RUnlock()
	cfg, ok := e.DoorRegistry[name]
	return cfg, ok
}

// SetLoginSequence atomically updates the login sequence.
func (e *MenuExecutor) SetLoginSequence(sequence []config.LoginItem) {
	e.configMu.Lock()
	defer e.configMu.Unlock()
	e.LoginSequence = sequence
}

// GetLoginSequence atomically retrieves the login sequence.
func (e *MenuExecutor) GetLoginSequence() []config.LoginItem {
	e.configMu.RLock()
	defer e.configMu.RUnlock()
	return e.LoginSequence
}

// SetStrings atomically updates the strings configuration.
func (e *MenuExecutor) SetStrings(strings config.StringsConfig) {
	e.configMu.Lock()
	defer e.configMu.Unlock()
	e.LoadedStrings = strings
}

// GetStrings atomically retrieves the strings configuration.
func (e *MenuExecutor) GetStrings() config.StringsConfig {
	e.configMu.RLock()
	defer e.configMu.RUnlock()
	return e.LoadedStrings
}

// SetTheme atomically updates the theme configuration.
func (e *MenuExecutor) SetTheme(theme config.ThemeConfig) {
	e.configMu.Lock()
	defer e.configMu.Unlock()
	e.Theme = theme
}

// GetTheme atomically retrieves the theme configuration.
func (e *MenuExecutor) GetTheme() config.ThemeConfig {
	e.configMu.RLock()
	defer e.configMu.RUnlock()
	return e.Theme
}

// SetServerConfig atomically updates the server configuration.
func (e *MenuExecutor) SetServerConfig(serverCfg config.ServerConfig) {
	e.configMu.Lock()
	defer e.configMu.Unlock()
	e.ServerCfg = serverCfg
}

// GetServerConfig atomically retrieves the server configuration.
func (e *MenuExecutor) GetServerConfig() config.ServerConfig {
	e.configMu.RLock()
	defer e.configMu.RUnlock()
	return e.ServerCfg
}

// idleTimeout returns the effective idle timeout duration for the given user.
// Sysops and co-sysops are exempt and receive 0 (disabled).
// Pass nil for pre-login contexts (e.g. matrix screen) where there is no
// authenticated user — the configured timeout applies to everyone at that stage.
func (e *MenuExecutor) idleTimeout(u *user.User) time.Duration {
	cfg := e.GetServerConfig()
	if cfg.SessionIdleTimeoutMinutes <= 0 {
		return 0
	}
	if u != nil && u.AccessLevel >= cfg.CoSysOpLevel {
		return 0
	}
	return time.Duration(cfg.SessionIdleTimeoutMinutes) * time.Minute
}

// transferContext returns a context for file transfers rooted at the caller's
// session context so that transfers are cancelled when the session ends.
// If TransferTimeoutMinutes > 0, an additional deadline is layered on top —
// whichever fires first (session close or timeout) wins.
// Callers must invoke the cancel function when done (e.g. via defer cancel()).
func (e *MenuExecutor) transferContext(sessionCtx context.Context) (context.Context, context.CancelFunc) {
	if sessionCtx == nil {
		sessionCtx = context.Background()
	}
	cfg := e.GetServerConfig()
	if cfg.TransferTimeoutMinutes <= 0 {
		return sessionCtx, func() {}
	}
	return context.WithTimeout(sessionCtx, time.Duration(cfg.TransferTimeoutMinutes)*time.Minute)
}

// isCoSysOpOrAbove returns true if the user has CoSysOp or SysOp access level.
func (e *MenuExecutor) isCoSysOpOrAbove(u *user.User) bool {
	return u != nil && u.AccessLevel >= e.ServerCfg.CoSysOpLevel
}

// handleIdleTimeout displays TIMEOUT.ANS (if available) or falls back to the
// idle timeout string, then logs the disconnection. Call this before returning
// LOGOFF/DISCONNECT whenever ErrIdleTimeout is received from any input loop.
func (e *MenuExecutor) handleIdleTimeout(terminal *term.Terminal, outputMode ansi.OutputMode, nodeNumber int, termHeight int) {
	// Try to display TIMEOUT.ANS first.
	ansPath := filepath.Join(e.MenuSetPath, "ansi", "TIMEOUT.ANS")
	if rawContent, err := ansi.GetAnsiFileContent(ansPath); err == nil {
		terminalio.WriteProcessedBytes(terminal, []byte(ansi.ClearScreen()), outputMode)
		if outputMode == ansi.OutputModeCP437 {
			terminal.Write(rawContent)
		} else {
			terminalio.WriteProcessedBytes(terminal, rawContent, outputMode)
		}
	} else {
		// Fall back to the configured string.
		msg := e.LoadedStrings.IdleTimeout
		if msg == "" {
			msg = "\r\n|09You've been idle too long... Come back when you are there!|07\r\n"
		}
		row := termHeight - 1
		if row < 1 {
			row = 1
		}
		terminalio.WriteProcessedBytes(terminal, []byte(fmt.Sprintf("\x1b[%d;1H", row)), outputMode)
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
	}
	slog.Info("idle timeout, disconnecting", "node", nodeNumber, "minutes", e.GetServerConfig().SessionIdleTimeoutMinutes)
}

// remoteIPFromSession extracts the IP address from an SSH session's remote address,
// correctly handling both IPv4 and IPv6 addresses with ports.
func remoteIPFromSession(s ssh.Session) string {
	host, _, err := net.SplitHostPort(s.RemoteAddr().String())
	if err != nil {
		// If no port, use the full address and strip brackets
		return strings.Trim(s.RemoteAddr().String(), "[]")
	}
	// Strip IPv6 brackets if present
	return strings.Trim(host, "[]")
}

func (e *MenuExecutor) showUndefinedMenuInput(terminal *term.Terminal, outputMode ansi.OutputMode, nodeNumber int) {
	errMsg := e.LoadedStrings.ExecUnknownCommand
	processedErrMsg := ansi.ReplacePipeCodes([]byte(errMsg))
	if wErr := terminalio.WriteProcessedBytes(terminal, processedErrMsg, outputMode); wErr != nil {
		slog.Error("failed writing unknown command message", "node", nodeNumber, "error", wErr)
	}
	time.Sleep(500 * time.Millisecond)
}

// setUserMsgConference updates the user's current message conference based on a conference ID.
func (e *MenuExecutor) setUserMsgConference(u *user.User, conferenceID int) {
	u.CurrentMsgConferenceID = conferenceID
	u.CurrentMsgConferenceTag = ""
	if conferenceID != 0 && e.ConferenceMgr != nil {
		if conf, ok := e.ConferenceMgr.GetByID(conferenceID); ok {
			u.CurrentMsgConferenceTag = conf.Tag
		}
	}
}

// setUserFileConference updates the user's current file conference based on a conference ID.
func (e *MenuExecutor) setUserFileConference(u *user.User, conferenceID int) {
	u.CurrentFileConferenceID = conferenceID
	u.CurrentFileConferenceTag = ""
	if conferenceID != 0 && e.ConferenceMgr != nil {
		if conf, ok := e.ConferenceMgr.GetByID(conferenceID); ok {
			u.CurrentFileConferenceTag = conf.Tag
		}
	}
}

// registerPlaceholderRunnables adds dummy functions for testing
func registerPlaceholderRunnables(registry map[string]RunnableFunc) { // Use local RunnableFunc
	// Keep READMAIL as a placeholder for now
	registry["READMAIL"] = func(c *cmdCtx, args string) (*user.User, string, error) {
		e := c.e
		terminal := c.terminal
		currentUser := c.currentUser
		nodeNumber := c.nodeNumber
		outputMode := c.outputMode

		if currentUser == nil {
			slog.Warn("readmail called without logged in user", "node", nodeNumber)
			msg := e.LoadedStrings.ExecReadmailLogin
			wErr := terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
			if wErr != nil {
				slog.Error("failed writing readmail error message", "error", wErr)
			}
			time.Sleep(1 * time.Second)
			return nil, "", nil // No user change, no next action, no error
		}
		msg := fmt.Sprintf(e.LoadedStrings.ExecReadmailPlaceholder, currentUser.Handle)
		wErr := terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
		if wErr != nil {
			slog.Error("failed writing readmail placeholder message", "error", wErr)
		}
		time.Sleep(500 * time.Millisecond)
		return nil, "", nil // No user change, no next action, no error
	}

	// Register DOOR handler — delegates to door_handler.go
	registry["DOOR:"] = func(c *cmdCtx, doorName string) (*user.User, string, error) {
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
			slog.Warn("door called without logged in user", "node", nodeNumber, "door", doorName)
			msg := e.LoadedStrings.ExecDoorLogin
			wErr := terminalio.WriteProcessedBytes(s.Stderr(), ansi.ReplacePipeCodes([]byte(msg)), outputMode)
			if wErr != nil {
				slog.Error("failed writing door error message (not logged in)", "error", wErr)
			}
			return nil, "", nil
		}
		slog.Info("user attempting to run door", "node", nodeNumber, "handle", currentUser.Handle, "door", doorName)

		// Look up door configuration
		doorConfig, exists := e.GetDoorConfig(strings.ToUpper(doorName))
		if !exists {
			slog.Warn("door configuration not found", "door", doorName)
			errMsg := fmt.Sprintf(e.LoadedStrings.ExecDoorNotConfigured, doorName)
			wErr := terminalio.WriteProcessedBytes(s.Stderr(), ansi.ReplacePipeCodes([]byte(errMsg)), outputMode)
			if wErr != nil {
				slog.Error("failed writing door error message (not configured) to stderr", "error", wErr)
			}
			return nil, "", nil
		}

		// Check per-door access level
		if doorConfig.MinAccessLevel > 0 && currentUser.AccessLevel < doorConfig.MinAccessLevel {
			slog.Warn("user denied access to door", "node", nodeNumber, "handle", currentUser.Handle, "level", currentUser.AccessLevel, "door", doorName, "requires", doorConfig.MinAccessLevel)
			errFmt := e.LoadedStrings.DoorAccessDenied
			if strings.TrimSpace(errFmt) == "" {
				errFmt = "\r\n|14Access denied to door: |11%s|07\r\n"
			}
			errMsg := fmt.Sprintf(errFmt, doorName)
			wErr := terminalio.WriteProcessedBytes(s.Stderr(), ansi.ReplacePipeCodes([]byte(errMsg)), outputMode)
			if wErr != nil {
				slog.Error("failed writing door access denied message", "error", wErr)
			}
			time.Sleep(1 * time.Second)
			return nil, "", nil
		}

		// Build door context and execute
		// Use passed termWidth/termHeight (from user preferences) instead of reading from User struct
		ctx := buildDoorCtx(e, s, terminal,
			currentUser.ID, currentUser.Handle, currentUser.RealName,
			currentUser.AccessLevel, currentUser.TimeLimit, currentUser.TimesCalled,
			currentUser.GroupLocation,
			termWidth, termHeight,
			nodeNumber, sessionStartTime, outputMode,
			doorConfig, doorName)
		ctx.UserManager = userManager
		ctx.CurrentUser = currentUser

		// Doors read directly from ssh.Session; reset shared InputHandler first
		// so it does not race and steal door/menu keystrokes.
		resetSessionIH(s)
		cmdErr := executeDoor(ctx)
		_ = getSessionIH(s)

		if cmdErr != nil {
			if errors.Is(cmdErr, ErrDoorBusy) {
				slog.Info("door is busy", "node", nodeNumber, "door", doorName, "handle", currentUser.Handle)
				busyFmt := e.LoadedStrings.DoorBusyFormat
				if strings.TrimSpace(busyFmt) == "" {
					busyFmt = "\r\n|14Door is currently in use: |11%s|07\r\n"
				}
				busyMsg := fmt.Sprintf(busyFmt, doorName)
				terminalio.WriteProcessedBytes(s.Stderr(), ansi.ReplacePipeCodes([]byte(busyMsg)), outputMode)
				time.Sleep(1 * time.Second)
			} else {
				slog.Error("door execution failed", "node", nodeNumber, "handle", currentUser.Handle, "door", doorName, "error", cmdErr)
				doorErrorMessage(ctx, fmt.Sprintf("Error running external program '%s': %v", doorName, cmdErr))
				time.Sleep(2 * time.Second)
			}
		} else {
			slog.Info("door completed", "node", nodeNumber, "handle", currentUser.Handle, "door", doorName)
		}

		return nil, "", nil
	}
}

// registerAppRunnables registers the actual application command functions.
func registerAppRunnables(registry map[string]RunnableFunc) { // Use local RunnableFunc
	registry["PLACEHOLDER"] = runPlaceholderCommand // Canonical handler for undefined/not-yet-implemented options
	registry["MAINLOGOFF"] = runMainLogoffCommand   // MAIN menu logoff with confirmation + GOODBYE.ANS
	registry["IMMEDIATELOGOFF"] = runImmediateLogoffCommand
	registry["SHOWSTATS"] = runShowStats
	registry["SYSTEMSTATS"] = runSystemStats
	registry["LASTCALLERS"] = runLastCallers
	registry["AUTHENTICATE"] = runAuthenticate
	registry["ONELINER"] = runOneliners                              // Register new placeholder
	registry["FULL_LOGIN_SEQUENCE"] = runFullLoginSequence           // Register the new sequence
	registry["SHOWVERSION"] = runShowVersion                         // Register the version display runnable
	registry["LISTUSERS"] = runListUsers                             // Register the user list runnable
	registry["PENDINGVALIDATIONNOTICE"] = runPendingValidationNotice // SysOp notice for new users awaiting validation
	registry["VALIDATEUSER"] = runValidateUser                       // Validate user accounts from admin menu
	registry["NEWUSERVAL"] = runNewUserValidation                    // Prompt to validate new users if any pending
	registry["UNVALIDATEUSER"] = runUnvalidateUser                   // Remove validation from user accounts
	registry["BANUSER"] = runBanUser                                 // Quick-ban user accounts
	registry["DELETEUSER"] = runDeleteUser                           // Soft-delete user accounts (data preserved)
	registry["PURGEUSERS"] = runPurgeUsers                           // Permanently purge soft-deleted users past retention period
	registry["ADMINLISTUSERS"] = runAdminListUsers                   // Admin detailed user browser
	registry["TOGGLEALLOWNEWUSERS"] = runAdminToggleAllowNewUsers    // Toggle allowNewUsers config flag
	registry["LISTMSGAR"] = runListMessageAreas                      // <-- ADDED: Register message area list runnable
	registry["COMPOSEMSG"] = runComposeMessage                       // <-- ADDED: Register compose message runnable
	registry["PROMPTANDCOMPOSEMESSAGE"] = runPromptAndComposeMessage // <-- ADDED: Register prompt/compose runnable (Corrected key to uppercase)
	registry["READMSGS"] = runReadMsgs                               // <-- ADDED: Register message reading runnable
	registry["NEWSCAN"] = runNewscan                                 // <-- ADDED: Register newscan runnable
	registry["LISTFILES"] = runListFiles                             // <-- ADDED: Register file list runnable
	registry["VIEW_FILE"] = runViewFile                              // View file (archives show listing, text gets paged)
	registry["TYPE_TEXT_FILE"] = runTypeTextFile                     // Type text file with paging
	registry["LISTFILEAR"] = runListFileAreas                        // <-- ADDED: Register file area list runnable
	registry["SELECTFILEAREA"] = runSelectFileAreaDispatch           // File area selection (dispatches by fileListingMode)
	registry["SELECTMSGAREA"] = runSelectMessageAreaLightbar         // Register message area selection runnable (lightbar)
	registry["CHANGEMSGCONF"] = runChangeMsgConferenceLightbar       // Change message conference (lightbar)
	registry["NEXTMSGAREA"] = runNextMsgArea                         // Navigate to next message area
	registry["PREVMSGAREA"] = runPrevMsgArea                         // Navigate to previous message area
	registry["NEXTMSGCONF"] = runNextMsgConf                         // Navigate to next message conference
	registry["PREVMSGCONF"] = runPrevMsgConf                         // Navigate to previous message conference
	registry["NEWUSER"] = runNewUser                                 // Register new user application runnable
	registry["GETHEADERTYPE"] = runGetHeaderType                     // Message header style selection
	registry["LISTMSGS"] = runListMsgs                               // List messages in current area
	registry["SENDPRIVMAIL"] = runSendPrivateMail                    // Send private mail to user
	registry["READPRIVMAIL"] = runReadPrivateMail                    // Read private mail
	registry["LISTPRIVMAIL"] = runListPrivateMail                    // List private mail
	registry["NEWSCANCONFIG"] = runNewscanConfig                     // Configure newscan tagged areas
	registry["UPDATENEWSCAN"] = runUpdateNewscanPointers             // Update newscan pointers to a specific date
	registry["NMAILSCAN"] = runNewMailScan                           // New mail scan
	registry["DISPLAYFILE"] = runLoginDisplayFile                    // Display ANSI file
	registry["RUNDOOR"] = runLoginDoor                               // Run external script/door
	registry["FASTLOGIN"] = runFastLogin                             // Inline fast login menu
	registry["LISTDOORS"] = runListDoors                             // List available doors
	registry["OPENDOOR"] = runOpenDoor                               // Prompt and open a door
	registry["DOORINFO"] = runDoorInfo                               // Show door information
	registry["UPLOADFILE"] = runUploadFile                           // ZMODEM file upload
	registry["DOWNLOADFILE"] = runDownloadFile                       // V2-style download: prompt, add to batch, transfer
	registry["BATCHDOWNLOAD"] = runBatchDownload                     // Download tagged batch files
	registry["CLEAR_BATCH"] = runClearBatch                          // Clear tagged file batch queue
	registry["SEARCH_FILES"] = runSearchFiles                        // Search files across all areas
	registry["SHOWFILEINFO"] = runShowFileInfo                       // Show file metadata
	registry["FILE_NEWSCAN"] = runFileNewscan                        // Scan file areas for new uploads
	registry["EDITFILERECORD"] = runEditFileRecord                   // Sysop file review queue
	registry["WANTLIST"] = runWantList                               // File want list
	registry["FILENEWSCANCONFIG"] = runFileNewscanConfig             // File newscan area config
	registry["CFG_FILECOLUMNS"] = runCfgFileColumns                  // Configure file listing columns
	registry["LISTFILES_EXTENDED"] = runListFilesExtended            // Extended file listing (all columns)
	registry["QWKDOWNLOAD"] = runQWKDownload                         // QWK mail packet download
	registry["QWKUPLOAD"] = runQWKUpload                             // QWK REP packet upload
	registry["WHOISONLINE"] = runWhoIsOnline                         // Who's online display
	registry["CFG_HOTKEYS"] = runCfgHotKeys
	registry["CFG_MOREPROMPTS"] = runCfgMorePrompts
	registry["CFG_SCREENWIDTH"] = runCfgScreenWidth
	registry["CFG_SCREENHEIGHT"] = runCfgScreenHeight
	registry["CFG_TERMTYPE"] = runCfgTermType
	registry["CFG_REALNAME"] = runCfgRealName
	registry["CFG_NOTE"] = runCfgNote
	registry["CFG_CUSTOMPROMPT"] = runCfgCustomPrompt
	registry["CFG_COLOR"] = runCfgColor
	registry["CFG_PASSWORD"] = runCfgPassword
	registry["CFG_FILELISTMODE"] = runCfgFileListMode
	registry["CFG_AUTOSIG"] = runCfgAutoSig
	registry["CFG_VIEWCONFIG"] = runCfgViewConfig
	registry["CHAT"] = runChat
	registry["PAGE"] = runPage
	registry["SPONSORMENU"] = runSponsorMenu                 // Sponsor menu (% key in Messages Menu)
	registry["SPONSOREDITAREA"] = runSponsorEditArea         // Edit current message area fields
	registry["PRINTNEWS"] = runPrintNews                     // Display news new since last login (login sequence)
	registry["LISTNEWS"] = runListNews                       // List/read all news items
	registry["EDITNEWS"] = runEditNews                       // SysOp: news management (Add/Delete/Edit/List/View)
	registry["VOTE"] = runVote                               // Voting booths system
	registry["VOTEMANDATORY"] = runVoteOnMandatory           // Mandatory voting check (login sequence)
	registry["LISTNUV"] = runNUVList                         // List NUV candidates and vote tallies
	registry["SCANNUV"] = runNUVScan                         // Vote on pending NUV candidates
	registry["BBSLIST"] = runBBSList                         // List BBS directory entries
	registry["BBSLISTADD"] = runBBSListAdd                   // Add new BBS listing
	registry["BBSLISTEDIT"] = runBBSListEdit                 // Edit BBS listing (owner or sysop)
	registry["BBSLISTDELETE"] = runBBSListDelete             // Delete BBS listing (owner or sysop)
	registry["BBSLISTVERIFY"] = runBBSListVerify             // SysOp: toggle verified flag
	registry["RUMORSLIST"] = runRumorsList                   // List all rumors
	registry["RUMORSADD"] = runRumorsAdd                     // Add a new rumor
	registry["RUMORSDELETE"] = runRumorsDelete               // Delete a rumor
	registry["RUMORSSEARCH"] = runRumorsSearch               // Search rumors
	registry["RUMORSNEWSCAN"] = runRumorsNewscan             // Rumors newscan (since last login)
	registry["RANDOMRUMOR"] = runRandomRumor                 // Display random rumor (login sequence)
	registry["INFOFORMS"] = runInfoForms                     // InfoForms menu (list/fill/view forms)
	registry["INFOFORMVIEW"] = runInfoFormView               // View own completed infoform
	registry["INFOFORMHUNT"] = runInfoFormHunt               // SysOp: browse all users' completed forms
	registry["INFOFORMREQUIRED"] = runInfoFormRequired       // Login sequence: force required forms
	registry["INFOFORMNUKE"] = runInfoFormNuke               // SysOp: delete all forms for a user
	registry["V3NETSTATUS"] = runV3NetStatus                 // V3Net networking status display
	registry["V3NETAREAS"] = runV3NetAreas                   // V3Net area subscriptions
	registry["V3NETPROPOSE"] = runV3NetPropose               // V3Net propose new area
	registry["V3NETACCESSREQUESTS"] = runV3NetAccessRequests // V3Net area access requests (manager)
	registry["V3NETCOORDINATOR"] = runV3NetCoordinator       // V3Net coordinator panel
	registry["V3NETREGISTRY"] = runV3NetRegistry             // V3Net network registry browser
}

func runPlaceholderCommand(c *cmdCtx, args string) (*user.User, string, error) {
	e := c.e
	terminal := c.terminal
	currentUser := c.currentUser
	nodeNumber := c.nodeNumber
	outputMode := c.outputMode

	e.showUndefinedMenuInput(terminal, outputMode, nodeNumber)
	return currentUser, "", nil
}

func runMainLogoffCommand(c *cmdCtx, args string) (*user.User, string, error) {
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

	prompt := e.LoadedStrings.LogOffStr
	if prompt == "" {
		prompt = "\r\n|07Log off now? @"
	}

	confirm, err := e.PromptYesNo(s, terminal, prompt, outputMode, nodeNumber, termWidth, termHeight, false)
	if err != nil {
		if errors.Is(err, io.EOF) {
			return nil, "LOGOFF", io.EOF
		}
		return currentUser, "", err
	}

	if !confirm {
		return currentUser, "", nil
	}

	return runImmediateLogoffCommand(&cmdCtx{e: e, s: s, terminal: terminal, userManager: userManager, currentUser: currentUser, nodeNumber: nodeNumber, sessionStartTime: sessionStartTime, outputMode: outputMode, termWidth: termWidth, termHeight: termHeight}, args)
}

func runImmediateLogoffCommand(c *cmdCtx, args string) (*user.User, string, error) {
	e := c.e
	terminal := c.terminal
	currentUser := c.currentUser
	nodeNumber := c.nodeNumber
	outputMode := c.outputMode

	if displayErr := e.displayFile(terminal, "GOODBYE.ANS", outputMode); displayErr != nil {
		slog.Warn("failed to display GOODBYE.ANS before logoff", "node", nodeNumber, "error", displayErr)
		_ = terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(e.LoadedStrings.ExecGoodbye)), outputMode)
	}

	time.Sleep(1 * time.Second)
	return currentUser, "LOGOFF", nil
}

// runShowStats displays the user statistics screen (YOURSTAT.ANS).
func runShowStats(c *cmdCtx, args string) (*user.User, string, error) {
	e := c.e
	s := c.s
	terminal := c.terminal
	currentUser := c.currentUser
	nodeNumber := c.nodeNumber
	sessionStartTime := c.sessionStartTime
	outputMode := c.outputMode
	termWidth := c.termWidth
	termHeight := c.termHeight

	if currentUser == nil {
		slog.Warn("showstats called without logged in user", "node", nodeNumber)
		msg := e.LoadedStrings.ExecStatsLogin
		wErr := terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
		if wErr != nil {
			slog.Error("failed writing showstats error message", "error", wErr)
		}
		time.Sleep(1 * time.Second)
		return nil, "", nil // Updated return
	}

	ansFilename := "YOURSTAT.ANS"
	// Use MenuSetPath for ANSI file
	fullAnsPath := filepath.Join(e.MenuSetPath, "ansi", ansFilename)
	rawAnsiContent, readErr := ansi.GetAnsiFileContent(fullAnsPath)
	if readErr != nil {
		slog.Error("failed to read file for showstats", "node", nodeNumber, "path", fullAnsPath, "error", readErr)
		msg := fmt.Sprintf(e.LoadedStrings.ExecStatsError, ansFilename)
		wErr := terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
		if wErr != nil {
			slog.Error("failed writing showstats file read error message", "error", wErr)
		}
		time.Sleep(1 * time.Second)
		return nil, "", fmt.Errorf("failed to read %s: %w", ansFilename, readErr) // Updated return
	}

	placeholders := map[string]string{
		"|UH": currentUser.Handle,
		"|UN": currentUser.PrivateNote,
		"|UL": strconv.Itoa(currentUser.AccessLevel),
		"|FL": strconv.Itoa(currentUser.AccessLevel),
		"|UK": strconv.Itoa(currentUser.NumUploads),
		"|NU": strconv.Itoa(currentUser.NumUploads),
		"|DK": "0", "|ND": "0", "|TP": "0", "|NM": "0", "|LC": "N/A",
	}
	if currentUser.TimeLimit <= 0 {
		placeholders["|TL"] = "Unlimited"
	} else {
		elapsedSeconds := time.Since(sessionStartTime).Seconds()
		totalSeconds := float64(currentUser.TimeLimit * 60)
		remainingSeconds := totalSeconds - elapsedSeconds
		if remainingSeconds < 0 {
			remainingSeconds = 0
		}
		placeholders["|TL"] = strconv.Itoa(int(remainingSeconds / 60))
	}

	// Branch based on output mode to preserve encoding correctness
	slog.Debug("showstats output mode", "node", nodeNumber, "outputMode", outputMode)
	var statsDisplayBytes []byte
	if outputMode == ansi.OutputModeUTF8 {
		// UTF-8 mode: Convert CP437→UTF-8 first, then substitute placeholders
		utf8Content := string(ansi.CP437BytesToUTF8(rawAnsiContent))
		for key, val := range placeholders {
			utf8Content = strings.ReplaceAll(utf8Content, key, val)
		}
		statsDisplayBytes = []byte(utf8Content)
	} else {
		// CP437 mode: Substitute placeholders directly on raw bytes
		// (WriteProcessedBytes will pass them through unchanged)
		statsDisplayBytes = rawAnsiContent
		for key, val := range placeholders {
			statsDisplayBytes = bytes.ReplaceAll(statsDisplayBytes, []byte(key), []byte(val))
		}
	}

	// Use WriteProcessedBytes for ClearScreen
	wErr := terminalio.WriteProcessedBytes(terminal, []byte(ansi.ClearScreen()), outputMode)
	if wErr != nil {
		// Log error but continue if possible
		slog.Error("failed clearing screen for showstats", "node", nodeNumber, "error", wErr)
	}
	// For CP437 mode with raw ANSI content, write bytes directly to avoid UTF-8 decode artifacts
	if outputMode == ansi.OutputModeCP437 {
		_, wErr = terminal.Write(statsDisplayBytes)
	} else {
		wErr = terminalio.WriteProcessedBytes(terminal, statsDisplayBytes, outputMode)
	}
	if wErr != nil {
		slog.Error("failed writing processed YOURSTAT.ANS", "node", nodeNumber, "error", wErr)
		return nil, "", wErr // Updated return
	}

	// 5. Wait for Enter key press
	pausePrompt := e.LoadedStrings.PauseString // Use configured pause string
	if pausePrompt == "" {
		pausePrompt = "\r\n|07Press |15[ENTER]|07 to continue... " // Fallback
	}

	slog.Debug("displaying showstats pause prompt (centered)", "node", nodeNumber)
	err := writeCenteredPausePrompt(s, terminal, pausePrompt, outputMode, termWidth, termHeight)
	if err != nil {
		if errors.Is(err, io.EOF) {
			slog.Info("user disconnected during showstats pause", "node", nodeNumber)
			return nil, "LOGOFF", io.EOF
		}
		slog.Error("failed during showstats pause", "error", err)
		return nil, "", err
	}
	return nil, "", nil // Updated return (Success)
}

const (
	oneLinerMaxStored  = 20
	oneLinerMaxDisplay = 10
	oneLinerMaxLength  = 51
	oneLinerNameWidth  = 20
)

type onelinerRecord struct {
	Text             string `json:"text"`
	Anonymous        bool   `json:"anonymous,omitempty"`
	PostedByUsername string `json:"posted_by_username,omitempty"`
	PostedByHandle   string `json:"posted_by_handle,omitempty"`
	PostedAt         string `json:"posted_at,omitempty"`
}

type onelinerRecordCompat struct {
	DisplayName      string `json:"display_name,omitempty"`
	Username         string `json:"username,omitempty"`
	Text             string `json:"text"`
	Anonymous        bool   `json:"anonymous,omitempty"`
	PostedByUsername string `json:"posted_by_username,omitempty"`
	PostedByHandle   string `json:"posted_by_handle,omitempty"`
	PostedAt         string `json:"posted_at,omitempty"`
}

// Run executes the menu logic for a given starting menu name.
// Reverted s parameter back to ssh.Session
// Added outputMode parameter
// Added currentAreaName parameter
func (e *MenuExecutor) Run(s ssh.Session, terminal *term.Terminal, userManager *user.UserMgr, currentUser *user.User, startMenu string, nodeNumber int, sessionStartTime time.Time, autoRunLog types.AutoRunTracker, outputMode ansi.OutputMode, currentAreaName string, termWidth int, termHeight int) (string, *user.User, error) {
	currentMenuName := strings.ToUpper(startMenu)
	var previousMenuName string // Track the last menu visited
	// var authenticatedUserResult *user.User // Unused

	// Clean up the session-scoped InputHandler when this Run() returns so the
	// goroutine is not reused across re-entrant calls or after the session ends.
	// resetSessionIH calls CloseAndWait() before deleting, which stops the telnet
	// read goroutine via the read-interrupt mechanism before a new one is created.
	// Without this, two goroutines compete on the same bufio.Reader, freezing input.
	defer resetSessionIH(s)

	if currentUser != nil {
		slog.Debug("running menu for user", "handle", currentUser.Handle, "level", currentUser.AccessLevel)
	} else {
		slog.Debug("running menu for potentially unauthenticated user (login phase)")
	}

	// Apply the session-level idle timeout to the shared InputHandler.
	// Sysops/co-sysops are exempt (idleTimeout returns 0 for them).
	// This covers every ReadKey call in the entire session — menus, prompts,
	// message reader, etc. — without requiring per-call changes.
	getSessionIH(s).SetSessionIdleTimeout(e.idleTimeout(currentUser))

	for {
		slog.Info("running menu", "menu", currentMenuName, "previous", previousMenuName, "node", nodeNumber)

		var userInput string // Declare userInput here (Keep this one)
		// Removed authenticatedUserResult declaration from here
		// Numeric commands must be explicitly defined in KEYS tokens (no positional matching)

		// Determine ANSI filename using standard convention
		ansFilename := currentMenuName + ".ANS"
		// Use MenuSetPath for ANSI file
		fullAnsPath := filepath.Join(e.MenuSetPath, "ansi", ansFilename)

		// Process the associated ANSI file to get display bytes and coordinates
		rawAnsiContent, readErr := ansi.GetAnsiFileContent(fullAnsPath)
		if readErr == nil {
			if currentMenuName == "ADMIN" {
				pendingCount := pendingValidationCount(userManager)
				rawAnsiContent = bytes.ReplaceAll(rawAnsiContent, []byte("{{PENDING_VALIDATIONS}}"), []byte(strconv.Itoa(pendingCount)))
			}
			// Substitute global server-state placeholders before ANSI processing,
			// so multi-letter codes like |NEWUSERS aren't mis-parsed as coord markers.
			newUsersVal := "NO"
			if e.GetServerConfig().AllowNewUsers {
				newUsersVal = "YES"
			}
			rawAnsiContent = bytes.ReplaceAll(rawAnsiContent, []byte("|NEWUSERS"), []byte(newUsersVal))
			currentAreaTag, currentAreaDisplayName := e.resolveCurrentAreaTokens(currentUser, currentAreaName)
			currentFileAreaTag, currentFileAreaDisplayName := e.resolveCurrentFileAreaTokens(currentUser)
			// Replace longer tokens first to avoid partial replacement conflicts (e.g. |FCONFPATH, |CFAN vs |CFA vs |CAN vs |CA).
			rawAnsiContent = bytes.ReplaceAll(rawAnsiContent, []byte("|FCONFPATH"), []byte(e.resolveFileConferencePath(currentUser)))
			rawAnsiContent = bytes.ReplaceAll(rawAnsiContent, []byte("|CFAN"), []byte(currentFileAreaDisplayName))
			rawAnsiContent = bytes.ReplaceAll(rawAnsiContent, []byte("|CFA"), []byte(currentFileAreaTag))
			rawAnsiContent = bytes.ReplaceAll(rawAnsiContent, []byte("|CAN"), []byte(currentAreaDisplayName))
			rawAnsiContent = bytes.ReplaceAll(rawAnsiContent, []byte("|CA"), []byte(currentAreaTag))
			rawAnsiContent = replaceMenuATCode(rawAnsiContent, "UC", strconv.Itoa(userManager.GetUserCount()))
			rawAnsiContent = replaceMenuATCode(rawAnsiContent, "U", strconv.Itoa(e.SessionRegistry.ActiveCount()))
			// @RR@ — Random Rumor text (supports @RR@, @RR:50@, @RR######@)
			rumorLevel := 1 // default MinLevel when no user context
			if currentUser != nil {
				rumorLevel = currentUser.AccessLevel
			}
			rawAnsiContent = expandRandomRumorATCode(rawAnsiContent, e.RootConfigPath, rumorLevel)
		}
		var ansiProcessResult ansi.ProcessAnsiResult
		var processErr error
		if readErr != nil {
			slog.Error("failed to read ANSI file", "file", ansFilename, "error", readErr)
			// Display error message to user (using new helper)
			errMsg := fmt.Sprintf("\r\n|01Error reading screen file: %s|07\r\n", ansFilename)
			wErr := terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(errMsg)), outputMode)
			if wErr != nil {
				slog.Error("failed writing screen read error", "error", wErr)
			}
			// Reading the screen file is critical, return error
			return "", nil, fmt.Errorf("failed to read screen file %s: %w", ansFilename, readErr)
		}

		// Process for coords and display bytes
		// Use CP437 mode to keep raw bytes for coordinate tracking, then convert based on outputMode
		ansiProcessResult, processErr = ansi.ProcessAnsiAndExtractCoords(rawAnsiContent, ansi.OutputModeCP437)
		if processErr != nil {
			slog.Error("failed to process ANSI file, display may be incorrect", "file", ansFilename, "error", processErr)
			// Processing error is also critical, return error
			return "", nil, fmt.Errorf("failed to process screen file %s: %w", ansFilename, processErr)
		}

		// Convert encoding based on output mode (similar to SHOWSTATS fix)
		if outputMode == ansi.OutputModeUTF8 {
			// UTF-8 mode: Convert CP437 bytes to UTF-8 for proper display
			ansiProcessResult.DisplayBytes = ansi.CP437BytesToUTF8(ansiProcessResult.DisplayBytes)
		}
		// CP437 mode: DisplayBytes already contain raw CP437, pass through as-is

		// --- SPECIAL HANDLING FOR LOGIN MENU INTERACTION ---
		if currentMenuName == "LOGIN" {
			if currentUser != nil {
				slog.Warn("attempting to run LOGIN menu for already authenticated user, skipping login, going to MAIN", "handle", currentUser.Handle)

				// Set default message area if not already set (e.g., SSH auto-login)
				if currentUser.CurrentMessageAreaID == 0 && e.MessageMgr != nil {
					for _, area := range e.MessageMgr.ListAreas() {
						if checkACS(area.ACSRead, currentUser, s, terminal, sessionStartTime) {
							currentUser.CurrentMessageAreaID = area.ID
							currentUser.CurrentMessageAreaTag = area.Tag
							e.setUserMsgConference(currentUser, area.ConferenceID)
							break
						}
					}
				}

				// Set default file area if not already set
				if currentUser.CurrentFileAreaID == 0 && e.FileMgr != nil {
					for _, area := range e.FileMgr.ListAreas() {
						if checkACS(area.ACSList, currentUser, s, terminal, sessionStartTime) {
							currentUser.CurrentFileAreaID = area.ID
							currentUser.CurrentFileAreaTag = area.Tag
							e.setUserFileConference(currentUser, area.ConferenceID)
							break
						}
					}
				}

				// Persist defaults
				if userManager != nil {
					if saveErr := userManager.UpdateUser(currentUser); saveErr != nil {
						slog.Error("failed to save user default area selections", "error", saveErr)
					}
				}

				currentMenuName = "MAIN"
				previousMenuName = "LOGIN" // Set previous explicitly here
				continue
			}

			// Display the processed LOGIN screen, truncated to fit terminal height
			terminalio.WriteProcessedBytes(terminal, []byte(ansi.ClearScreen()), outputMode) // Clear first
			displayBytes := ansiProcessResult.DisplayBytes
			if termHeight > 0 {
				// Truncate ANSI output to terminal height to prevent scrolling
				// which would shift all Y coordinates
				lines := bytes.Split(displayBytes, []byte("\n"))
				if len(lines) > termHeight {
					displayBytes = bytes.Join(lines[:termHeight], []byte("\n"))
					slog.Debug("truncated LOGIN.ANS to fit terminal", "from", len(lines), "to", termHeight, "rows", termHeight)
				}
			}
			// For CP437 mode, write raw bytes directly to avoid UTF-8 false positives
			// (some CP437 byte pairs like 0xDF 0xB2 accidentally form valid UTF-8)
			// For UTF-8 mode, bytes are already converted to UTF-8, pass through
			var wErr error
			if outputMode == ansi.OutputModeCP437 {
				_, wErr = terminal.Write(displayBytes)
			} else {
				wErr = terminalio.WriteProcessedBytes(terminal, displayBytes, outputMode)
			}
			if wErr != nil {
				slog.Error("failed to write processed LOGIN.ANS bytes to terminal", "error", wErr)
				return "", nil, fmt.Errorf("failed to display LOGIN.ANS: %w", wErr)
			}

			// Handle the interactive login prompt using extracted coordinates and colors
			authenticatedUserResult, loginErr := e.handleLoginPrompt(s, terminal, userManager, nodeNumber, ansiProcessResult.FieldCoords, ansiProcessResult.FieldColors, outputMode, termWidth, termHeight)

			// Process result of login attempt
			if loginErr != nil {
				if errors.Is(loginErr, io.EOF) {
					slog.Info("user disconnected during login prompt")
					return "LOGOFF", nil, nil // Signal logoff
				}
				slog.Error("error during login prompt handling", "error", loginErr)
				return "", nil, loginErr // Propagate critical error
			}

			if authenticatedUserResult != nil {
				slog.Info("login successful, proceeding based on LOGIN menu config", "handle", authenticatedUserResult.Handle)
				currentUser = authenticatedUserResult // Update the user for this Run context

				// --- Update user's terminal dimensions from detected size ---
				if termWidth > 0 && termHeight > 0 {
					currentUser.ScreenWidth = termWidth
					currentUser.ScreenHeight = termHeight
					slog.Info("updated user screen preferences", "handle", currentUser.Handle, "width", termWidth, "height", termHeight)
					if userManager != nil {
						if saveErr := userManager.UpdateUser(currentUser); saveErr != nil {
							slog.Error("failed to save user screen preferences", "error", saveErr)
						}
					}
				}

				// --- BEGIN Set Default Message Area (only if not already set from saved prefs) ---
				if currentUser.CurrentMessageAreaID == 0 && e.MessageMgr != nil {
					allAreas := e.MessageMgr.ListAreas() // Already sorted by Position
					slog.Debug("found message areas for user", "count", len(allAreas), "handle", currentUser.Handle)
					foundDefaultArea := false
					for _, area := range allAreas {
						// Check if user has read access to this area
						if checkACS(area.ACSRead, currentUser, s, terminal, sessionStartTime) {
							slog.Info("setting default message area for user", "handle", currentUser.Handle, "id", area.ID, "tag", area.Tag)
							currentUser.CurrentMessageAreaID = area.ID
							currentUser.CurrentMessageAreaTag = area.Tag
							e.setUserMsgConference(currentUser, area.ConferenceID)
							foundDefaultArea = true
							break // Found the first accessible area
						} else {
							slog.Debug("user denied read access to message area", "handle", currentUser.Handle, "id", area.ID, "tag", area.Tag, "acs", area.ACSRead)
						}
					}
					if !foundDefaultArea {
						slog.Warn("user has no access to any message areas", "handle", currentUser.Handle)
						currentUser.CurrentMessageAreaID = 0
						currentUser.CurrentMessageAreaTag = ""
					}
				} else if currentUser.CurrentMessageAreaID != 0 {
					slog.Info("user has saved message area", "handle", currentUser.Handle, "id", currentUser.CurrentMessageAreaID, "tag", currentUser.CurrentMessageAreaTag, "conferenceID", currentUser.CurrentMsgConferenceID, "conferenceTag", currentUser.CurrentMsgConferenceTag)
				}
				// --- END Set Default Message Area ---

				// --- BEGIN Set Default File Area (only if not already set from saved prefs) ---
				if currentUser.CurrentFileAreaID == 0 && e.FileMgr != nil {
					allFileAreas := e.FileMgr.ListAreas() // Assumes ListAreas is sorted by ID
					slog.Debug("found file areas for user", "count", len(allFileAreas), "handle", currentUser.Handle)
					foundDefaultFileArea := false
					for _, area := range allFileAreas {
						// Check if user has list access to this area
						if checkACS(area.ACSList, currentUser, s, terminal, sessionStartTime) { // Use ACSList
							slog.Info("setting default file area for user", "handle", currentUser.Handle, "id", area.ID, "tag", area.Tag)
							currentUser.CurrentFileAreaID = area.ID
							currentUser.CurrentFileAreaTag = area.Tag
							e.setUserFileConference(currentUser, area.ConferenceID)
							foundDefaultFileArea = true
							break // Found the first accessible area
						} else {
							slog.Debug("user denied list access to file area", "handle", currentUser.Handle, "id", area.ID, "tag", area.Tag, "acs", area.ACSList)
						}
					}
					if !foundDefaultFileArea {
						slog.Warn("user has no access to any file areas", "handle", currentUser.Handle)
						currentUser.CurrentFileAreaID = 0
						currentUser.CurrentFileAreaTag = ""
					}
				} else if currentUser.CurrentFileAreaID != 0 {
					slog.Info("user has saved file area", "handle", currentUser.Handle, "id", currentUser.CurrentFileAreaID, "tag", currentUser.CurrentFileAreaTag, "conferenceID", currentUser.CurrentFileConferenceID, "conferenceTag", currentUser.CurrentFileConferenceTag)
				}
				// --- END Set Default File Area ---

				// Persist default area/conference selections to disk
				if userManager != nil {
					if saveErr := userManager.UpdateUser(currentUser); saveErr != nil {
						slog.Error("failed to save user default area selections", "error", saveErr)
					}
				}

				// --- BEGIN POST-AUTHENTICATION TRANSITION ---
				// Load LOGIN.CFG to find the default action
				loginCfgPath := filepath.Join(e.MenuSetPath, "cfg") // Use correct path structure
				loginCommands, loadCmdErr := LoadCommands("LOGIN", loginCfgPath)
				if loadCmdErr != nil {
					slog.Error("failed to load LOGIN.CFG after successful authentication", "path", filepath.Join(loginCfgPath, "LOGIN.CFG"), "error", loadCmdErr)
					// Return an error? Or try to default to MAIN?
					return "LOGOFF", currentUser, fmt.Errorf("failed loading LOGIN.CFG post-auth") // Logoff user on critical error
				}

				// Find the default command (Keys == "")
				nextAction := "" // Default action if not found?
				foundDefault := false
				for _, cmd := range loginCommands {
					if cmd.Keys == "" { // Check for empty string
						if cmd.Command == "RUN:AUTHENTICATE" {
							continue
						}
						if checkACS(cmd.ACS, currentUser, s, terminal, sessionStartTime) { // Use ssh.Session 's'
							nextAction = cmd.Command
							foundDefault = true
							slog.Debug("found default command in LOGIN.CFG after auth", "command", nextAction)
							break // Found the relevant default command (e.g., GOTO:MAIN)
						} else {
							slog.Warn("user denied default command in LOGIN.CFG", "handle", currentUser.Handle, "command", cmd.Command, "acs", cmd.ACS)
						}
					}
				}

				if !foundDefault {
					slog.Error("no accessible default command found in LOGIN.CFG, logging off", "handle", currentUser.Handle)
					return "LOGOFF", currentUser, fmt.Errorf("no accessible default command found in LOGIN.CFG")
				}
				// -- Return the next action AND the authenticated user --
				return nextAction, currentUser, nil
			} else { // authenticatedUserResult == nil
				slog.Info("login failed, redisplaying LOGIN menu")
				continue // Restart loop for LOGIN
			}
		} // --- END SPECIAL LOGIN INTERACTION BLOCK ---

		// --- REGULAR MENU PROCESSING (Common for ALL menus, including LOGIN after interaction) ---
		// 1. Load Menu Definition (.MNU)
		menuMnuPath := filepath.Join(e.MenuSetPath, "mnu") // Use correct path structure for MNU
		menuRec, err := LoadMenu(currentMenuName, menuMnuPath)
		if err != nil {
			errMsg := fmt.Sprintf(e.LoadedStrings.ExecMenuLoadError, currentMenuName, err)
			processedErrMsg := ansi.ReplacePipeCodes([]byte(errMsg))
			// Use new helper for error message
			wErr := terminalio.WriteProcessedBytes(terminal, processedErrMsg, outputMode)
			if wErr != nil {
				slog.Error("failed writing menu load error message", "error", wErr)
			}
			slog.Error(errMsg)
			return "", nil, fmt.Errorf("failed to load menu %s: %w", currentMenuName, err)
		}

		// 2. Load Commands (.CFG) for the *current* menu (which might be LOGIN)
		menuCfgPath := filepath.Join(e.MenuSetPath, "cfg") // Use correct path structure for CFG
		commands, err := LoadCommands(currentMenuName, menuCfgPath)
		if err != nil {
			slog.Warn("failed to load commands for menu", "menu", currentMenuName, "error", err)
			commands = []CommandRecord{} // Use empty slice
		}

		// Determine default node activity for this menu from autorun entries
		menuDefaultActivity := currentMenuName
		for _, cmd := range commands {
			if (cmd.Keys == "//" || cmd.Keys == "~~") && cmd.NodeActivity != "" {
				menuDefaultActivity = cmd.NodeActivity
				break
			}
		}
		// Set default activity on session for Who's Online display
		if sess := e.SessionRegistry.Get(nodeNumber); sess != nil {
			sess.Mutex.Lock()
			sess.Activity = menuDefaultActivity
			sess.Mutex.Unlock()
		}

		// Check Menu Password if required
		menuPassword := menuRec.Password
		if menuPassword != "" {
			slog.Debug("menu requires password", "menu", currentMenuName)
			passwordOk := false
			for i := 0; i < 3; i++ { // Allow 3 attempts
				prompt := fmt.Sprintf(e.LoadedStrings.ExecMenuPasswordPrompt, currentMenuName, i+1)
				processedPrompt := ansi.ReplacePipeCodes([]byte(prompt))
				wErr := terminalio.WriteProcessedBytes(terminal, processedPrompt, outputMode)
				if wErr != nil {
					slog.Error("failed writing menu password prompt", "node", nodeNumber, "error", wErr)
				}

				// Use our helper for secure input reading (using ssh.Session 's')
				inputPassword, err := readPasswordSecurely(s, terminal, outputMode)
				if err != nil {
					if errors.Is(err, io.EOF) {
						slog.Info("user disconnected during menu password entry", "menu", currentMenuName)
						return "LOGOFF", nil, nil // Signal logoff
					}
					if errors.Is(err, errInputAborted) { // Check for specific error
						slog.Info("user interrupted password entry for menu", "menu", currentMenuName)
						return "LOGOFF", nil, nil // Signal logoff
					}
					slog.Error("failed to read password input securely", "error", err)
					return "", nil, fmt.Errorf("failed reading password: %w", err)
				}
				if inputPassword == menuPassword {
					passwordOk = true
					// Use new helper for feedback message
					wErr := terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(e.LoadedStrings.ExecPasswordAccepted)), outputMode)
					if wErr != nil {
						slog.Error("failed writing password accepted message", "error", wErr)
					}
					break
				} else {
					// Use new helper for feedback message
					wErr := terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(e.LoadedStrings.ExecIncorrectPassword)), outputMode)
					if wErr != nil {
						slog.Error("failed writing incorrect password message", "error", wErr)
					}
				}
			}
			if !passwordOk {
				slog.Warn("user failed password entry for menu", "menu", currentMenuName, "user", currentUser)
				// Use new helper for feedback message
				wErr := terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(e.LoadedStrings.ExecTooManyAttempts)), outputMode)
				if wErr != nil {
					slog.Error("failed writing too many attempts message", "error", wErr)
				}
				time.Sleep(1 * time.Second)
				return "LOGOFF", nil, nil // Signal logoff after too many failures
			}
		}

		// Check Menu ACS before proceeding
		menuACS := menuRec.ACS
		if !checkACS(menuACS, currentUser, s, terminal, sessionStartTime) { // Use ssh.Session 's'
			slog.Info("user denied access to menu", "menu", currentMenuName, "acs", menuACS, "user", currentUser)
			errMsg := e.LoadedStrings.ExecAccessDenied
			processedErrMsg := ansi.ReplacePipeCodes([]byte(errMsg))
			// Use new helper for error message
			wErr := terminalio.WriteProcessedBytes(terminal, processedErrMsg, outputMode)
			if wErr != nil {
				slog.Error("failed writing ACS denied message", "error", wErr)
			}
			time.Sleep(1 * time.Second) // Brief pause
			return "LOGOFF", nil, nil   // Signal logoff
		}

		// --- AutoRun Command Execution ---
		autoRunActionTaken := false
		for _, cmd := range commands {
			if cmd.Keys == "//" || cmd.Keys == "~~" {
				autoRunKey := fmt.Sprintf("%s:%s", currentMenuName, cmd.Command) // Unique key per menu/command

				if cmd.Keys == "//" && autoRunLog[autoRunKey] {
					slog.Debug("skipping already executed run-once command", "command", autoRunKey)
					continue // Skip if already run
				}
				if checkACS(cmd.ACS, currentUser, s, terminal, sessionStartTime) { // Use ssh.Session 's'
					slog.Info("executing autorun command", "keys", cmd.Keys, "command", cmd.Command, "acs", cmd.ACS)

					if cmd.Keys == "//" {
						autoRunLog[autoRunKey] = true
					}
					nextAction, nextMenu, userResult, err := e.executeCommandAction(cmd.Command, s, terminal, userManager, currentUser, nodeNumber, sessionStartTime, outputMode, termWidth, termHeight)
					if err != nil {
						return "", userResult, err
					}
					if nextAction == "GOTO" {
						previousMenuName = currentMenuName
						currentMenuName = nextMenu
						autoRunActionTaken = true
						break
					} else if nextAction == "LOGOFF" {
						return "LOGOFF", userResult, nil
					} else if nextAction == "CONTINUE" {
						if userResult != nil {
							currentUser = userResult
						}
					}
				} else {
					slog.Debug("autorun command denied by ACS", "keys", cmd.Keys, "command", cmd.Command, "acs", cmd.ACS)
				}
			}
		}
		if autoRunActionTaken {
			continue
		}
		// --- End AutoRun Command Execution ---

		// 3. Display ANSI Screen (Processed Bytes) - Moved display logic here for ALL menus
		// (Avoid double-display for LOGIN which handles its own display before prompt)
		// We still need the raw content for potential lightbar background
		// Note: ansBackgroundBytes is currently unused but will be needed for full lightbar implementation
		// ansBackgroundBytes := ansiProcessResult.DisplayBytes
		if currentMenuName != "LOGIN" {
			// Truncate ANSI output to terminal height to prevent scrolling
			displayBytes := ansiProcessResult.DisplayBytes
			// Prepend clear sequence when CLR is set (single write for reliable clearing)
			if menuRec.GetClrScrBefore() {
				displayBytes = append([]byte(ansi.ClearScreen()), displayBytes...)
			}
			if termHeight > 0 {
				lines := bytes.Split(displayBytes, []byte("\n"))
				if len(lines) > termHeight {
					displayBytes = bytes.Join(lines[:termHeight], []byte("\n"))
					slog.Debug("truncated menu ANSI to fit terminal", "menu", currentMenuName, "from", len(lines), "to", termHeight, "rows", termHeight)
				}
			}
			// For CP437 mode, write raw bytes directly to avoid UTF-8 false positives
			var wErr error
			if outputMode == ansi.OutputModeCP437 {
				_, wErr = terminal.Write(displayBytes)
			} else {
				wErr = terminalio.WriteProcessedBytes(terminal, displayBytes, outputMode)
			}
			if wErr != nil {
				slog.Error("failed writing ANSI screen", "menu", currentMenuName, "error", wErr)
				return "", nil, fmt.Errorf("failed displaying screen: %w", wErr)
			}
		}

		// --- Check for Lightbar Menu (.BAR) ---
		// Check if a .BAR file exists for this menu in the MENU SET directory
		isLightbarMenu := HasBarFile(currentMenuName, e.MenuSetPath)

		// Variable declarations for command handling
		// var userInput string // REMOVE this redeclaration
		// var numericMatchAction string // Moved declaration up

		// 4. Determine Input Mode / Method
		if isLightbarMenu {
			slog.Debug("entering lightbar input mode", "menu", currentMenuName)

			// Load lightbar options from the config directory
			lightbarOptions, loadErr := loadLightbarOptions(currentMenuName, e)
			if loadErr != nil {
				slog.Error("failed to load lightbar options", "menu", currentMenuName, "error", loadErr)
				isLightbarMenu = false
			} else if len(lightbarOptions) == 0 {
				slog.Warn("no valid lightbar options loaded", "menu", currentMenuName)
				isLightbarMenu = false
			}

			if isLightbarMenu {
				cursorHidden := e.hideCursorIfNeeded(terminal, outputMode, cursorHideContextDefault)
				ansBackgroundBytes := ansiProcessResult.DisplayBytes

				// Initially draw with first option selected
				selectedIndex := 0
				drawErr := drawLightbarMenu(terminal, ansBackgroundBytes, lightbarOptions, selectedIndex, outputMode, false)
				if drawErr != nil {
					slog.Error("failed to draw lightbar menu", "menu", currentMenuName, "error", drawErr)
					e.showCursorIfHidden(terminal, outputMode, cursorHidden)
					isLightbarMenu = false
				} else {
					// Process keyboard navigation for lightbar.
					// Use the session-scoped InputHandler so the same goroutine that
					// reads from the SSH session is shared with any editor invocations
					// triggered from this menu (e.g. COMPOSEMSG). This prevents the
					// orphaned goroutine from consuming the first keystroke after the
					// editor exits, which caused the "double key press" bug.
					lightbarResult := "" // Use a local variable for the result
					inputLoop := true
					sessionIH := getSessionIH(s)
					for inputLoop {
						key, err := sessionIH.ReadKey()
						if err != nil {
							e.showCursorIfHidden(terminal, outputMode, cursorHidden)
							if errors.Is(err, io.EOF) {
								slog.Info("user disconnected during lightbar input", "menu", currentMenuName)
								return "LOGOFF", nil, nil
							}
							if errors.Is(err, editor.ErrIdleTimeout) {
								e.handleIdleTimeout(terminal, outputMode, nodeNumber, termHeight)
								return "LOGOFF", nil, nil
							}
							slog.Error("failed to read lightbar input", "menu", currentMenuName, "error", err)
							return "", nil, fmt.Errorf("failed reading lightbar input: %w", err)
						}
						slog.Debug("lightbar input key", "key", key)

						switch key {
						case editor.KeyArrowUp:
							prevIndex := selectedIndex
							selectedIndex--
							if selectedIndex < 0 {
								selectedIndex = len(lightbarOptions) - 1
							}
							if prevIndex != selectedIndex {
								_ = drawLightbarOption(terminal, lightbarOptions[prevIndex], false, outputMode)
								_ = drawLightbarOption(terminal, lightbarOptions[selectedIndex], true, outputMode)
							}
						case editor.KeyArrowDown:
							prevIndex := selectedIndex
							selectedIndex++
							if selectedIndex >= len(lightbarOptions) {
								selectedIndex = 0
							}
							if prevIndex != selectedIndex {
								_ = drawLightbarOption(terminal, lightbarOptions[prevIndex], false, outputMode)
								_ = drawLightbarOption(terminal, lightbarOptions[selectedIndex], true, outputMode)
							}
						case editor.KeyHome:
							if selectedIndex != 0 {
								prevIndex := selectedIndex
								selectedIndex = 0
								_ = drawLightbarOption(terminal, lightbarOptions[prevIndex], false, outputMode)
								_ = drawLightbarOption(terminal, lightbarOptions[selectedIndex], true, outputMode)
							}
						case editor.KeyEnd:
							lastIdx := len(lightbarOptions) - 1
							if selectedIndex != lastIdx {
								prevIndex := selectedIndex
								selectedIndex = lastIdx
								_ = drawLightbarOption(terminal, lightbarOptions[prevIndex], false, outputMode)
								_ = drawLightbarOption(terminal, lightbarOptions[selectedIndex], true, outputMode)
							}
						case int('\r'), int('\n'): // Enter (CR or LF) - select current item
							if selectedIndex >= 0 && selectedIndex < len(lightbarOptions) {
								lightbarResult = lightbarOptions[selectedIndex].HotKey
								inputLoop = false
							}
						case editor.KeyEsc:
							// Bare ESC (InputHandler already consumed any ANSI sequence) — ignore
						default:
							if key >= int('1') && key <= int('9') {
								// Direct selection by number
								numIndex := key - int('1') // Convert 1-9 to 0-8
								if numIndex >= 0 && numIndex < len(lightbarOptions) {
									prevIndex := selectedIndex
									selectedIndex = numIndex
									if prevIndex != selectedIndex {
										_ = drawLightbarOption(terminal, lightbarOptions[prevIndex], false, outputMode)
										_ = drawLightbarOption(terminal, lightbarOptions[selectedIndex], true, outputMode)
									}
									lightbarResult = lightbarOptions[numIndex].HotKey
									inputLoop = false
								}
							} else if key >= 32 && key < 127 {
								// Check if printable key matches any hotkey directly
								keyStr := strings.ToUpper(string(rune(key)))
								for _, opt := range lightbarOptions {
									if keyStr == opt.HotKey {
										lightbarResult = opt.HotKey
										inputLoop = false
										break
									}
								}
							}
							// Control chars and other special codes are ignored
						}
					}
					slog.Debug("processed lightbar input", "result", lightbarResult)
					e.showCursorIfHidden(terminal, outputMode, cursorHidden)
					// Set userInput to lightbar result if a selection was made
					if lightbarResult != "" {
						userInput = lightbarResult
					}
				}
			}

			if !isLightbarMenu || userInput == "" {
				// Fallback to standard input if lightbar loading failed or no valid selection made
				e.deliverPendingPages(terminal, nodeNumber, outputMode)
				// Display Prompt (Skip if USEPROMPT is false)
				if menuRec.GetUsePrompt() { // Condition changed: Only check UsePrompt
					err = e.displayPrompt(terminal, menuRec, currentUser, userManager, nodeNumber, currentMenuName, sessionStartTime, outputMode, currentAreaName) // Pass currentAreaName
					if err != nil {
						return "", nil, err // Propagate the error
					}
				} else {
					// Log message remains the same, but the condition causing it is now just UsePrompt==false
					slog.Debug("skipping prompt display", "menu", currentMenuName, "usePrompt", menuRec.GetUsePrompt(), "prompt1Empty", menuRec.Prompt1 == "")
				}

				// Read User Input Line via shared InputHandler to avoid reader races.
				input, err := readLineFromSessionIH(s, terminal)
				if err != nil {
					if err == io.EOF {
						slog.Info("user disconnected during menu input", "menu", currentMenuName)
						return "LOGOFF", nil, nil // Signal logoff
					}
					slog.Error("failed to read input for menu", "menu", currentMenuName, "error", err)
					return "", nil, fmt.Errorf("failed reading input: %w", err)
				}
				userInput = strings.ToUpper(strings.TrimSpace(input))
				slog.Debug("user input", "input", userInput)
			}
		} else {
			// --- Standard Menu Input Handling ---
			e.deliverPendingPages(terminal, nodeNumber, outputMode)
			// Display Prompt (Skip if USEPROMPT is false)
			slog.Debug("checking prompt display for menu", "menu", currentMenuName, "usePrompt", menuRec.GetUsePrompt())
			if menuRec.GetUsePrompt() { // Condition changed: Only check UsePrompt
				slog.Debug("calling displayPrompt for menu", "menu", currentMenuName)
				err = e.displayPrompt(terminal, menuRec, currentUser, userManager, nodeNumber, currentMenuName, sessionStartTime, outputMode, currentAreaName) // Pass currentAreaName
				slog.Debug("returned from displayPrompt for menu", "menu", currentMenuName, "error", err)
				if err != nil {
					return "", nil, err // Propagate the error
				}
			} else {
				// Log message remains the same, but the condition causing it is now just UsePrompt==false
				slog.Debug("skipping prompt display", "menu", currentMenuName, "usePrompt", menuRec.GetUsePrompt(), "prompt1Empty", menuRec.Prompt1 == "")
			}

			// Read User Input Line via shared InputHandler to avoid reader races.
			input, err := readLineFromSessionIH(s, terminal)
			if err != nil {
				if errors.Is(err, io.EOF) {
					slog.Info("user disconnected during menu input", "menu", currentMenuName)
					return "LOGOFF", nil, nil
				}
				if errors.Is(err, editor.ErrIdleTimeout) {
					e.handleIdleTimeout(terminal, outputMode, nodeNumber, termHeight)
					return "LOGOFF", nil, nil
				}
				slog.Error("failed to read input for menu", "menu", currentMenuName, "error", err)
				return "", nil, fmt.Errorf("failed reading input: %w", err)
			}
			userInput = strings.ToUpper(strings.TrimSpace(input))
			slog.Debug("user input", "input", userInput)

			// --- Special Input Handling (^P, ##) ---
			if userInput == "\x10" || userInput == "^P" { // Ctrl+P is ASCII 16 (\x10)
				if previousMenuName != "" {
					slog.Debug("user entered ^P, going back to previous menu", "previous", previousMenuName)
					temp := currentMenuName
					currentMenuName = previousMenuName
					previousMenuName = temp // Update previous in case they go back again
					continue                // Go directly to the previous menu loop iteration
				} else {
					slog.Debug("user entered ^P, but no previous menu recorded")
					continue // Re-display current menu prompt
				}
			}

			// --- End Special Input Handling ---
		} // End if isLightbarMenu / else

		// 6. Process Input / Find Command Match (userInput determined by menu type)
		matched := false
		nextAction := ""          // Store the action determined by the matched command
		matchedNodeActivity := "" // Store matched command's node activity

		// Global hangup shortcut: /G
		if userInput == "/G" {
			nextAction = "RUN:IMMEDIATELOGOFF"
			matched = true
		}

		if !matched { // Check keyword matches (relevant for both)
			for _, cmdRec := range commands {
				// Hidden commands are still matched (e.g. % for sponsor menu); HIDDEN only affects display/prompts.

				cmdACS := cmdRec.ACS
				if !checkACS(cmdACS, currentUser, s, terminal, sessionStartTime) { // Use ssh.Session 's'
					if currentUser != nil {
						slog.Debug("user does not meet ACS for command keys", "handle", currentUser.Handle, "acs", cmdACS, "keys", cmdRec.Keys)
					} else {
						slog.Debug("unauthenticated user does not meet ACS for command keys", "acs", cmdACS, "keys", cmdRec.Keys)
					}
					continue // Skip this command if ACS check fails
				}

				keys := strings.Split(cmdRec.Keys, " ") // Use string directly
				for _, key := range keys {
					// ^M matches when user presses Enter with no input (classic BBS default command)
					if key == "^M" && userInput == "" {
						nextAction = cmdRec.Command
						matchedNodeActivity = cmdRec.NodeActivity
						slog.Debug("matched ^M (enter/default) to command action", "command", nextAction)
						matched = true
						break
					}
					// Standard exact key match
					if key != "" && userInput != "" && userInput == key {
						nextAction = cmdRec.Command
						matchedNodeActivity = cmdRec.NodeActivity
						slog.Debug("matched key to command action", "key", key, "command", nextAction)
						matched = true
						break
					}
					// ## matches any numeric input (classic BBS numeric wildcard)
					if key == "##" && userInput != "" {
						isNumeric := true
						for _, ch := range userInput {
							if ch < '0' || ch > '9' {
								isNumeric = false
								break
							}
						}
						if isNumeric {
							// Append the entered number as args so executeCommandAction
							// forwards it to the RUN: handler via runArgs.
							nextAction = cmdRec.Command + " " + userInput
							matchedNodeActivity = cmdRec.NodeActivity
							slog.Debug("matched ## numeric wildcard to command action", "input", userInput, "command", nextAction)
							matched = true
							break
						}
					}
				}
				if matched {
					break // Break outer command loop
				}
			}
		}

		// 7. Handle Action or No Match
		if matched {
			// Update session activity before executing command
			if matchedNodeActivity != "" {
				if sess := e.SessionRegistry.Get(nodeNumber); sess != nil {
					sess.Mutex.Lock()
					sess.Activity = matchedNodeActivity
					sess.Mutex.Unlock()
				}
			}

			// Execute the determined action here
			nextActionType, nextMenuName, userResult, err := e.executeCommandAction(nextAction, s, terminal, userManager, currentUser, nodeNumber, sessionStartTime, outputMode, termWidth, termHeight)
			if err != nil {
				return "", userResult, err
			}
			if nextActionType == "GOTO" {
				previousMenuName = currentMenuName // Store current before going to next
				currentMenuName = nextMenuName
				continue // Continue main loop to the new menu
			} else if nextActionType == "LOGOFF" {
				return "LOGOFF", userResult, nil // Return specific logoff action
			} else if nextActionType == "CONTINUE" {
				// Reset activity to menu default after command completes
				if sess := e.SessionRegistry.Get(nodeNumber); sess != nil {
					sess.Mutex.Lock()
					sess.Activity = menuDefaultActivity
					sess.Mutex.Unlock()
				}
				if userResult != nil {
					currentUser = userResult
				}
				continue // Re-display current menu prompt
			}
			slog.Warn("unhandled action type after executing command", "actionType", nextActionType, "command", nextAction)
			continue
		} else {
			slog.Debug("input did not match any commands in menu", "input", userInput, "menu", currentMenuName)

			// If it was a lightbar menu and input was ignored (userInput == ""), just loop again
			if isLightbarMenu {
				continue
			}

			// Empty Enter should just redisplay the current menu, not fall through to fallback
			if userInput == "" {
				continue
			}

			fallbackMenu := menuRec.Fallback
			if fallbackMenu != "" {
				slog.Info("no command match, using fallback menu", "menu", fallbackMenu)
				previousMenuName = currentMenuName // Store current before going to fallback
				currentMenuName = strings.ToUpper(fallbackMenu)
				continue
			}
			e.showUndefinedMenuInput(terminal, outputMode, nodeNumber)
			continue // Redisplay current menu
		}
	}
}

// executeCommandAction handles the logic for executing a command string (GOTO, RUN, DOOR, LOGOFF).
// Returns: actionType (GOTO, LOGOFF, CONTINUE), nextMenu, resultingUser, error
func (e *MenuExecutor) executeCommandAction(action string, s ssh.Session, terminal *term.Terminal, userManager *user.UserMgr, currentUser *user.User, nodeNumber int, sessionStartTime time.Time, outputMode ansi.OutputMode, termWidth int, termHeight int) (actionType string, nextMenu string, userResult *user.User, err error) {
	if strings.HasPrefix(action, "GOTO:") {
		nextMenu = strings.ToUpper(strings.TrimPrefix(action, "GOTO:"))
		return "GOTO", nextMenu, currentUser, nil
	} else if action == "LOGOFF" {
		return "LOGOFF", "", currentUser, nil
	} else if strings.HasPrefix(action, "RUN:") {
		parts := strings.SplitN(strings.TrimPrefix(action, "RUN:"), " ", 2)
		runTarget := strings.ToUpper(parts[0])
		var runArgs string
		if len(parts) > 1 {
			runArgs = parts[1]
		}
		slog.Info("executing RUN action", "target", runTarget, "args", runArgs)

		if runnableFunc, exists := e.RunRegistry[runTarget]; exists {
			slog.Debug("calling registered function for RUN", "node", nodeNumber, "target", runTarget)
			// RunnableFunc now returns user, nextActionString, error
			authUser, nextActionStr, runErr := runnableFunc(&cmdCtx{e: e, s: s, terminal: terminal, userManager: userManager, currentUser: currentUser, nodeNumber: nodeNumber, sessionStartTime: sessionStartTime, outputMode: outputMode, termWidth: termWidth, termHeight: termHeight}, runArgs)
			if runErr != nil {
				if errors.Is(runErr, io.EOF) {
					slog.Info("user disconnected during RUN execution", "node", nodeNumber, "target", runTarget)
					return "LOGOFF", "", nil, nil
				}
				if errors.Is(runErr, editor.ErrIdleTimeout) {
					e.handleIdleTimeout(terminal, outputMode, nodeNumber, termHeight)
					return "LOGOFF", "", nil, nil
				}
				slog.Error("RUN function failed", "target", runTarget, "error", runErr)
				errMsg := fmt.Sprintf(e.LoadedStrings.ExecRunCommandError, runTarget, runErr)
				wErr := terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(errMsg)), outputMode)
				if wErr != nil {
					slog.Error("failed writing RUN command error message", "error", wErr)
				}
				time.Sleep(1 * time.Second)
				// Assign the potentially updated user before returning
				userResult = authUser                     // Capture potential user changes (like from AUTHENTICATE)
				return "CONTINUE", "", userResult, runErr // Continue but report error?
			}
			slog.Debug("RUN function completed", "target", runTarget)

			// Check if the runnable function returned a specific next action
			if strings.HasPrefix(nextActionStr, "GOTO:") {
				nextMenu = strings.ToUpper(strings.TrimPrefix(nextActionStr, "GOTO:"))
				slog.Debug("RUN requested GOTO", "target", runTarget, "menu", nextMenu)
				return "GOTO", nextMenu, authUser, nil
			} else if nextActionStr == "LOGOFF" {
				slog.Debug("RUN requested LOGOFF", "target", runTarget)
				return "LOGOFF", "", authUser, nil
			}

			// Default action for RUN is CONTINUE
			return "CONTINUE", "", authUser, nil
		} else {
			slog.Warn("no internal function registered for RUN", "target", runTarget)
			msg := fmt.Sprintf(e.LoadedStrings.ExecRunCommandNotFound, runTarget)
			wErr := terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
			if wErr != nil {
				slog.Error("failed writing missing RUN command message", "error", wErr)
			}
			time.Sleep(1 * time.Second)
			return "CONTINUE", "", currentUser, nil
		}
	} else if strings.HasPrefix(action, "DOOR:") {
		doorTarget := strings.TrimPrefix(action, "DOOR:")
		slog.Info("executing DOOR action", "door", doorTarget)
		if doorFunc, exists := e.RunRegistry["DOOR:"]; exists {
			// DOOR runnable returns user, "", error
			userResultDoor, nextActionStrDoor, doorErr := doorFunc(&cmdCtx{e: e, s: s, terminal: terminal, userManager: userManager, currentUser: currentUser, nodeNumber: nodeNumber, sessionStartTime: sessionStartTime, outputMode: outputMode, termWidth: termWidth, termHeight: termHeight}, doorTarget)
			if doorErr != nil {
				if errors.Is(doorErr, io.EOF) {
					slog.Info("user disconnected during DOOR execution", "node", nodeNumber, "door", doorTarget)
					return "LOGOFF", "", nil, nil
				}
				slog.Error("DOOR execution failed", "door", doorTarget, "error", doorErr)
				errMsg := fmt.Sprintf(e.LoadedStrings.ExecRunDoorError, doorTarget, doorErr)
				wErr := terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(errMsg)), outputMode)
				if wErr != nil {
					slog.Error("failed writing DOOR command error message", "error", wErr)
				}
				time.Sleep(1 * time.Second)
				// Assign potential user result before returning
				userResult = userResultDoor
				return "CONTINUE", "", userResult, doorErr // Continue after door error?
			}
			// Handle potential LOGOFF request from DOOR runnable (though currently returns "")
			if nextActionStrDoor == "LOGOFF" {
				slog.Debug("DOOR requested LOGOFF", "door", doorTarget)
				return "LOGOFF", "", userResultDoor, nil
			}
			slog.Debug("DOOR completed", "door", doorTarget)
			return "CONTINUE", "", userResultDoor, nil // Default CONTINUE after door
		} else {
			slog.Error("DOOR function not registered")
			return "CONTINUE", "", currentUser, nil
		}
	} else {
		slog.Warn("unhandled command action type in executeCommandAction", "action", action)
		return "CONTINUE", "", currentUser, nil
	}
}

func (e *MenuExecutor) resolveCurrentAreaTokens(currentUser *user.User, currentAreaName string) (string, string) {
	areaTag := "None"
	areaName := strings.TrimSpace(currentAreaName)

	if currentUser == nil {
		if areaName == "" {
			areaName = "None"
		}
		return areaTag, areaName
	}

	if currentUser.CurrentMessageAreaTag != "" {
		areaTag = currentUser.CurrentMessageAreaTag
	}
	if e.MessageMgr != nil && currentUser.CurrentMessageAreaID > 0 {
		if area, found := e.MessageMgr.GetAreaByID(currentUser.CurrentMessageAreaID); found {
			if strings.TrimSpace(area.Tag) != "" {
				areaTag = area.Tag
			}
			if strings.TrimSpace(area.Name) != "" {
				areaName = area.Name
			}
		}
	}

	if areaName == "" {
		areaName = "None"
	}
	return areaTag, areaName
}

// resolveFileConferencePath returns "Conference > File Area Name" as plain text.
// Colors should be applied in the template surrounding the placeholder.
func (e *MenuExecutor) resolveFileConferencePath(currentUser *user.User) string {
	confName := "Local"
	areaName := "None"
	if currentUser == nil || e.FileMgr == nil {
		return confName + " > " + areaName
	}
	if currentUser.CurrentFileAreaID > 0 {
		if area, found := e.FileMgr.GetAreaByID(currentUser.CurrentFileAreaID); found {
			if strings.TrimSpace(area.Name) != "" {
				areaName = area.Name
			}
			if area.ConferenceID != 0 && e.ConferenceMgr != nil {
				if conf, found := e.ConferenceMgr.GetByID(area.ConferenceID); found && strings.TrimSpace(conf.Name) != "" {
					confName = conf.Name
				}
			}
		}
	}
	return confName + " > " + areaName
}

// resolveCurrentFileAreaTokens returns the tag and display name for the user's
// current file area. If no file area is set or the area cannot be found, it
// returns "None" for both values.
func (e *MenuExecutor) resolveCurrentFileAreaTokens(currentUser *user.User) (string, string) {
	areaTag := "None"
	areaName := "None"

	if currentUser == nil {
		return areaTag, areaName
	}

	if currentUser.CurrentFileAreaTag != "" {
		areaTag = currentUser.CurrentFileAreaTag
	}
	if e.FileMgr != nil && currentUser.CurrentFileAreaID > 0 {
		if area, found := e.FileMgr.GetAreaByID(currentUser.CurrentFileAreaID); found {
			if strings.TrimSpace(area.Tag) != "" {
				areaTag = area.Tag
			}
			if strings.TrimSpace(area.Name) != "" {
				areaName = area.Name
			}
		}
	}

	return areaTag, areaName
}

// applyCommonTemplateTokens replaces pipe-style tokens (|CFAN, |CFA, |CAN, |CA,
// |UH, |NODE, |DATE, |TIME, etc.) in template bytes before ANSI pipe-code
// processing.  This mirrors the substitution that displayMenuPrompt performs so
// templates behave consistently with prompts.  Longer tokens are replaced first
// to avoid prefix collisions (e.g. |CFAN before |CFA, |CAN before |CA).
func (e *MenuExecutor) applyCommonTemplateTokens(data []byte, currentUser *user.User, nodeNumber int) []byte {
	now := config.NowIn(e.ServerCfg.Timezone)
	fileAreaTag, fileAreaName := e.resolveCurrentFileAreaTokens(currentUser)
	msgAreaTag, msgAreaName := e.resolveCurrentAreaTokens(currentUser, "")

	tokens := map[string]string{
		"|NODE":      strconv.Itoa(nodeNumber),
		"|DATE":      now.Format("01/02/06"),
		"|TIME":      now.Format("3:04 pm"),
		"|CA":        msgAreaTag,
		"|CAN":       msgAreaName,
		"|CFA":       fileAreaTag,
		"|CFAN":      fileAreaName,
		"|FCONFPATH": e.resolveFileConferencePath(currentUser),
		"|UH":        "Guest",
		"|ALIAS":     "Guest",
		"|HANDLE":    "Guest",
		"|LEVEL":     "0",
		"|CC":        "None",
		"|CCN":       "None",
		"|FC":        "None",
		"|FCN":       "None",
	}
	if currentUser != nil {
		tokens["|UH"] = currentUser.Handle
		tokens["|ALIAS"] = currentUser.Handle
		tokens["|HANDLE"] = currentUser.Handle
		tokens["|LEVEL"] = strconv.Itoa(currentUser.AccessLevel)
		if currentUser.CurrentMsgConferenceTag != "" {
			tokens["|CC"] = currentUser.CurrentMsgConferenceTag
		}
		if currentUser.CurrentFileConferenceTag != "" {
			tokens["|FC"] = currentUser.CurrentFileConferenceTag
		}
		if e.ConferenceMgr != nil {
			if currentUser.CurrentMsgConferenceID != 0 {
				if conf, ok := e.ConferenceMgr.GetByID(currentUser.CurrentMsgConferenceID); ok {
					tokens["|CCN"] = conf.Name
					if tokens["|CC"] == "None" {
						tokens["|CC"] = conf.Tag
					}
				}
			}
			if currentUser.CurrentFileConferenceID != 0 {
				if conf, ok := e.ConferenceMgr.GetByID(currentUser.CurrentFileConferenceID); ok {
					tokens["|FCN"] = conf.Name
					if tokens["|FC"] == "None" {
						tokens["|FC"] = conf.Tag
					}
				}
			}
		}
	}

	// Sort longest-key-first so |CFAN is replaced before |CFA, etc.
	keys := make([]string, 0, len(tokens))
	for k := range tokens {
		keys = append(keys, k)
	}
	sort.SliceStable(keys, func(i, j int) bool { return len(keys[i]) > len(keys[j]) })
	pairs := make([]string, 0, len(tokens)*2)
	for _, k := range keys {
		pairs = append(pairs, k, tokens[k])
	}
	return []byte(strings.NewReplacer(pairs...).Replace(string(data)))
}

// displayFile reads and displays an ANSI file from the MENU SET's ansi directory.
// If clearFirst is true, prepends the ANSI clear sequence so clear and content go out in one write.
func (e *MenuExecutor) displayFile(terminal *term.Terminal, filename string, outputMode ansi.OutputMode, clearFirst ...bool) error {
	// Construct full path using MenuSetPath
	filePath := filepath.Join(e.MenuSetPath, "ansi", filename)

	// Read ANSI content via helper (strips SAUCE metadata)
	data, err := ansi.GetAnsiFileContent(filePath)
	if err != nil {
		slog.Error("failed to read ANSI file", "path", filePath, "error", err)
		errMsg := fmt.Sprintf(e.LoadedStrings.ExecFileLoadError, filename)
		writeErr := terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(errMsg)), outputMode)
		if writeErr != nil {
			slog.Error("failed writing displayFile error message", "error", writeErr)
			return fmt.Errorf("read: %w; write error: %w", err, writeErr)
		}
		return err
	}
	if len(clearFirst) > 0 && clearFirst[0] {
		data = append([]byte(ansi.ClearScreen()), data...)
	}

	// Expand AT-codes before pipe code processing.
	// Use level 1 (default MinLevel) since displayFile lacks user context.
	data = expandRandomRumorATCode(data, e.RootConfigPath, 1)

	// Process pipe codes before output — ANSI escape sequences produced are
	// ASCII-safe and work correctly in both CP437 and UTF-8 output modes.
	data = ansi.ReplacePipeCodes(data)

	// For CP437 mode, write raw bytes directly to avoid UTF-8 false positives
	var writeErr error
	if outputMode == ansi.OutputModeCP437 {
		_, writeErr = terminal.Write(data)
	} else {
		writeErr = terminalio.WriteProcessedBytes(terminal, data, outputMode)
	}
	if writeErr != nil {
		slog.Error("failed to write ANSI file", "path", filePath, "error", writeErr)
		return writeErr
	}

	return nil
}

// deliverPendingPages checks for and displays any queued page messages.
func (e *MenuExecutor) deliverPendingPages(terminal *term.Terminal, nodeNumber int, outputMode ansi.OutputMode) {
	if e.SessionRegistry == nil {
		return
	}
	sess := e.SessionRegistry.Get(nodeNumber)
	if sess == nil {
		return
	}
	pages := sess.DrainPages()
	for _, page := range pages {
		if err := terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte("\r\n"+page+"\r\n")), outputMode); err != nil {
			slog.Error("failed to deliver page", "node", nodeNumber, "error", err)
			return
		}
	}
}

// displayPrompt handles rendering the menu prompt, including file includes and placeholder substitution.
// Added currentAreaName parameter
func (e *MenuExecutor) displayPrompt(terminal *term.Terminal, menu *MenuRecord, currentUser *user.User, userManager *user.UserMgr, nodeNumber int, currentMenuName string, sessionStartTime time.Time, outputMode ansi.OutputMode, currentAreaName string) error {
	promptParts := make([]string, 0, 2)
	if strings.TrimSpace(menu.Prompt1) != "" {
		promptParts = append(promptParts, menu.Prompt1)
	}
	if strings.TrimSpace(menu.Prompt2) != "" {
		promptParts = append(promptParts, menu.Prompt2)
	}

	if currentMenuName == "MAIN" {
		isAdmin := currentUser != nil && currentUser.AccessLevel >= 100
		pendingCount := pendingValidationCount(userManager)
		showValidationLine := isAdmin && pendingCount > 0
		if !showValidationLine {
			filtered := make([]string, 0, len(promptParts))
			for _, part := range promptParts {
				if strings.Contains(part, "|PV") {
					continue
				}
				filtered = append(filtered, part)
			}
			promptParts = filtered
		}
	}

	promptString := strings.Join(promptParts, "\r\n")

	if promptString == "" {
		if e.LoadedStrings.DefPrompt != "" { // Use loaded strings
			promptString = e.LoadedStrings.DefPrompt
		} else {
			slog.Warn("default prompt empty and menu prompt fields empty, no prompt will be displayed", "menu", currentMenuName)
			return nil // Explicitly return nil if no prompt string can be determined
		}
	}

	slog.Debug("displaying menu prompt", "menu", currentMenuName)

	newUsersStatus := "NO"
	if e.GetServerConfig().AllowNewUsers {
		newUsersStatus = "YES"
	}

	now := config.NowIn(e.ServerCfg.Timezone)
	currentAreaTag, currentAreaDisplayName := e.resolveCurrentAreaTokens(currentUser, currentAreaName)
	currentFileAreaTag, currentFileAreaDisplayName := e.resolveCurrentFileAreaTokens(currentUser)

	placeholders := map[string]string{
		"|NODE":     strconv.Itoa(nodeNumber), // Node Number
		"|DATE":     now.Format("01/02/06"),
		"|TIME":     now.Format("3:04 pm"),
		"|MN":       currentMenuName,            // Menu Name
		"|PV":       "0",                        // Pending validations
		"|UH":       "Guest",                    // User Handle
		"|NEWUSERS": newUsersStatus,             // Allow new users (YES/NO)
		"|ALIAS":    "Guest",                    // Default
		"|HANDLE":   "Guest",                    // Default
		"|LEVEL":    "0",                        // Default
		"|NAME":     "Guest User",               // Default
		"|GL":       "",                         // Group/Location default
		"|UN":       "",                         // User note (privateNote) default
		"|UPLDS":    "0",                        // Default
		"|DNLDS":    "0",                        // Default
		"|POSTS":    "0",                        // Default
		"|CALLS":    "0",                        // Default
		"|LCALL":    "Never",                    // Default
		"|TL":       "N/A",                      // Default
		"|CA":       currentAreaTag,             // Current message area tag
		"|CAN":      currentAreaDisplayName,     // Current message area display name
		"|CFA":      currentFileAreaTag,         // Current file area tag
		"|CFAN":     currentFileAreaDisplayName, // Current file area display name
		"|CC":       "None",                     // Current message conference tag default
		"|CCN":      "None",                     // Current message conference name default
		"|FC":       "None",                     // Current file conference tag default
		"|FCN":      "None",                     // Current file conference name default
	}

	// Populate user-specific placeholders if logged in
	if currentUser != nil {
		placeholders["|UH"] = currentUser.Handle
		placeholders["|ALIAS"] = currentUser.Handle
		placeholders["|HANDLE"] = currentUser.Handle
		placeholders["|LEVEL"] = strconv.Itoa(currentUser.AccessLevel)
		placeholders["|NAME"] = currentUser.RealName
		placeholders["|GL"] = currentUser.GroupLocation
		placeholders["|UN"] = currentUser.PrivateNote
		placeholders["|UPLDS"] = strconv.Itoa(currentUser.NumUploads)
		placeholders["|CALLS"] = strconv.Itoa(currentUser.TimesCalled)
		if !currentUser.LastLogin.IsZero() {
			placeholders["|LCALL"] = currentUser.LastLogin.Format("01/02/06")
		}

		// Set |CC/|CCN based on user's current message conference
		if currentUser.CurrentMsgConferenceTag != "" {
			placeholders["|CC"] = currentUser.CurrentMsgConferenceTag
		}
		if e.ConferenceMgr != nil && currentUser.CurrentMsgConferenceID != 0 {
			if conf, ok := e.ConferenceMgr.GetByID(currentUser.CurrentMsgConferenceID); ok {
				placeholders["|CCN"] = conf.Name
			}
		}

		// Set |FC/|FCN based on user's current file conference
		if currentUser.CurrentFileConferenceTag != "" {
			placeholders["|FC"] = currentUser.CurrentFileConferenceTag
		}
		if e.ConferenceMgr != nil && currentUser.CurrentFileConferenceID != 0 {
			if conf, ok := e.ConferenceMgr.GetByID(currentUser.CurrentFileConferenceID); ok {
				placeholders["|FCN"] = conf.Name
			}
		}

		// Calculate Time Left |TL
		if currentUser.TimeLimit <= 0 {
			placeholders["|TL"] = "Unlimited"
		} else {
			elapsedSeconds := time.Since(sessionStartTime).Seconds()
			totalSeconds := float64(currentUser.TimeLimit * 60)
			remainingSeconds := totalSeconds - elapsedSeconds
			if remainingSeconds < 0 {
				remainingSeconds = 0
			}
			remainingMinutes := int(remainingSeconds / 60)
			placeholders["|TL"] = strconv.Itoa(remainingMinutes)
		}

		if currentMenuName == "MAIN" && currentUser.AccessLevel >= 100 {
			placeholders["|PV"] = strconv.Itoa(pendingValidationCount(userManager))
		}
	} // End if currentUser != nil

	// Replace longer placeholders before shorter ones to avoid prefix collisions (e.g. |CAN vs |CA).
	replacementPairs := make([]string, 0, len(placeholders)*2)
	orderedKeys := make([]string, 0, len(placeholders))
	for key := range placeholders {
		orderedKeys = append(orderedKeys, key)
	}
	sort.SliceStable(orderedKeys, func(i, j int) bool {
		return len(orderedKeys[i]) > len(orderedKeys[j])
	})
	for _, key := range orderedKeys {
		replacementPairs = append(replacementPairs, key, placeholders[key])
	}
	substitutedPrompt := strings.NewReplacer(replacementPairs...).Replace(promptString)

	// Replace @CODE@ AT-codes with width support (@UC@, @UC:5@, @UC##@, @U@, etc.)
	promptBytes := replaceMenuATCode([]byte(substitutedPrompt), "UC", strconv.Itoa(userManager.GetUserCount()))
	promptBytes = replaceMenuATCode(promptBytes, "U", strconv.Itoa(e.SessionRegistry.ActiveCount()))
	substitutedPrompt = string(promptBytes)

	processedPrompt, err := e.processFileIncludes(substitutedPrompt, 0) // Pass 'e'
	if err != nil {
		slog.Error("failed processing file includes in prompt", "menu", currentMenuName, "error", err)

		// Use RootAssetsPath for global assets if needed, or MenuSetPath for set-specific
		// pausePrompt := e.LoadedStrings.PauseString // This comes from global strings
		// ... (rest of pause logic) ...
		return err // Use original error if includes fail
	}

	// 2b. Expand @RR@ after file includes so %%file.ans%% content is also processed.
	rumorLevel := 1 // default MinLevel when no user context
	if currentUser != nil {
		rumorLevel = currentUser.AccessLevel
	}
	processedPromptBytes := expandRandomRumorATCode([]byte(processedPrompt), e.RootConfigPath, rumorLevel)

	// 3. Process pipe codes in the final string (includes/placeholders already processed)
	rawPromptBytes := ansi.ReplacePipeCodes(processedPromptBytes)

	// 4. Process character encoding based on outputMode (Reverted to manual loop)
	var finalBuf bytes.Buffer
	finalBuf.Write([]byte("\r\n")) // Add newline prefix

	for i := 0; i < len(rawPromptBytes); i++ {
		b := rawPromptBytes[i]
		if b < 128 || outputMode == ansi.OutputModeCP437 {
			// ASCII or CP437 mode, write raw byte
			finalBuf.WriteByte(b)
		} else {
			// UTF-8 mode, convert extended characters
			r := ansi.Cp437ToUnicode[b] // Use the exported map
			if r == 0 && b != 0 {
				finalBuf.WriteByte('?') // Fallback
			} else if r != 0 {
				finalBuf.WriteRune(r)
			}
		}
	}

	// 5. Write the final processed bytes using the terminal's standard Write (Reverted)
	err = terminalio.WriteProcessedBytes(terminal, finalBuf.Bytes(), outputMode)
	if err != nil {
		slog.Error("failed writing processed prompt", "menu", currentMenuName, "error", err)
		return err
	}

	return nil
}

// includeTagRe matches %%filename.ext%% include tags. Package-level so menu
// renders don't recompile it on every call and recursion level.
var includeTagRe = regexp.MustCompile(`%%([a-zA-Z0-9_\-]+\.[a-zA-Z0-9]+)%%`)

// processFileIncludes recursively replaces %%filename.ans tags with file content.
// It now looks for included files within the MENU SET's ansi directory.
func (e *MenuExecutor) processFileIncludes(prompt string, depth int) (string, error) {
	const maxDepth = 5 // Limit recursion depth
	if depth > maxDepth {
		slog.Warn("exceeded maximum file inclusion depth, stopping processing", "maxDepth", maxDepth)
		return prompt, nil
	}

	processedAny := false
	result := includeTagRe.ReplaceAllStringFunc(prompt, func(match string) string {
		processedAny = true
		// match is "%%name.ext%%"; strip the delimiters instead of re-matching.
		fileName := strings.TrimSuffix(strings.TrimPrefix(match, "%%"), "%%")
		// Look for included file in MenuSetPath/ansi
		filePath := filepath.Join(e.MenuSetPath, "ansi", fileName)

		slog.Debug("including file in prompt", "path", filePath, "depth", depth)
		data, err := os.ReadFile(filePath)
		if err != nil {
			slog.Warn("failed to read included file, skipping inclusion", "path", filePath, "error", err)
			return ""
		}
		return string(data)
	})

	if processedAny {
		return e.processFileIncludes(result, depth+1)
	}

	return result, nil
}

// Define needed ANSI attributes
const (
	attrInverse = "\x1b[7m" // Inverse video - Keep for fallback?
	attrReset   = "\x1b[0m" // Reset attributes
)

// LightbarOption represents a single option in a lightbar menu
type LightbarOption struct {
	X, Y           int    // Screen coordinates
	Text           string // Display text
	HotKey         string // Command hotkey
	ReturnValue    string // Return value (often same as hotkey, but can differ)
	HighlightColor int    // Color code when highlighted
	RegularColor   int    // Color code when not highlighted
}

// ANSI foreground color codes (standard and bright)
var ansiFg = map[int]int{
	0: 30, 1: 34, 2: 32, 3: 36, 4: 31, 5: 35, 6: 33, 7: 37, // Standard
	8: 90, 9: 94, 10: 92, 11: 96, 12: 91, 13: 95, 14: 93, 15: 97, // Bright
}

// ANSI background color codes (standard, non-bright)
var ansiBg = map[int]int{
	0: 40, 1: 44, 2: 42, 3: 46, 4: 41, 5: 45, 6: 43, 7: 47,
	// Note: 40-47 are standard (darker) backgrounds
	// 100-107 would be bright backgrounds (less terminal support)
}

// runShowVersion displays configured version information.
func runShowVersion(c *cmdCtx, args string) (*user.User, string, error) {
	e := c.e
	s := c.s
	terminal := c.terminal
	nodeNumber := c.nodeNumber
	outputMode := c.outputMode
	termWidth := c.termWidth
	termHeight := c.termHeight

	slog.Debug("running showversion", "node", nodeNumber)

	versionTemplate := e.LoadedStrings.ExecVersionString
	versionString := versionTemplate
	if strings.Contains(versionTemplate, "%s") {
		versionString = fmt.Sprintf(versionTemplate, version.Number)
	}

	// Display the version
	terminalio.WriteProcessedBytes(terminal, []byte(ansi.ClearScreen()), outputMode) // Optional: Clear screen
	terminalio.WriteProcessedBytes(terminal, []byte("\r\n\r\n"), outputMode)         // Add some spacing
	wErr := terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(versionString)), outputMode)
	if wErr != nil {
		slog.Error("failed writing showversion output", "node", nodeNumber, "error", wErr)
		// Don't return error, just log it
	}

	// Wait for Enter
	pausePrompt := e.LoadedStrings.PauseString // Use configured pause string
	if pausePrompt == "" {
		slog.Warn("pausestring empty, no pause prompt will be shown for showversion", "node", nodeNumber)
		// Don't use a hardcoded fallback. If it's empty, it's empty.
	} else {
		terminalio.WriteProcessedBytes(terminal, []byte("\r\n"), outputMode) // Add newline before pause
		slog.Debug("displaying showversion pause prompt (centered)", "node", nodeNumber)
		err := writeCenteredPausePrompt(s, terminal, pausePrompt, outputMode, termWidth, termHeight)
		if err != nil {
			if errors.Is(err, io.EOF) {
				slog.Info("user disconnected during showversion pause", "node", nodeNumber)
				return nil, "LOGOFF", io.EOF
			}
			slog.Error("failed during showversion pause", "node", nodeNumber, "error", err)
			return nil, "", err
		}
	}

	return nil, "", nil // Return to the current menu
}

// errInputAborted is returned by styledInput when the user presses ESC to cancel entry.
var errInputAborted = errors.New("input aborted")
