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
	"github.com/ViSiON-3/vision-3-bbs/internal/snoop"
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

// RunnableFunc is the signature of a built-in command that a menu reaches with
// a "RUN:<TARGET> args" action (and of the "DOOR:" handler); implementations
// are registered in MenuExecutor.RunRegistry under the upper-cased target.
// args is whatever followed the target on the command line. The returned user
// replaces the session's current user (login runnables use it to report who
// authenticated). nextAction may be "GOTO:<MENU>" or "LOGOFF"; anything else,
// including "", stays on the current menu. An io.EOF or idle-timeout error
// logs the caller off; any other error is shown to the caller and the menu
// continues.
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
	Pager           SysopPager                    // WFC side of PAGESYSOP (nil = no console)

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
	SetSysopChatEnv(chatEnv{theme: e.Theme, strings: e.Strings, caller: e.chatCaller, ended: e.chatEnded, holdDropped: e.chatHoldDropped})
	return e
}

// chatCaller returns the handle and saved screen size of the logged-in user
// on the session carrying tap, or zero values when there is none.
func (e *MenuExecutor) chatCaller(tap *snoop.Tap) (handle string, width, height int) {
	for _, bs := range e.activeSessions() {
		if bs.Tap != tap {
			continue
		}
		bs.Mutex.RLock()
		defer bs.Mutex.RUnlock()
		if bs.User == nil {
			return "", 0, 0
		}
		return bs.User.Handle, bs.User.ScreenWidth, bs.User.ScreenHeight
	}
	return "", 0, 0
}

// chatEnded tells the WFC consoles, through Pager, that chat on the session
// carrying tap is over.
func (e *MenuExecutor) chatEnded(tap *snoop.Tap) {
	p, ok := e.Pager.(interface {
		ChatEnded(nodeID int, handle string)
	})
	if !ok {
		return
	}
	for _, bs := range e.activeSessions() {
		if bs.Tap != tap {
			continue
		}
		bs.Mutex.RLock()
		node, handle := bs.NodeID, ""
		if bs.User != nil {
			handle = bs.User.Handle
		}
		bs.Mutex.RUnlock()
		p.ChatEnded(node, handle)
		return
	}
}

