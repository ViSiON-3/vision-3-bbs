package menu

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"sync/atomic"
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
	OneLiners       []string                      // Loaded oneliners (Consider if these should be menu-set specific)
	MessageMgr      *message.MessageManager       // <-- ADDED FIELD
	FileMgr         *file.FileManager             // <-- ADDED FIELD: File manager instance
	ConferenceMgr   *conference.ConferenceManager // Conference grouping manager
	IPLockoutCheck  IPLockoutChecker              // IP-based authentication lockout checker
	SessionRegistry *session.SessionRegistry      // Session registry for who's online
	ChatLeaves      ChatLeafProvider              // V3Net chat leaf provider (nil = local only)
	V3NetReload     func() error                  // Applies v3net.json subscription changes live (nil = restart required)
	V3NetStatus     V3NetStatusProvider           // V3Net service status (nil if disabled)

	// Hot-reloadable configuration.
	//
	// Each of these is replaced wholesale when its file is re-read, while
	// sessions are actively reading it. They are held as atomic pointers to
	// immutable snapshots rather than as plain fields: a reload stores a
	// freshly loaded value and readers keep whatever snapshot they loaded, so
	// no reader ever observes a half-updated config.
	//
	// A mutex would not work here in practice. There are several hundred read
	// sites, and every one of them would have to take it — a single missed
	// read is a data race, and the compiler cannot catch one. Snapshots make
	// the safe form the only form the accessors offer.
	//
	// The struct-valued snapshots (strings, theme, server config) are shared
	// by every reader that loaded them and must be treated as read-only. The
	// map- and slice-valued ones (doors, login sequence, protocols) are
	// cloned at both the setter and the collection accessor, so no caller
	// ever holds a reference into a shared snapshot; GetDoorConfig reads the
	// shared map without cloning, which is safe because it returns a value.
	stringsCfg atomic.Pointer[config.StringsConfig]
	themeCfg   atomic.Pointer[config.ThemeConfig]
	serverCfg  atomic.Pointer[config.ServerConfig]
	doorReg    atomic.Pointer[map[string]config.DoorConfig]
	loginSeq   atomic.Pointer[[]config.LoginItem]
	protocols  atomic.Pointer[[]transfer.ProtocolConfig]
}

// NewExecutor creates a new MenuExecutor.
func NewExecutor(menuSetPath, rootConfigPath, rootAssetsPath string, oneLiners []string, doorRegistry map[string]config.DoorConfig, loadedStrings config.StringsConfig, theme config.ThemeConfig, serverCfg config.ServerConfig, msgMgr *message.MessageManager, fileMgr *file.FileManager, confMgr *conference.ConferenceManager, ipLockoutCheck IPLockoutChecker, loginSequence []config.LoginItem, sessionRegistry *session.SessionRegistry, protocols []transfer.ProtocolConfig) *MenuExecutor {

	// Initialize the run registry
	runRegistry := make(map[string]RunnableFunc) // Use local RunnableFunc
	registerPlaceholderRunnables(runRegistry)    // Add placeholder registrations
	registerAppRunnables(runRegistry)            // Add application-specific runnables

	e := &MenuExecutor{
		MenuSetPath:     menuSetPath,
		RootConfigPath:  rootConfigPath,
		RootAssetsPath:  rootAssetsPath,
		RunRegistry:     runRegistry,
		OneLiners:       oneLiners,
		MessageMgr:      msgMgr,
		FileMgr:         fileMgr,
		ConferenceMgr:   confMgr,
		IPLockoutCheck:  ipLockoutCheck,
		SessionRegistry: sessionRegistry,
	}
	e.SetDoorRegistry(doorRegistry)
	e.SetStrings(loadedStrings)
	e.SetTheme(theme)
	e.SetServerConfig(serverCfg)
	e.SetLoginSequence(loginSequence)
	e.SetProtocols(protocols)
	return e
}

// --- Hot Reload Methods ---
//
// Each setter stores a new immutable snapshot; each accessor loads whatever
// snapshot is current. Readers must not mutate what an accessor returns.

// SetDoorRegistry atomically updates the door registry. The map is cloned on
// store, so the caller keeping (and mutating) its own reference cannot reach
// the shared snapshot.
func (e *MenuExecutor) SetDoorRegistry(doors map[string]config.DoorConfig) {
	cloned := make(map[string]config.DoorConfig, len(doors))
	for k, v := range doors {
		cloned[k] = v
	}
	e.doorReg.Store(&cloned)
}

// DoorRegistry returns a copy of the current door registry, safe for the
// caller to hold or mutate. Per-door lookups should use GetDoorConfig, which
// reads the shared snapshot without copying.
func (e *MenuExecutor) DoorRegistry() map[string]config.DoorConfig {
	p := e.doorReg.Load()
	if p == nil {
		return nil
	}
	cloned := make(map[string]config.DoorConfig, len(*p))
	for k, v := range *p {
		cloned[k] = v
	}
	return cloned
}

