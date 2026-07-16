package menu

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/conference"
	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/file"
	"github.com/ViSiON-3/vision-3-bbs/internal/message"
	"github.com/ViSiON-3/vision-3-bbs/internal/session"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
	"github.com/ViSiON-3/vision-3-bbs/internal/transfer"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
	"github.com/gliderlabs/ssh"
	"golang.org/x/term"
)

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