// chatHoldDropped logs the end of a type-in hold that the caller's end of
// chat dropped. It records no keystrokes.
func (e *MenuExecutor) chatHoldDropped(tap *snoop.Tap, sysop string, held time.Duration, injected int) {
	node := 0
	for _, bs := range e.activeSessions() {
		if bs.Tap == tap {
			bs.Mutex.RLock()
			node = bs.NodeID
			bs.Mutex.RUnlock()
			break
		}
	}
	slog.Info("wfc-snoop: type-in off", "sysop", sysop, "node", node,
		"duration", held.Round(time.Second), "bytes", injected)
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

// SetServerConfig atomically updates the server configuration. The sysop
// levels are sanitized here as well as at load, since configs also arrive
// from the config editor and reloads, and the handlers and ACS keywords
// must agree on them.
func (e *MenuExecutor) SetServerConfig(serverCfg config.ServerConfig) {
	serverCfg.SanitizeAccessLevels()
	e.serverCfg.Store(&serverCfg)
	setACSSysOpLevels(serverCfg.SysOpLevel, serverCfg.CoSysOpLevel)
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

// timeLimit returns u's time limit per call in minutes, 0 meaning none.
// CoSysOps and above have none whatever their record says, as they have no
// idle timeout.
func (e *MenuExecutor) timeLimit(u *user.User) int {
	if u == nil || u.TimeLimit <= 0 || e.isCoSysOpOrAbove(u) {
		return 0
	}
	return u.TimeLimit
}

// TimeLimit is timeLimit for callers outside the menu package, such as the
// WFC console's time-left column.
func (e *MenuExecutor) TimeLimit(u *user.User) int { return e.timeLimit(u) }

// sessionDeadline returns when u's time runs out for a session that started
// at sessionStart, or the zero time if u has no limit.
func (e *MenuExecutor) sessionDeadline(u *user.User, sessionStart time.Time) time.Time {
	limit := e.timeLimit(u)
	if limit == 0 {
		return time.Time{}
	}
	return sessionStart.Add(time.Duration(limit) * time.Minute)
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

// isSysOpOrAbove returns true if the user has SysOp access level. Handlers use
// this instead of comparing against a literal 255 so a board that sets
// sysOpLevel lower in config.json gets the same answer everywhere.
func (e *MenuExecutor) isSysOpOrAbove(u *user.User) bool {
	return u != nil && u.AccessLevel >= e.GetServerConfig().SysOpLevel
}

// nodeSession returns the registered session for nodeNumber, or nil when
// there is none. The server always sets SessionRegistry, but tests and tools
// that build an executor by hand may not, so every lookup goes through here
// rather than dereferencing the registry directly.
func (e *MenuExecutor) nodeSession(nodeNumber int) *session.BbsSession {
	if e.SessionRegistry == nil {
		return nil
	}
	return e.SessionRegistry.Get(nodeNumber)
}

// activeSessions returns the online sessions sorted by node, or nil when the
// executor has no SessionRegistry (see nodeSession).
func (e *MenuExecutor) activeSessions() []*session.BbsSession {
	if e.SessionRegistry == nil {
		return nil
	}
	return e.SessionRegistry.ListActive()
}

// activeNodeCount returns how many sessions are online, or 0 when the
// executor has no SessionRegistry (see nodeSession).
func (e *MenuExecutor) activeNodeCount() int {
	if e.SessionRegistry == nil {
		return 0
	}
	return e.SessionRegistry.ActiveCount()
}

// handleIdleTimeout displays TIMEOUT.ANS (if available) or falls back to the
// idle timeout string, then logs the disconnection. Call this before returning
// LOGOFF/DISCONNECT whenever ErrIdleTimeout is received from any input loop.
func (e *MenuExecutor) handleIdleTimeout(terminal *term.Terminal, outputMode ansi.OutputMode, nodeNumber int, termWidth, termHeight int) {
	// Try to display TIMEOUT.ANS first.
	ansPath := e.menuFile("ansi", "TIMEOUT.ANS")
	if rawContent, err := ansi.GetAnsiFileContent(ansPath); err == nil {
		terminalio.WriteProcessedBytes(terminal, []byte(ansi.ClearScreen()), outputMode)
		_ = writeArt(terminal, rawContent, outputMode, termWidth) // best-effort display
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

// timeLimitReached reports whether s has a time limit and it has run out.
func timeLimitReached(s ssh.Session) bool {
	d, ok := sessionDeadlines.Load(s)
	return ok && !time.Now().Before(d.(time.Time))
}

// handleSessionTimeout shows why a session is ending after an input loop
// returned editor.ErrIdleTimeout: the time-limit message if the caller's time
// has run out, the idle timeout screen otherwise. The two share an error
// (editor.ErrTimeLimit wraps editor.ErrIdleTimeout, and many loops pass on the
// plain sentinel), so the session's deadline is what tells them apart.
func (e *MenuExecutor) handleSessionTimeout(s ssh.Session, terminal *term.Terminal, outputMode ansi.OutputMode, nodeNumber int, termWidth, termHeight int) {
	if timeLimitReached(s) {
		e.handleTimeLimit(terminal, outputMode, nodeNumber)
		return
	}
	e.handleIdleTimeout(terminal, outputMode, nodeNumber, termWidth, termHeight)
}

// timeLimitWarnWindow is how close to the end of their time a caller starts
// being warned at each menu prompt.
const timeLimitWarnWindow = 5 * time.Minute

// timeLeftWarning returns the warning due for s once it is within
// timeLimitWarnWindow of its time limit, and false before that or with no
// limit. Part of a minute counts as a whole one, so the last warning says 1
// rather than 0.
func (e *MenuExecutor) timeLeftWarning(s ssh.Session) (string, bool) {
	d, ok := sessionDeadlines.Load(s)
	if !ok {
		return "", false
	}
	left := time.Until(d.(time.Time))
	if left <= 0 || left > timeLimitWarnWindow {
		return "", false
	}
	minutes := int((left + time.Minute - 1) / time.Minute)
	return fmt.Sprintf(e.Strings().TimeLimitWarning, minutes), true
}

// warnTimeLeft writes the time-limit warning, if one is due, at the cursor.
// Standard menus call it before their prompt.
func (e *MenuExecutor) warnTimeLeft(s ssh.Session, terminal *term.Terminal, outputMode ansi.OutputMode) {
	if msg, ok := e.timeLeftWarning(s); ok {
		_ = terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode) // best-effort notice
	}
}

// warnTimeLeftOnRow draws the time-limit warning, if one is due, on screen
// row row of a width-column terminal and puts the cursor back. Lightbar menus
// use it: their screens are drawn at fixed positions, so the warning goes on
// the bottom row and must not scroll it. The string's line breaks are dropped
// and it is clipped a column short of the width, since text reaching the last
// column of the bottom row makes some terminals scroll too.
func (e *MenuExecutor) warnTimeLeftOnRow(s ssh.Session, terminal *term.Terminal, outputMode ansi.OutputMode, row, width int) {
	msg, ok := e.timeLeftWarning(s)
	if !ok {
		return
	}
	msg = strings.NewReplacer("\r", "", "\n", "").Replace(msg)
	if row < 1 {
		row = 1
	}
	if width < 2 {
		width = 80
	}
	msg = clipColumns(string(ansi.ReplacePipeCodes([]byte(msg))), width-1, outputMode)
	out := fmt.Sprintf("\x1b[s\x1b[%d;1H\x1b[2K%s\x1b[0m\x1b[u", row, msg)
	_ = terminalio.WriteProcessedBytes(terminal, []byte(out), outputMode) // best-effort notice
}

// handleTimeLimit tells the caller their time limit is up and logs it. Call
// it before returning LOGOFF for an expired time limit.
func (e *MenuExecutor) handleTimeLimit(terminal *term.Terminal, outputMode ansi.OutputMode, nodeNumber int) {
	_ = terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(e.Strings().TimeLimitExpired)), outputMode) // best-effort notice
	slog.Info("time limit reached, disconnecting", "node", nodeNumber)
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
	uiPause(500 * time.Millisecond)
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