// GetDoorConfig atomically retrieves a door configuration.
func (e *MenuExecutor) GetDoorConfig(name string) (config.DoorConfig, bool) {
	cfg, ok := e.DoorRegistry()[name]
	return cfg, ok
}

// SetLoginSequence atomically updates the login sequence. The slice is
// cloned on store, so the caller keeping its own reference cannot reach the
// shared snapshot.
func (e *MenuExecutor) SetLoginSequence(sequence []config.LoginItem) {
	cloned := append([]config.LoginItem(nil), sequence...)
	e.loginSeq.Store(&cloned)
}

// GetLoginSequence returns a copy of the login sequence, safe for the caller
// to hold or mutate.
func (e *MenuExecutor) GetLoginSequence() []config.LoginItem {
	p := e.loginSeq.Load()
	if p == nil {
		return nil
	}
	return append([]config.LoginItem(nil), (*p)...)
}

// SetStrings atomically updates the strings configuration.
func (e *MenuExecutor) SetStrings(strings config.StringsConfig) {
	e.stringsCfg.Store(&strings)
}

// Strings returns the current strings configuration.
//
// A pointer, not a copy: StringsConfig is several hundred fields and this is
// the hottest accessor in the package, called once per displayed string. The
// value behind it is shared and must not be mutated.
func (e *MenuExecutor) Strings() *config.StringsConfig {
	if p := e.stringsCfg.Load(); p != nil {
		return p
	}
	// Only reachable if an executor was built without NewExecutor. Returning a
	// zero config keeps callers from dereferencing nil; every string reads as
	// empty, which their existing empty-string fallbacks already handle.
	return &config.StringsConfig{}
}

// SetTheme atomically updates the theme configuration.
func (e *MenuExecutor) SetTheme(theme config.ThemeConfig) {
	e.themeCfg.Store(&theme)
}

// Theme returns the current theme configuration. Read-only.
func (e *MenuExecutor) Theme() *config.ThemeConfig {
	if p := e.themeCfg.Load(); p != nil {
		return p
	}
	return &config.ThemeConfig{}
}

// SetServerConfig atomically updates the server configuration.
func (e *MenuExecutor) SetServerConfig(serverCfg config.ServerConfig) {
	e.serverCfg.Store(&serverCfg)
}

// GetServerConfig atomically retrieves the server configuration.
//
// Returns a copy, unlike the other accessors: callers routinely assign the
// result to a local and some adjust fields on it before use, which would
// corrupt the shared snapshot if they held a pointer into it.
func (e *MenuExecutor) GetServerConfig() config.ServerConfig {
	if p := e.serverCfg.Load(); p != nil {
		return *p
	}
	return config.ServerConfig{}
}

// SetProtocols atomically updates the transfer protocol configurations. The
// slice is cloned on store, so the caller keeping its own reference cannot
// reach the shared snapshot.
func (e *MenuExecutor) SetProtocols(protocols []transfer.ProtocolConfig) {
	cloned := append([]transfer.ProtocolConfig(nil), protocols...)
	e.protocols.Store(&cloned)
}

// Protocols returns a copy of the transfer protocol configurations, safe for
// the caller to hold or mutate.
func (e *MenuExecutor) Protocols() []transfer.ProtocolConfig {
	p := e.protocols.Load()
	if p == nil {
		return nil
	}
	return append([]transfer.ProtocolConfig(nil), (*p)...)
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
	return u != nil && u.AccessLevel >= e.GetServerConfig().CoSysOpLevel
}

// handleIdleTimeout displays TIMEOUT.ANS (if available) or falls back to the
// idle timeout string, then logs the disconnection. Call this before returning
// LOGOFF/DISCONNECT whenever ErrIdleTimeout is received from any input loop.
func (e *MenuExecutor) handleIdleTimeout(terminal *term.Terminal, outputMode ansi.OutputMode, nodeNumber int, termHeight int) {
	// Try to display TIMEOUT.ANS first.
	ansPath := e.menuFile("ansi", "TIMEOUT.ANS")
	if rawContent, err := ansi.GetAnsiFileContent(ansPath); err == nil {
		terminalio.WriteProcessedBytes(terminal, []byte(ansi.ClearScreen()), outputMode)
		if outputMode == ansi.OutputModeCP437 {
			_, _ = terminal.Write(rawContent) // best-effort display
		} else {
			terminalio.WriteProcessedBytes(terminal, rawContent, outputMode)
		}
	} else {
		// Fall back to the configured string.
		msg := e.Strings().IdleTimeout
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
	errMsg := e.Strings().ExecUnknownCommand
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
