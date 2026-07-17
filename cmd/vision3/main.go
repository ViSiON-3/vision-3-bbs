package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/gliderlabs/ssh" // Keep for type compatibility
	"golang.org/x/term"

	// Local packages (Update paths)
	"github.com/ViSiON-3/vision-3-bbs/internal/admin"
	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/chat"
	"github.com/ViSiON-3/vision-3-bbs/internal/conference"
	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/file"
	"github.com/ViSiON-3/vision-3-bbs/internal/logging"
	"github.com/ViSiON-3/vision-3-bbs/internal/mailer"
	"github.com/ViSiON-3/vision-3-bbs/internal/menu"
	"github.com/ViSiON-3/vision-3-bbs/internal/message"
	"github.com/ViSiON-3/vision-3-bbs/internal/qwkapi"
	"github.com/ViSiON-3/vision-3-bbs/internal/qwkservice"
	"github.com/ViSiON-3/vision-3-bbs/internal/scheduler"
	"github.com/ViSiON-3/vision-3-bbs/internal/session"
	"github.com/ViSiON-3/vision-3-bbs/internal/telnetserver"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
	"github.com/ViSiON-3/vision-3-bbs/internal/transfer"
	"github.com/ViSiON-3/vision-3-bbs/internal/types"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
	v3net "github.com/ViSiON-3/vision-3-bbs/internal/v3net"
)

var (
	userMgr         *user.UserMgr
	messageMgr      *message.MessageManager
	fileMgr         *file.FileManager
	confMgr         *conference.ConferenceManager
	menuExecutor    *menu.MenuExecutor
	sessionRegistry *session.SessionRegistry
	// globalConfig *config.GlobalConfig // Still commented out
	nodeCounter         int32
	activeSessions      = make(map[ssh.Session]int32)
	activeSessionsMutex sync.Mutex
	loadedStrings       config.StringsConfig
	loadedTheme         config.ThemeConfig
	// colorTestMode       bool   // Flag variable REMOVED
	outputModeFlag    string             // Output mode flag (auto, utf8, cp437)
	connectionTracker *ConnectionTracker // Global connection tracker
	v3netService      *v3net.Service     // V3Net networking service (nil if disabled)
)

// allocateNodeIDForSession assigns the lowest available node slot (1..maxNodes)
// and records it in activeSessions. Falls back to a monotonic counter if maxNodes
// is unavailable or all slots appear occupied.
func allocateNodeIDForSession(s ssh.Session) int32 {
	activeSessionsMutex.Lock()
	defer activeSessionsMutex.Unlock()

	maxNodes := 0
	if connectionTracker != nil {
		maxNodes = connectionTracker.maxNodes
	}

	if maxNodes > 0 {
		used := make(map[int32]bool, len(activeSessions))
		for _, id := range activeSessions {
			if id > 0 && int(id) <= maxNodes {
				used[id] = true
			}
		}

		for slot := int32(1); slot <= int32(maxNodes); slot++ {
			if !used[slot] {
				activeSessions[s] = slot
				return slot
			}
		}
	}

	fallback := atomic.AddInt32(&nodeCounter, 1)
	activeSessions[s] = fallback
	return fallback
}

// IPList holds a list of IP addresses and CIDR ranges
type IPList struct {
	ips      map[string]bool // Individual IP addresses
	networks []*net.IPNet    // CIDR ranges
}

// IPLockoutTracker tracks failed login attempts per IP
type IPLockoutTracker struct {
	Attempts    int
	LastAttempt time.Time
	LockedUntil time.Time
}

// ConnectionTracker manages active connections and enforces limits
type ConnectionTracker struct {
	mu                  sync.Mutex
	activeConnections   map[string]int               // IP address -> connection count
	failedLogins        map[string]*IPLockoutTracker // IP address -> lockout tracker
	maxNodes            int
	maxConnectionsPerIP int
	totalConnections    int
	blocklist           *IPList
	allowlist           *IPList
	blocklistPath       string // Path to blocklist file for watching
	allowlistPath       string // Path to allowlist file for watching
	maxFailedLogins     int
	lockoutMinutes      int
	watcher             *fsnotify.Watcher // File system watcher for auto-reload
	watcherDone         chan bool         // Signal to stop watcher
}

// NewConnectionTracker creates a new connection tracker
func NewConnectionTracker(maxNodes, maxConnectionsPerIP, maxFailedLogins, lockoutMinutes int, blocklistPath, allowlistPath string) *ConnectionTracker {
	ct := &ConnectionTracker{
		activeConnections:   make(map[string]int),
		failedLogins:        make(map[string]*IPLockoutTracker),
		maxNodes:            maxNodes,
		maxConnectionsPerIP: maxConnectionsPerIP,
		blocklist:           nil,
		allowlist:           nil,
		blocklistPath:       blocklistPath,
		allowlistPath:       allowlistPath,
		maxFailedLogins:     maxFailedLogins,
		lockoutMinutes:      lockoutMinutes,
	}

	// Load initial IP lists
	if blocklistPath != "" {
		blocklist, err := LoadIPList(blocklistPath)
		if err != nil {
			slog.Error("failed to load initial blocklist", "error", err)
		} else {
			ct.blocklist = blocklist
		}
	}

	if allowlistPath != "" {
		allowlist, err := LoadIPList(allowlistPath)
		if err != nil {
			slog.Error("failed to load initial allowlist", "error", err)
		} else {
			ct.allowlist = allowlist
		}
	}

	// Start watching files for changes
	if err := ct.startWatching(); err != nil {
		slog.Error("failed to start file watcher", "error", err)
	}

	return ct
}

// LoadIPList loads an IP list from a file
// File format: one IP or CIDR range per line, # for comments
func LoadIPList(filePath string) (*IPList, error) {
	if filePath == "" {
		return nil, nil
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // File doesn't exist, not an error
		}
		return nil, fmt.Errorf("failed to read IP list %s: %w", filePath, err)
	}

	list := &IPList{
		ips:      make(map[string]bool),
		networks: make([]*net.IPNet, 0),
	}

	lines := strings.Split(string(data), "\n")
	for lineNum, line := range lines {
		// Trim whitespace
		line = strings.TrimSpace(line)

		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Check if it's a CIDR range
		if strings.Contains(line, "/") {
			_, network, err := net.ParseCIDR(line)
			if err != nil {
				slog.Warn("invalid CIDR in IP list", "file", filePath, "line", lineNum+1, "value", line)
				continue
			}
			list.networks = append(list.networks, network)
		} else {
			// Individual IP address
			ip := net.ParseIP(line)
			if ip == nil {
				slog.Warn("invalid IP in IP list", "file", filePath, "line", lineNum+1, "value", line)
				continue
			}
			list.ips[ip.String()] = true
		}
	}

	slog.Info("loaded IP list", "file", filePath, "ips", len(list.ips), "cidrs", len(list.networks))
	return list, nil
}

// Contains checks if an IP address is in the list
func (list *IPList) Contains(ipStr string) bool {
	if list == nil {
		return false
	}

	// Check individual IPs
	if list.ips[ipStr] {
		return true
	}

	// Check CIDR ranges
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return false
	}

	for _, network := range list.networks {
		if network.Contains(ip) {
			return true
		}
	}

	return false
}

// CanAccept checks if a new connection from the given IP can be accepted
func (ct *ConnectionTracker) CanAccept(remoteAddr net.Addr) (bool, string) {
	ct.mu.Lock()
	defer ct.mu.Unlock()

	return ct.canAcceptLocked(remoteAddr)
}

// canAcceptLocked performs the accept check without acquiring the lock.
func (ct *ConnectionTracker) canAcceptLocked(remoteAddr net.Addr) (bool, string) {
	// Extract IP from address (strip port)
	ip := extractIP(remoteAddr)

	// Check allowlist first - if IP is on allowlist, skip all other checks
	if ct.allowlist != nil && ct.allowlist.Contains(ip) {
		slog.Debug("IP on allowlist, bypassing checks", "ip", ip)
		return true, ""
	}

	// Check blocklist
	if ct.blocklist != nil && ct.blocklist.Contains(ip) {
		return false, "IP address is blocked"
	}

	// Check max nodes limit
	if ct.maxNodes > 0 && ct.totalConnections >= ct.maxNodes {
		return false, "maximum nodes reached"
	}

	// Check per-IP limit
	if ct.maxConnectionsPerIP > 0 {
		if count, exists := ct.activeConnections[ip]; exists && count >= ct.maxConnectionsPerIP {
			return false, "maximum connections per IP reached"
		}
	}

	return true, ""
}

// TryAccept atomically checks limits and registers the connection.
// Returns (true, "") on success, (false, reason) on rejection.
func (ct *ConnectionTracker) TryAccept(remoteAddr net.Addr) (bool, string) {
	ct.mu.Lock()
	defer ct.mu.Unlock()

	ok, reason := ct.canAcceptLocked(remoteAddr)
	if !ok {
		return false, reason
	}

	ip := extractIP(remoteAddr)
	ct.activeConnections[ip]++
	ct.totalConnections++

	slog.Info("connection added", "ip", ip, "ip_count", ct.activeConnections[ip], "total", ct.totalConnections, "max", ct.maxNodes)
	return true, ""
}

// AddConnection registers a new connection from the given IP
func (ct *ConnectionTracker) AddConnection(remoteAddr net.Addr) {
	ct.mu.Lock()
	defer ct.mu.Unlock()

	ip := extractIP(remoteAddr)
	ct.activeConnections[ip]++
	ct.totalConnections++

	slog.Info("connection added", "ip", ip, "ip_count", ct.activeConnections[ip], "total", ct.totalConnections, "max", ct.maxNodes)
}

// RemoveConnection unregisters a connection from the given IP
func (ct *ConnectionTracker) RemoveConnection(remoteAddr net.Addr) {
	ct.mu.Lock()
	defer ct.mu.Unlock()

	ip := extractIP(remoteAddr)
	if count, exists := ct.activeConnections[ip]; exists {
		if count <= 1 {
			delete(ct.activeConnections, ip)
		} else {
			ct.activeConnections[ip]--
		}
	}
	if ct.totalConnections > 0 {
		ct.totalConnections--
	}

	slog.Info("connection removed", "ip", ip, "ip_count", ct.activeConnections[ip], "total", ct.totalConnections, "max", ct.maxNodes)
}

// GetStats returns current connection statistics
func (ct *ConnectionTracker) GetStats() (totalConns, uniqueIPs int) {
	ct.mu.Lock()
	defer ct.mu.Unlock()
	return ct.totalConnections, len(ct.activeConnections)
}

// extractIP extracts the IP address from a net.Addr, stripping the port
func extractIP(addr net.Addr) string {
	if tcpAddr, ok := addr.(*net.TCPAddr); ok {
		return tcpAddr.IP.String()
	}
	// Fallback: parse string representation
	host, _, err := net.SplitHostPort(addr.String())
	if err != nil {
		return addr.String() // Return as-is if parsing fails
	}
	return host
}

// IsIPLockedOut checks if an IP address is currently locked out due to failed login attempts.
// Returns (isLocked, lockedUntil, remainingAttempts)
func (ct *ConnectionTracker) IsIPLockedOut(ip string) (bool, time.Time, int) {
	ct.mu.Lock()
	defer ct.mu.Unlock()

	tracker, exists := ct.failedLogins[ip]
	if !exists {
		return false, time.Time{}, ct.maxFailedLogins
	}

	// Check if lockout has expired
	if !tracker.LockedUntil.IsZero() && time.Now().Before(tracker.LockedUntil) {
		return true, tracker.LockedUntil, 0
	}

	// Lockout expired, clear it
	if !tracker.LockedUntil.IsZero() && time.Now().After(tracker.LockedUntil) {
		delete(ct.failedLogins, ip)
		return false, time.Time{}, ct.maxFailedLogins
	}

	remainingAttempts := ct.maxFailedLogins - tracker.Attempts
	if remainingAttempts < 0 {
		remainingAttempts = 0
	}
	return false, time.Time{}, remainingAttempts
}

// IsAllowlisted reports whether the IP is on the allowlist (and thus exempt
// from the challenge gate, matching how it bypasses the accept-time checks).
func (ct *ConnectionTracker) IsAllowlisted(ip string) bool {
	ct.mu.Lock()
	defer ct.mu.Unlock()
	return ct.allowlist != nil && ct.allowlist.Contains(ip)
}

// RecordFailedLoginAttempt records a failed login attempt from an IP address.
// Returns true if the IP was just locked out.
func (ct *ConnectionTracker) RecordFailedLoginAttempt(ip string) bool {
	ct.mu.Lock()
	defer ct.mu.Unlock()

	// Don't track if feature is disabled
	if ct.maxFailedLogins == 0 {
		return false
	}

	tracker, exists := ct.failedLogins[ip]
	if !exists {
		tracker = &IPLockoutTracker{}
		ct.failedLogins[ip] = tracker
	}

	tracker.Attempts++
	tracker.LastAttempt = time.Now()

	// Check if lockout threshold reached
	if tracker.Attempts >= ct.maxFailedLogins {
		tracker.LockedUntil = time.Now().Add(time.Duration(ct.lockoutMinutes) * time.Minute)
		logging.Security("IP locked out after failed login attempts",
			"ip", ip,
			"attempts", tracker.Attempts,
			"locked_until", tracker.LockedUntil.Format(time.RFC3339))
		// Persist to blocklist file outside the lock to avoid deadlock
		go func() {
			if err := ct.AppendToBlocklist(ip); err != nil {
				slog.Error("failed to persist blocked IP to blocklist", "ip", ip, "error", err)
			}
		}()
		return true
	}

	logging.Security("failed login attempt", "ip", ip, "attempts", tracker.Attempts, "max", ct.maxFailedLogins)
	return false
}

// AppendToBlocklist appends an IP to the blocklist file and updates the in-memory list immediately.
// If blocklistPath is not configured, this is a no-op.
func (ct *ConnectionTracker) AppendToBlocklist(ip string) error {
	if ct.blocklistPath == "" {
		return nil
	}

	parsedIP := net.ParseIP(ip)
	if parsedIP == nil {
		return fmt.Errorf("invalid IP address: %s", ip)
	}
	normalizedIP := parsedIP.String()

	// Check if already in blocklist
	ct.mu.Lock()
	alreadyBlocked := ct.blocklist != nil && ct.blocklist.Contains(normalizedIP)
	ct.mu.Unlock()

	if alreadyBlocked {
		return nil
	}

	// Append to file
	f, err := os.OpenFile(ct.blocklistPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to open blocklist file: %w", err)
	}
	timestamp := time.Now().Format("2006-01-02 15:04:05")
	line := fmt.Sprintf("%s # auto-blocked %s: too many failed logins\n", normalizedIP, timestamp)
	if _, err := f.WriteString(line); err != nil {
		_ = f.Close() // best-effort; the write error takes precedence
		return fmt.Errorf("failed to write to blocklist file: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("failed to close blocklist file: %w", err)
	}

	// Update in-memory list immediately — don't wait for fsnotify debounce
	ct.mu.Lock()
	if ct.blocklist == nil {
		ct.blocklist = &IPList{
			ips:      make(map[string]bool),
			networks: make([]*net.IPNet, 0),
		}
	}
	ct.blocklist.ips[normalizedIP] = true
	ct.mu.Unlock()

	logging.Security("IP permanently added to blocklist", "ip", normalizedIP, "file", ct.blocklistPath)
	return nil
}

// ClearFailedLoginAttempts clears the failed login counter for an IP on successful authentication.
func (ct *ConnectionTracker) ClearFailedLoginAttempts(ip string) {
	ct.mu.Lock()
	defer ct.mu.Unlock()

	if tracker, exists := ct.failedLogins[ip]; exists {
		if tracker.Attempts > 0 {
			slog.Info("cleared failed login attempts", "ip", ip, "attempts", tracker.Attempts)
		}
		delete(ct.failedLogins, ip)
	}
}

// reloadIPLists reloads the blocklist and allowlist from disk.
// Both lists are loaded outside the lock and swapped atomically under a single lock.
func (ct *ConnectionTracker) reloadIPLists() {
	slog.Info("reloading IP filter lists")

	// Load both lists outside the lock (I/O can be slow)
	var newBlocklist, newAllowlist *IPList

	if ct.blocklistPath != "" {
		bl, err := LoadIPList(ct.blocklistPath)
		if err != nil {
			slog.Error("failed to reload blocklist", "file", ct.blocklistPath, "error", err)
		} else {
			newBlocklist = bl
		}
	}

	if ct.allowlistPath != "" {
		al, err := LoadIPList(ct.allowlistPath)
		if err != nil {
			slog.Error("failed to reload allowlist", "file", ct.allowlistPath, "error", err)
		} else {
			newAllowlist = al
		}
	}

	// Swap both lists atomically under a single lock
	ct.mu.Lock()
	if ct.blocklistPath != "" {
		ct.blocklist = newBlocklist
	}
	if ct.allowlistPath != "" {
		ct.allowlist = newAllowlist
	}
	ct.mu.Unlock()

	slog.Info("IP filter lists reloaded")
}

// startWatching starts watching the IP list files for changes
func (ct *ConnectionTracker) startWatching() error {
	// Don't start watcher if no files to watch
	if ct.blocklistPath == "" && ct.allowlistPath == "" {
		slog.Debug("no IP list files to watch, file watching disabled")
		return nil
	}

	var err error
	ct.watcher, err = fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("failed to create file watcher: %w", err)
	}

	ct.watcherDone = make(chan bool)

	// Add files to watch
	filesToWatch := []string{}
	if ct.blocklistPath != "" {
		if _, err := os.Stat(ct.blocklistPath); err == nil {
			filesToWatch = append(filesToWatch, ct.blocklistPath)
		} else {
			slog.Warn("blocklist file does not exist, not watching", "file", ct.blocklistPath)
		}
	}
	if ct.allowlistPath != "" {
		if _, err := os.Stat(ct.allowlistPath); err == nil {
			filesToWatch = append(filesToWatch, ct.allowlistPath)
		} else {
			slog.Warn("allowlist file does not exist, not watching", "file", ct.allowlistPath)
		}
	}

	for _, file := range filesToWatch {
		if err := ct.watcher.Add(file); err != nil {
			slog.Error("failed to watch file", "file", file, "error", err)
		} else {
			slog.Info("watching file for changes", "file", file)
		}
	}

	// Start watching in a goroutine
	go ct.watchLoop()

	return nil
}

// watchLoop handles file system events
func (ct *ConnectionTracker) watchLoop() {
	// Debounce timer to avoid reloading on rapid successive writes
	var debounceTimer *time.Timer
	debounceDuration := 500 * time.Millisecond

	for {
		select {
		case event, ok := <-ct.watcher.Events:
			if !ok {
				return
			}

			// Only care about Write and Create events
			if event.Op&fsnotify.Write == fsnotify.Write || event.Op&fsnotify.Create == fsnotify.Create {
				slog.Debug("file change detected", "file", event.Name, "op", event.Op)

				// Reset debounce timer
				if debounceTimer != nil {
					debounceTimer.Stop()
				}
				debounceTimer = time.AfterFunc(debounceDuration, func() {
					ct.reloadIPLists()
				})
			}

		case err, ok := <-ct.watcher.Errors:
			if !ok {
				return
			}
			slog.Error("file watcher error", "error", err)

		case <-ct.watcherDone:
			slog.Info("stopping IP list file watcher")
			return
		}
	}
}

// StopWatching stops the file watcher
func (ct *ConnectionTracker) StopWatching() {
	if ct.watcher != nil {
		close(ct.watcherDone)
		_ = ct.watcher.Close() // best-effort watcher shutdown
	}
}

// --- BBS sessionHandler (Original logic) ---
func sessionHandler(s ssh.Session) {
	nodeID := allocateNodeIDForSession(s)
	remoteAddr := s.RemoteAddr().String()

	// Extract session ID if available (type-specific)
	sessionID := fmt.Sprintf("node-%d", nodeID)
	if ctx, ok := s.Context().(interface{ SessionID() string }); ok {
		sessionID = ctx.SessionID()
	}

	slog.Info("connection from", "node", nodeID, "addr", remoteAddr, "user", s.User(), "session", sessionID)
	slog.Debug("session environment", "node", nodeID, "env", s.Environ())
	slog.Debug("session command", "node", nodeID, "command", s.Command())

	// Capture start time and declare authenticatedUser *before* the defer
	capturedStartTime := time.Now()          // Capture start time close to session start
	var authenticatedUser *user.User = nil   // Declare here so the closure can capture it
	var bbsSession *session.BbsSession = nil // Declare here so the deferred disconnect can read Invisible flag

	// Defer removal from active sessions map and logging disconnection
	// The deferred function now uses a closure to access authenticatedUser
	defer func(startTime time.Time) {
		slog.Info("disconnected", "node", nodeID, "addr", remoteAddr, "user", s.User())
		activeSessionsMutex.Lock()
		delete(activeSessions, s) // Remove using session as key
		activeSessionsMutex.Unlock()
		if sessionRegistry != nil {
			sessionRegistry.Unregister(int(nodeID))
		}

		// V3Net logoff notification
		if v3netService != nil && authenticatedUser != nil {
			v3netService.SendLogoff(authenticatedUser.Handle)
		}

		// --- Record Call History ---
		if authenticatedUser != nil {
			slog.Debug("adding call record", "node", nodeID, "user", authenticatedUser.Handle, "id", authenticatedUser.ID)

			// Mark user as offline
			userMgr.MarkUserOffline(authenticatedUser.ID)

			disconnectTime := time.Now()
			duration := disconnectTime.Sub(startTime) // Use the captured startTime
			callRec := user.CallRecord{
				UserID:         authenticatedUser.ID,
				Handle:         authenticatedUser.Handle,
				GroupLocation:  authenticatedUser.GroupLocation,
				NodeID:         int(nodeID),
				ConnectTime:    startTime,
				DisconnectTime: disconnectTime,
				Duration:       duration,
				UploadedMB:     0.0,
				DownloadedMB:   0.0,
				Actions:        "",
				BaudRate:       "38400",
			}
			if bbsSession != nil {
				bbsSession.Mutex.RLock()
				callRec.Invisible = bbsSession.Invisible
				bbsSession.Mutex.RUnlock()
			}
			userMgr.AddCallRecord(callRec)
		} else {
			slog.Debug("no authenticated user, skipping call record", "node", nodeID)
		}
		// ------------------------
		_ = s.Close() // ensure the session is closed; best-effort
	}(capturedStartTime) // Pass only the startTime value

	// Create the session state object *early* - COMMENTED OUT (not used, type mismatch)
	// sessionState := &session.BbsSession{
	// 	// Conn:    s.Conn,     // Need the underlying gossh.Conn if possible, might need context
	// 	Channel:    nil,         // Channel might not be directly available here, depends on gliderlabs/ssh context
	// 	User:       nil,         // Set after authentication
	// 	ID:         int(nodeID), // Use correct field name 'ID'
	// 	StartTime:  time.Now(),  // Record session start time
	// 	Pty:        nil,         // Will be set if/when PTY is granted
	// 	AutoRunLog: make(types.AutoRunTracker),
	// }

	// --- PTY Request Handling ---
	ptyReq, winCh, isPty := s.Pty() // Get PTY info from SessionAdapter
	var termWidth, termHeight atomic.Int32
	termWidth.Store(80)  // Default terminal width
	termHeight.Store(25) // Default terminal height
	if isPty {
		slog.Info("PTY request accepted", "node", nodeID, "term", ptyReq.Term)
		if ptyReq.Window.Width > 0 {
			termWidth.Store(int32(ptyReq.Window.Width))
		}
		if ptyReq.Window.Height > 0 {
			termHeight.Store(int32(ptyReq.Window.Height))
		}
		slog.Info("terminal size from PTY", "node", nodeID, "width", termWidth.Load(), "height", termHeight.Load())
	} else {
		slog.Info("no PTY request", "node", nodeID)
	}

	// --- Determine Output Mode ---
	effectiveMode := ansi.OutputModeAuto // Start with Auto as the base
	switch outputModeFlag {              // Check the global flag first
	case "utf8":
		effectiveMode = ansi.OutputModeUTF8
		slog.Info("output mode forced to UTF-8 by flag", "node", nodeID)
	case "cp437":
		effectiveMode = ansi.OutputModeCP437
		slog.Info("output mode forced to CP437 by flag", "node", nodeID)
	case "auto":
		// Auto mode: Use PTY info if available
		if isPty {
			termType := strings.ToLower(ptyReq.Term)
			termCols := int(termWidth.Load())
			slog.Info("auto mode detecting output based on TERM", "node", nodeID, "term", termType, "cols", termCols)

			// Detect retro BBS terminal clients using terminal capabilities
			isRetroTerminal := false

			// NetRunner: reports "ansi-256color-rgb" directly, or "xterm" with >80 cols
			// (the xterm heuristic is an SSH-path fallback for older NetRunner builds
			// that self-identify as xterm; telnet clients now use TERM_TYPE negotiation
			// and will report their actual type, so this heuristic is SSH-specific)
			if termType == "ansi-256color-rgb" || (termType == "xterm" && termCols > 80) {
				slog.Info("detected NetRunner", "node", nodeID, "term", termType, "cols", termCols)
				isRetroTerminal = true
			}

			// SyncTerm: Reports syncterm or sync
			if termType == "syncterm" || termType == "sync" {
				slog.Info("detected SyncTerm", "node", nodeID, "term", termType)
				isRetroTerminal = true
			}

			// Magiterm: Reports magiterm
			if termType == "magiterm" {
				slog.Info("detected Magiterm", "node", nodeID, "term", termType)
				isRetroTerminal = true
			}

			// Other known CP437 terminal types
			if termType == "ansi" || termType == "scoansi" || termType == "ansi-bbs" ||
				termType == "netrunner" || strings.HasPrefix(termType, "vt100") {
				slog.Info("detected CP437 terminal type", "node", nodeID, "term", termType)
				isRetroTerminal = true
			}

			if isRetroTerminal {
				slog.Info("auto mode selecting CP437 for retro BBS terminal", "node", nodeID)
				effectiveMode = ansi.OutputModeCP437
			} else {
				slog.Info("auto mode selecting UTF-8 for modern terminal", "node", nodeID, "term", termType)
				effectiveMode = ansi.OutputModeUTF8
			}
		} else {
			// No PTY, safer to default to UTF-8? Or CP437?
			// Let's default to UTF-8 for non-PTY as it's more common for raw streams.
			slog.Info("auto mode selecting UTF-8, no PTY requested", "node", nodeID)
			effectiveMode = ansi.OutputModeUTF8
		}
	}

	// --- Create Terminal ---
	slog.Info("creating terminal for session", "node", nodeID)
	terminal := term.NewTerminal(s, "") // Use session 's' as the R/W source for the terminal

	// Set initial terminal size from PTY request (term.NewTerminal defaults to 80 columns)
	if isPty {
		tw, th := int(termWidth.Load()), int(termHeight.Load())
		if tw > 0 && th > 0 {
			_ = terminal.SetSize(tw, th)
			slog.Info("set terminal size", "node", nodeID, "width", tw, "height", th)
		}
	}

	// --- Handle Window Size Changes ---
	// Forward resize events to both our atomic values and the term.Terminal.
	// Guard against nil winCh (ranging a nil channel blocks forever).
	if isPty && winCh != nil {
		go func() {
			type transferChecker interface{ IsTransferActive() bool }
			var pendingResize atomic.Bool
			var pendingW, pendingH atomic.Int32
			// When a resize is suppressed during a transfer, spin up a
			// short-lived goroutine that polls for transfer completion and
			// replays the suppressed resize so the terminal doesn't stay at
			// the wrong dimensions for the rest of the session.
			var replayOnce sync.Once
			var replayDone atomic.Bool
			spawnReplayWaiter := func() {
				tc, ok := s.(transferChecker)
				if !ok {
					return
				}
				replayOnce.Do(func() {
					go func() {
						ticker := time.NewTicker(250 * time.Millisecond)
						defer ticker.Stop()
						for range ticker.C {
							if !tc.IsTransferActive() {
								if pendingResize.Load() {
									w := int(pendingW.Load())
									h := int(pendingH.Load())
									slog.Debug("replaying pending resize after transfer ended", "node", nodeID, "width", w, "height", h)
									_ = terminal.SetSize(w, h)
									pendingResize.Store(false)
								}
								replayDone.Store(true)
								return
							}
						}
					}()
				})
			}
			for win := range winCh {
				slog.Debug("window resize event", "node", nodeID, "width", win.Width, "height", win.Height)
				if win.Width > 0 {
					termWidth.Store(int32(win.Width))
				}
				if win.Height > 0 {
					termHeight.Store(int32(win.Height))
				}
				if win.Width > 0 && win.Height > 0 {
					// Skip terminal repaint during binary transfers — SetSize
					// writes ANSI sequences via session.Write() which does CRLF
					// conversion, corrupting the RawWrite binary data stream.
					if tc, ok := s.(transferChecker); ok && tc.IsTransferActive() {
						slog.Debug("suppressed terminal.SetSize during active transfer", "node", nodeID)
						pendingResize.Store(true)
						pendingW.Store(int32(win.Width))
						pendingH.Store(int32(win.Height))
						spawnReplayWaiter()
					} else {
						// Replay any suppressed resize now that the transfer is done.
						if pendingResize.Load() {
							w := int(pendingW.Load())
							h := int(pendingH.Load())
							slog.Debug("replaying pending resize after transfer ended", "node", nodeID, "width", w, "height", h)
							pendingResize.Store(false)
							_ = terminal.SetSize(w, h)
						}
						// Reset the one-shot replay waiter for the next transfer.
						if replayDone.Load() {
							replayOnce = sync.Once{}
							replayDone.Store(false)
						}
						_ = terminal.SetSize(win.Width, win.Height)
					}
				}
			}
			slog.Debug("window change channel closed", "node", nodeID)
		}()
	}

	// Attempt to set raw mode (might fail, proceed anyway)
	if isPty {
		// Original terminal state
		// Note: term.MakeRaw requires an Fd(). This might not work directly with
		// the ssh.Session object on all platforms, especially Windows without
		// a proper underlying file descriptor for the PTY.
		// originalState, err := term.MakeRaw(int(s.Pty().Fd)) // This line is problematic
		// if err == nil {
		// 	 defer term.Restore(int(s.Pty().Fd), originalState)
		// 	 slog.Info("raw mode enabled", "node", nodeID)
		// } else {
		// 	 slog.Info("failed to enable raw mode, proceeding without", "node", nodeID, "error", err)
		// 	 // Continue without raw mode? Some BBS functions might rely on it.
		// }
		slog.Debug("skipping raw mode", "node", nodeID, "reason", "known issue with gliderlabs/ssh")
	} else {
		slog.Debug("skipping raw mode, no PTY requested", "node", nodeID)
	}

	// Encoding and terminal size prompts moved to after authentication
	// so we can check user's saved preferences and offer to save new settings

	// --- Authentication and Main Loop ---
	slog.Info("starting BBS logic", "node", nodeID)
	sessionStartTime := time.Now()

	bbsSession = &session.BbsSession{
		NodeID:       int(nodeID),
		StartTime:    sessionStartTime,
		LastActivity: sessionStartTime,
		CurrentMenu:  "LOGIN",
		RemoteAddr:   s.RemoteAddr(),
	}
	sessionRegistry.Register(bbsSession)

	currentMenuName := "LOGIN"               // Start with LOGIN
	autoRunLog := make(types.AutoRunTracker) // Initialize tracker for this session

	// Check if user was pre-authenticated at the SSH layer (password verified).
	// The PasswordHandler in ssh_server.go stashes the authenticated user in the
	// context when the SSH username matches a known BBS handle and the password
	// is correct. Unknown usernames are accepted without verification so the BBS
	// login menu can handle them.
	if authedUser, ok := s.Context().Value(sshAuthUserKey{}).(*user.User); ok && authedUser != nil {
		slog.Info("SSH pre-authenticated user detected", "node", nodeID, "user", authedUser.Handle)
		authenticatedUser = authedUser
		bbsSession.Mutex.Lock()
		bbsSession.User = authenticatedUser
		bbsSession.Mutex.Unlock()

		// Mark user as online
		userMgr.MarkUserOnline(authenticatedUser.ID)

		currentMenuName = "MAIN"
	}

	// Pre-login matrix screen for unauthenticated users (telnet or SSH without account)
	if authenticatedUser == nil {
		gateCfg := menuExecutor.GetServerConfig()
		if gateCfg.EnableChallengeGate && !connectionTracker.IsAllowlisted(extractIP(s.RemoteAddr())) {
			passed, gateErr := menuExecutor.RunChallengeGate(s, terminal, int(nodeID), effectiveMode, int(termWidth.Load()), int(termHeight.Load()))
			if gateErr != nil {
				slog.Info("challenge gate ended session", "node", nodeID, "error", gateErr)
				return
			}
			if !passed {
				slog.Info("challenge gate not passed, disconnecting", "node", nodeID)
				return
			}
		}

		matrixAction, matrixErr := menuExecutor.RunMatrixScreen(s, terminal, userMgr, int(nodeID), effectiveMode, int(termWidth.Load()), int(termHeight.Load()))
		if matrixErr != nil {
			slog.Error("matrix screen error", "node", nodeID, "error", matrixErr)
			return
		}
		if matrixAction == "DISCONNECT" {
			slog.Info("user selected disconnect from matrix", "node", nodeID)
			return
		}
		// matrixAction == "LOGIN" — proceed to normal login loop
	}

	// Login Loop
	for authenticatedUser == nil {
		if currentMenuName == "" || currentMenuName == "LOGOFF" {
			slog.Info("login failed or aborted", "node", nodeID)
			msg := loadedStrings.ExecLoginCancelled
			if msg == "" {
				msg = "\r\n|01Login cancelled.|07\r\n"
			}
			_ = terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), effectiveMode)
			return
		}

		// Execute the current menu (e.g., LOGIN)
		// Run returns the next menu name, the authenticated user (if successful), or an error.
		// Pass nodeID directly as int, use sessionStartTime from context
		// Pass the session's autoRunLog
		// Pass "" for currentAreaName during login
		nextMenuName, authUser, execErr := menuExecutor.Run(s, terminal, userMgr, nil, currentMenuName, int(nodeID), sessionStartTime, autoRunLog, effectiveMode, "", int(termWidth.Load()), int(termHeight.Load()))
		if execErr != nil {
			// Log the error and decide how to proceed
			slog.Error("error executing menu", "node", nodeID, "menu", currentMenuName, "error", execErr)
			// Optionally display an error message to the user
			_, _ = fmt.Fprintf(terminal, "\r\nSystem error during menu execution: %v\r\n", execErr) // best-effort display
			// Maybe force logoff or retry?
			currentMenuName = "LOGOFF" // Force logoff on error for now
			continue
		}

		// Check if authentication was successful during this menu execution
		if authUser != nil {
			authenticatedUser = authUser
			bbsSession.Mutex.Lock()
			bbsSession.User = authenticatedUser
			bbsSession.Mutex.Unlock()
			slog.Info("user authenticated", "node", nodeID, "user", authenticatedUser.Handle)

			// Mark user as online
			userMgr.MarkUserOnline(authenticatedUser.ID)

			// Login successful! Break out of login loop.

			// --- START MOVED Login Event Recording --- Removed call to AddLoginEvent
			/* // Removed LoginEvent tracking
			event := user.LoginEvent{
				Handle:    authenticatedUser.Handle,
				Timestamp: time.Now(), // Record time NOW
			}
			userMgr.AddLoginEvent(event)
			*/
			// --- END MOVED Login Event Recording ---

			break // Force exit from the login loop
		} else {
			// Authentication did not occur, proceed to the next menu in the login sequence
			currentMenuName = nextMenuName
		}
	} // End Login Loop

	slog.Debug("login loop completed")

	// --- Post-Authentication Main Loop ---
	// Safety check still useful here in case break logic fails somehow
	if authenticatedUser == nil {
		slog.Error("reached post-auth loop with nil user", "node", nodeID)
		return
	}
	// Set default message area if not already set (handles both SSH pre-auth and normal login)
	defaultsChanged := false
	if authenticatedUser.CurrentMessageAreaID == 0 && messageMgr != nil {
		for _, area := range messageMgr.ListAreas() {
			if menu.CheckUserACS(area.ACSRead, authenticatedUser) {
				authenticatedUser.CurrentMessageAreaID = area.ID
				authenticatedUser.CurrentMessageAreaTag = area.Tag
				authenticatedUser.CurrentMsgConferenceID = area.ConferenceID
				if confMgr != nil {
					if conf, ok := confMgr.GetByID(area.ConferenceID); ok {
						authenticatedUser.CurrentMsgConferenceTag = conf.Tag
					}
				}
				defaultsChanged = true
				break
			}
		}
	}
	// Set default file area if not already set
	if authenticatedUser.CurrentFileAreaID == 0 && fileMgr != nil {
		for _, area := range fileMgr.ListAreas() {
			if menu.CheckUserACS(area.ACSList, authenticatedUser) {
				authenticatedUser.CurrentFileAreaID = area.ID
				authenticatedUser.CurrentFileAreaTag = area.Tag
				authenticatedUser.CurrentFileConferenceID = area.ConferenceID
				if confMgr != nil {
					if conf, ok := confMgr.GetByID(area.ConferenceID); ok {
						authenticatedUser.CurrentFileConferenceTag = conf.Tag
					}
				}
				defaultsChanged = true
				break
			}
		}
	}
	// Persist defaults only if we actually assigned new values
	if defaultsChanged {
		if saveErr := userMgr.UpdateUser(authenticatedUser); saveErr != nil {
			slog.Error("failed to save user default area selections", "node", nodeID, "error", saveErr)
		}
	}

	slog.Info("entering main loop", "node", nodeID, "user", authenticatedUser.Handle)

	// --- Invisible Login Prompt (users at or above invisibleLevel) ---
	cfg := menuExecutor.GetServerConfig()
	invLevel := cfg.InvisibleLevel
	if invLevel == 0 {
		invLevel = cfg.CoSysOpLevel // backward compat: missing config uses coSysOpLevel
	}
	if authenticatedUser.AccessLevel >= invLevel {
		_, _ = terminal.Write([]byte("\x1b[2J\x1b[H")) // best-effort display
		invisPrompt := loadedStrings.InvisibleLogonPrompt
		if invisPrompt == "" {
			invisPrompt = " |03Invisible Logon?|07"
		}
		invisChoice, invisErr := menuExecutor.PromptYesNo(s, terminal,
			invisPrompt, effectiveMode, int(nodeID),
			int(termWidth.Load()), int(termHeight.Load()), false)
		if invisErr != nil {
			if errors.Is(invisErr, io.EOF) {
				slog.Info("user disconnected during invisible logon prompt", "node", nodeID)
				return
			}
			slog.Warn("error during invisible logon prompt", "node", nodeID, "error", invisErr)
		}
		// Prompt is "Add this login to Last Caller List?" — Yeah = add (visible), Nah = don't add (invisible).
		if !invisChoice {
			bbsSession.Mutex.Lock()
			bbsSession.Invisible = true
			bbsSession.Mutex.Unlock()
			slog.Info("user logged in as invisible", "node", nodeID, "user", authenticatedUser.Handle)
		}
	}

	// Determine effective terminal size based on user preferences and manual adjustments
	effectiveWidth := int(termWidth.Load())
	effectiveHeight := int(termHeight.Load())

	// Capture whether user needs first-time setup BEFORE auto-saving dimensions
	needsSetup := (authenticatedUser.ScreenWidth == 0 || authenticatedUser.ScreenHeight == 0 || authenticatedUser.PreferredEncoding == "")

	if authenticatedUser.ScreenWidth > 0 && authenticatedUser.ScreenHeight > 0 {
		detectedW := effectiveWidth
		detectedH := effectiveHeight

		if isPty && (detectedW != authenticatedUser.ScreenWidth || detectedH != authenticatedUser.ScreenHeight) {
			// Mismatch between detected and stored terminal size — prompt user
			slog.Info("terminal size mismatch",
				"node", nodeID,
				"detected_w", detectedW,
				"detected_h", detectedH,
				"stored_w", authenticatedUser.ScreenWidth,
				"stored_h", authenticatedUser.ScreenHeight)
			_, _ = terminal.Write([]byte("\r\n")) // best-effort display

			useNew, promptErr := menuExecutor.PromptYesNo(s, terminal,
				fmt.Sprintf(loadedStrings.TermSizeNewDetectedPrompt,
					detectedW, detectedH, authenticatedUser.ScreenWidth, authenticatedUser.ScreenHeight),
				effectiveMode, int(nodeID), detectedW, detectedH, false)
			if promptErr != nil {
				if errors.Is(promptErr, io.EOF) {
					slog.Info("user disconnected during terminal size prompt", "node", nodeID)
					return
				}
				slog.Warn("error during terminal size prompt", "node", nodeID, "error", promptErr)
			}

			if useNew {
				effectiveWidth = detectedW
				effectiveHeight = detectedH
				termWidth.Store(int32(detectedW))
				termHeight.Store(int32(detectedH))

				saveDefault, saveErr := menuExecutor.PromptYesNo(s, terminal,
					loadedStrings.TermSizeUpdateDefaultsPrompt,
					effectiveMode, int(nodeID), detectedW, detectedH, true)
				if saveErr != nil && errors.Is(saveErr, io.EOF) {
					slog.Info("user disconnected during save-defaults prompt", "node", nodeID)
					return
				}
				if saveDefault {
					authenticatedUser.ScreenWidth = detectedW
					authenticatedUser.ScreenHeight = detectedH
					if err := userMgr.UpdateUser(authenticatedUser); err != nil {
						slog.Warn("failed to update terminal size", "node", nodeID, "error", err)
					} else {
						slog.Info("updated default terminal size", "node", nodeID, "width", detectedW, "height", detectedH)
					}
				}
			} else {
				// Keep saved preferences for this session
				effectiveWidth = authenticatedUser.ScreenWidth
				effectiveHeight = authenticatedUser.ScreenHeight
				termWidth.Store(int32(effectiveWidth))
				termHeight.Store(int32(effectiveHeight))
			}
		} else {
			// Sizes match (or no PTY) — use saved preferences
			slog.Info("using stored terminal size",
				"node", nodeID,
				"stored_w", authenticatedUser.ScreenWidth,
				"stored_h", authenticatedUser.ScreenHeight,
				"pty_w", detectedW,
				"pty_h", detectedH)
			effectiveWidth = authenticatedUser.ScreenWidth
			effectiveHeight = authenticatedUser.ScreenHeight
			termWidth.Store(int32(effectiveWidth))
			termHeight.Store(int32(effectiveHeight))
		}
	} else {
		// No saved preferences and no manual adjustment - use PTY detected and save for next time
		slog.Info("no stored terminal size, using PTY detected", "node", nodeID, "width", effectiveWidth, "height", effectiveHeight)
		authenticatedUser.ScreenWidth = effectiveWidth
		authenticatedUser.ScreenHeight = effectiveHeight
		if err := userMgr.UpdateUser(authenticatedUser); err != nil {
			slog.Warn("failed to save terminal size", "node", nodeID, "error", err)
		}
	}

	_ = terminal.SetSize(effectiveWidth, effectiveHeight)
	slog.Info("effective terminal size", "node", nodeID, "user", authenticatedUser.Handle, "width", effectiveWidth, "height", effectiveHeight)

	// --- Post-Auth Terminal Setup Prompts ---
	// If user doesn't have saved preferences, prompt for encoding and terminal size configuration
	// needsSetup was captured above BEFORE auto-saving PTY dimensions, so it reflects the original state

	if needsSetup && isPty && outputModeFlag == "auto" {
		termType := strings.ToLower(ptyReq.Term)
		setupChanged := false

		// Encoding Selection Prompt (for ambiguous terminals like xterm)
		if effectiveMode == ansi.OutputModeUTF8 && termType == "xterm" && authenticatedUser.PreferredEncoding == "" {
			_, _ = terminal.Write([]byte("\r\n"))                                                                                                    // best-effort display
			_, _ = terminal.Write([]byte("\x1b[1;36m CHARACTER ENCODING SELECTION\x1b[0m\r\n"))                                                      // best-effort display
			_, _ = terminal.Write([]byte("\x1b[1;33m ----------------------------\x1b[0m\r\n"))                                                      // best-effort display
			_, _ = terminal.Write([]byte("\r\n"))                                                                                                    // best-effort display
			_, _ = terminal.Write([]byte("Your terminal reported as '\x1b[1m" + termType + "\x1b[0m' which can support multiple encodings.\r\n"))    // best-effort display
			_, _ = terminal.Write([]byte("\r\n"))                                                                                                    // best-effort display
			_, _ = terminal.Write([]byte("\x1b[1;32m[U]\x1b[0m Continue with \x1b[1mUTF-8\x1b[0m (modern terminals, Unicode support)\r\n"))          // best-effort display
			_, _ = terminal.Write([]byte("\x1b[1;32m[C]\x1b[0m Switch to \x1b[1mCP437\x1b[0m (retro BBS terminals: SyncTerm, NetRunner, etc.)\r\n")) // best-effort display
			_, _ = terminal.Write([]byte("\r\n"))                                                                                                    // best-effort display
			_, _ = terminal.Write([]byte("Choice \x1b[1;33m[U/C]\x1b[0m: "))                                                                         // best-effort display

			choice, err := terminal.ReadLine()
			if err == nil {
				choice = strings.TrimSpace(strings.ToUpper(choice))
				if choice == "C" || choice == "CP437" {
					slog.Info("user selected CP437 encoding", "node", nodeID)
					effectiveMode = ansi.OutputModeCP437
					authenticatedUser.PreferredEncoding = "cp437"
					setupChanged = true
					_, _ = terminal.Write([]byte("\r\n\x1b[1;32m[OK]\x1b[0m Switched to CP437 encoding for retro BBS experience.\r\n")) // best-effort display
				} else {
					slog.Info("user selected UTF-8 encoding", "node", nodeID)
					authenticatedUser.PreferredEncoding = "utf8"
					setupChanged = true
					_, _ = terminal.Write([]byte("\r\n\x1b[1;32m[OK]\x1b[0m Continuing with UTF-8 encoding.\r\n")) // best-effort display
				}
			}
		}

		// Terminal Height Adjustment Prompt
		detectedHeight := int(termHeight.Load())
		if detectedHeight > 25 && (authenticatedUser.ScreenWidth == 0 || authenticatedUser.ScreenHeight == 0) {
			_, _ = terminal.Write([]byte("\r\n"))                                                                                                     // best-effort display
			_, _ = terminal.Write([]byte("Your terminal reports \x1b[1m" + fmt.Sprintf("%d", detectedHeight) + " rows\x1b[0m.\r\n"))                  // best-effort display
			_, _ = terminal.Write([]byte("If you have a status bar enabled (NetRunner, SyncTerm, etc.),\r\n"))                                        // best-effort display
			_, _ = terminal.Write([]byte("some rows may not be available for display.\r\n"))                                                          // best-effort display
			_, _ = terminal.Write([]byte("\r\n"))                                                                                                     // best-effort display
			_, _ = terminal.Write([]byte("How many rows are available for BBS display? [\x1b[1m" + fmt.Sprintf("%d", detectedHeight) + "\x1b[0m]: ")) // best-effort display

			heightChoice, heightErr := terminal.ReadLine()
			if heightErr != nil {
				if errors.Is(heightErr, io.EOF) {
					slog.Info("user disconnected during height adjustment prompt", "node", nodeID)
					return
				}
				slog.Warn("error during height adjustment prompt", "node", nodeID, "error", heightErr)
			} else {
				heightChoice = strings.TrimSpace(heightChoice)
				if heightChoice != "" {
					if adjustedHeight, parseErr := strconv.Atoi(heightChoice); parseErr == nil && adjustedHeight >= 20 && adjustedHeight <= detectedHeight {
						slog.Info("user adjusted terminal height", "node", nodeID, "from", detectedHeight, "to", adjustedHeight)
						effectiveHeight = adjustedHeight
						termHeight.Store(int32(adjustedHeight))
						authenticatedUser.ScreenHeight = adjustedHeight
						setupChanged = true
						_ = terminal.SetSize(int(termWidth.Load()), adjustedHeight)
						_, _ = terminal.Write([]byte("\r\n\x1b[1;32m[OK]\x1b[0m Display height set to " + fmt.Sprintf("%d", adjustedHeight) + " rows.\r\n")) // best-effort display
					}
				}
			}
		}

		// Ask to save as default if anything changed
		if setupChanged {
			_, _ = terminal.Write([]byte("\r\n"))                                                                     // best-effort display
			_, _ = terminal.Write([]byte("Save these settings as your default preference? \x1b[1;33m[Y/n]\x1b[0m: ")) // best-effort display
			saveChoice, saveErr := terminal.ReadLine()
			if saveErr == nil {
				saveChoice = strings.TrimSpace(strings.ToUpper(saveChoice))
				if saveChoice == "" || saveChoice == "Y" || saveChoice == "YES" {
					if err := userMgr.UpdateUser(authenticatedUser); err != nil {
						slog.Warn("failed to save user preferences", "node", nodeID, "error", err)
						_, _ = terminal.Write([]byte("\r\n\x1b[1;33m[WARN]\x1b[0m Failed to save preferences.\r\n")) // best-effort display
					} else {
						slog.Info("saved user preferences",
							"node", nodeID,
							"encoding", authenticatedUser.PreferredEncoding,
							"width", authenticatedUser.ScreenWidth,
							"height", authenticatedUser.ScreenHeight)
						_, _ = terminal.Write([]byte("\r\n\x1b[1;32m[SAVED]\x1b[0m Your preferences have been saved.\r\n")) // best-effort display
					}
				} else {
					slog.Info("user declined to save preferences", "node", nodeID)
					_, _ = terminal.Write([]byte("\r\n\x1b[1;36m[INFO]\x1b[0m Settings will be used for this session only.\r\n")) // best-effort display
				}
			}
			_, _ = terminal.Write([]byte("\r\n")) // best-effort display
		}
	} else if authenticatedUser.PreferredEncoding != "" {
		// User has saved encoding preference - apply it
		switch authenticatedUser.PreferredEncoding {
		case "cp437":
			effectiveMode = ansi.OutputModeCP437
			slog.Info("using saved encoding preference", "node", nodeID, "encoding", "cp437")
		case "utf8":
			effectiveMode = ansi.OutputModeUTF8
			slog.Info("using saved encoding preference", "node", nodeID, "encoding", "utf8")
		}
	}

	// Run the configurable login sequence (login.json) directly after authentication.
	// This replaces the old FASTLOGN menu routing — FASTLOGIN is now an optional login.json item.
	loginNextMenu, loginErr := menuExecutor.RunLoginSequence(s, terminal, userMgr, authenticatedUser, int(nodeID), sessionStartTime, effectiveMode, int(termWidth.Load()), int(termHeight.Load()))
	if loginErr != nil {
		if errors.Is(loginErr, io.EOF) {
			slog.Info("user disconnected during login sequence", "node", nodeID)
			return
		}
		slog.Error("login sequence error", "node", nodeID, "error", loginErr)
		if loginNextMenu == "" {
			currentMenuName = "LOGOFF"
		} else {
			currentMenuName = loginNextMenu
		}
	} else {
		currentMenuName = loginNextMenu
	}

	// V3Net logon notification
	if v3netService != nil && authenticatedUser != nil {
		v3netService.SendLogon(authenticatedUser.Handle)
	}

	for {
		if currentMenuName == "" || currentMenuName == "LOGOFF" {
			slog.Info("user selected logoff", "node", nodeID, "user", authenticatedUser.Handle)
			_, _ = fmt.Fprintln(terminal, "\r\nLogging off...") // best-effort display
			// Add any cleanup tasks before closing the session
			break // Exit the loop
		}

		// *** ADD LOGGING HERE ***
		slog.Debug("entering main loop iteration", "node", nodeID, "menu", currentMenuName, "output_mode", effectiveMode)

		// Update session state for who's online tracking
		bbsSession.Mutex.Lock()
		bbsSession.CurrentMenu = currentMenuName
		bbsSession.LastActivity = time.Now()
		bbsSession.Mutex.Unlock()

		// Execute the current menu (e.g., MAIN, READ_MSG, etc.)
		// Pass nodeID directly as int, use sessionStartTime from context
		// Pass the session's autoRunLog
		// Pass "" for currentAreaName for now (TODO: Pass actual session area name)
		nextMenuName, _, execErr := menuExecutor.Run(s, terminal, userMgr, authenticatedUser, currentMenuName, int(nodeID), sessionStartTime, autoRunLog, effectiveMode, "", int(termWidth.Load()), int(termHeight.Load()))
		if execErr != nil {
			slog.Error("error executing menu", "node", nodeID, "menu", currentMenuName, "error", execErr)
			_, _ = fmt.Fprintf(terminal, "\r\nSystem error during menu execution: %v\r\n", execErr) // best-effort display
			// Logoff on error?
			currentMenuName = "LOGOFF"
			continue
		}

		// Move to the next menu determined by the user's action in the previous menu
		currentMenuName = nextMenuName
	}

	slog.Info("session handler finished", "node", nodeID, "user", authenticatedUser.Handle)
}

// telnetSessionHandler adapts telnet sessions to the existing BBS session handler
func telnetSessionHandler(adapter *telnetserver.TelnetSessionAdapter) {
	// Atomically check limits and register connection
	canAccept, reason := connectionTracker.TryAccept(adapter.RemoteAddr())
	if !canAccept {
		slog.Info("rejecting telnet connection", "addr", adapter.RemoteAddr(), "reason", reason)
		_, _ = fmt.Fprintf(adapter, "\r\nConnection rejected: %s\r\n", reason) // best-effort display
		_, _ = fmt.Fprintf(adapter, "Please try again later.\r\n")             // best-effort display
		time.Sleep(2 * time.Second)                                            // Brief delay before closing
		return
	}

	// Connection is registered; ensure it's removed when done
	defer connectionTracker.RemoveConnection(adapter.RemoteAddr())

	sessionHandler(adapter)
}

// --- Test Functions REMOVED ---

// --- Main Function --- //
func main() {
	// Define and parse the --colortest flag REMOVED
	// flag.BoolVar(&colorTestMode, "colortest", false, "Run ANSI color test mode instead of BBS")
	// Define output mode flag
	flag.StringVar(&outputModeFlag, "output-mode", "auto", "Terminal output mode: auto (default), utf8, cp437")
	flag.Parse()

	// Validate output mode flag
	outputModeFlag = strings.ToLower(outputModeFlag)
	if outputModeFlag != "auto" && outputModeFlag != "utf8" && outputModeFlag != "cp437" {
		logging.Fatal("invalid --output-mode value", "value", outputModeFlag, "valid", "auto, utf8, cp437")
	}
	slog.Info("output mode set", "mode", outputModeFlag)

	// --- Run Normal BBS Server --- //
	var err error
	fmt.Println("Starting ViSiON/3 BBS...") // Changed startup message

	// Determine base paths
	basePath, err := os.Getwd() // Or use a more robust method if needed
	if err != nil {
		logging.Fatal("failed to get working directory", "error", err)
	}
	menuSetPath := filepath.Join(basePath, "menus", "v3") // Default menu set
	rootConfigPath := filepath.Join(basePath, "configs")
	rootAssetsPath := filepath.Join(basePath, "assets") // Keep this path definition for now
	dataPath := filepath.Join(basePath, "data")         // For user data, logs, etc.
	userDataPath := filepath.Join(dataPath, "users")

	// Pre-flight: ensure setup has been run and all required files/dirs exist
	if !runPreflight(basePath) {
		os.Exit(1)
	}

	// Load server configuration (needed before logging init, which reads the
	// shared logging settings from it).
	serverConfig, err := config.LoadServerConfig(rootConfigPath)
	if err != nil {
		logging.Fatal("failed to load server configuration", "error", err)
	}

	// Initialize logging: a configurable rolling file plus console echo. This
	// also bridges stdlib log.Printf to the same destinations until Phase B
	// migrates those call sites to slog.
	_, closeLog, err := logging.Init(serverConfig.Logging, "vision3.log", true)
	if err != nil {
		logging.Fatal("failed to initialize logging", "error", err)
	}
	defer func() { _ = closeLog() }() // best-effort log flush at exit
	slog.Info("starting ViSiON/3 BBS server")

	// Initialize connection tracker with configured limits and IP filter file paths
	// This will load the initial lists and start watching for file changes
	connectionTracker = NewConnectionTracker(
		serverConfig.MaxNodes,
		serverConfig.MaxConnectionsPerIP,
		serverConfig.MaxFailedLogins,
		serverConfig.LockoutMinutes,
		serverConfig.IPBlocklistPath,
		serverConfig.IPAllowlistPath,
	)
	defer connectionTracker.StopWatching() // Ensure file watcher is stopped on shutdown

	slog.Info("connection security configured",
		"max_nodes", serverConfig.MaxNodes,
		"max_connections_per_ip", serverConfig.MaxConnectionsPerIP,
		"max_failed_logins", serverConfig.MaxFailedLogins,
		"lockout_min", serverConfig.LockoutMinutes)

	// Log IP filter status
	if serverConfig.IPBlocklistPath != "" {
		slog.Info("IP blocklist enabled", "file", serverConfig.IPBlocklistPath)
	}
	if serverConfig.IPAllowlistPath != "" {
		slog.Info("IP allowlist enabled", "file", serverConfig.IPAllowlistPath)
	}

	// Load global strings configuration from the new location
	loadedStrings, err = config.LoadStrings(rootConfigPath)
	if err != nil {
		logging.Fatal("failed to load strings configuration", "error", err)
	}

	// Load theme configuration from the menu set path
	loadedTheme, err = config.LoadThemeConfig(menuSetPath)
	if err != nil {
		slog.Warn("proceeding with default theme", "file", filepath.Join(menuSetPath, "theme.json"), "error", err)
	}

	// Load door configurations from the new location
	loadedDoors, err := config.LoadDoors(filepath.Join(rootConfigPath, "doors.json")) // Expects full path
	if err != nil {
		logging.Fatal("failed to load door configuration", "error", err)
	}

	// Load FTN configuration early so message manager can use per-network tearlines.
	ftnConfig, ftnErr := config.LoadFTNConfig(rootConfigPath)
	if ftnErr != nil {
		slog.Error("failed to load FTN config, echomail disabled", "error", ftnErr)
	}
	networkTearlines := make(map[string]string)
	if ftnErr == nil {
		for name, netCfg := range ftnConfig.Networks {
			if strings.TrimSpace(netCfg.Tearline) == "" {
				continue
			}
			networkTearlines[strings.ToLower(strings.TrimSpace(name))] = netCfg.Tearline
		}
	}
	if len(networkTearlines) == 0 {
		networkTearlines = nil
	}

	// Oneliners are loaded by the runnable; start with an empty list here.
	oneliners := []string{}

	// Initialize UserManager (using dataPath)
	userMgr, err = user.NewUserManager(userDataPath) // Pass the directory for users.json
	if err != nil {
		logging.Fatal("failed to initialize user manager", "error", err)
	}
	// Set the new user level from config
	userMgr.SetNewUserLevel(serverConfig.NewUserLevel)

	// Initialize MessageManager (areas config from configs/, message data from data/)
	messageMgr, err = message.NewMessageManager(dataPath, rootConfigPath, serverConfig.BoardName, networkTearlines)
	if err != nil {
		logging.Fatal("failed to initialize message manager", "error", err)
	}
	defer func() {
		if cerr := messageMgr.Close(); cerr != nil {
			slog.Error("closing JAM message bases on shutdown", "error", cerr)
		}
	}()

	// Initialize FileManager (using dataPath)
	fileMgr, err = file.NewFileManager(dataPath, rootConfigPath)
	if err != nil {
		logging.Fatal("failed to initialize file manager", "error", err)
	}

	// Initialize ConferenceManager (non-fatal if conferences.json is missing)
	confMgr, err = conference.NewConferenceManager(rootConfigPath)
	if err != nil {
		slog.Warn("failed to initialize conference manager, conferences disabled", "error", err)
		confMgr = nil
	}

	// Load login sequence configuration
	loginSequence, err := config.LoadLoginSequence(rootConfigPath)
	if err != nil {
		logging.Fatal("failed to load login sequence configuration", "error", err)
	}

	// Initialize session registry for who's online tracking
	sessionRegistry = session.NewSessionRegistry()

	// Initialize and start the WFC admin server.
	// adminMinLevel is a live getter so config hot-reloads take effect immediately.
	adminMinLevel = func() int { return menuExecutor.GetServerConfig().CoSysOpLevel }
	wfcEnabled = func() bool { return menuExecutor.GetServerConfig().WFCEnabled }
	adminServer = admin.NewServer(admin.ServerConfig{
		Reg:        sessionRegistry,
		SystemName: serverConfig.BoardName,
		StartedAt:  time.Now(),
		Refresh:    time.Second,
		MaxEvents:  200,
		CallsToday: func() int { return -1 },
	})
	go adminServer.Run(context.Background())

	// Load transfer protocol configuration
	var loadedProtocols []transfer.ProtocolConfig
	protocolsPath := filepath.Join(rootConfigPath, "protocols.json")
	if protocols, err := transfer.LoadProtocols(protocolsPath); err != nil {
		slog.Warn("failed to load protocols, file transfers unavailable", "error", err)
	} else {
		loadedProtocols = protocols
		slog.Info("loaded transfer protocols", "count", len(loadedProtocols), "file", protocolsPath)
	}

	// Initialize MenuExecutor with new paths, loaded theme, server config, message manager, and connection tracker
	serverConfig.DataDir = dataPath
	menuExecutor = menu.NewExecutor(menuSetPath, rootConfigPath, rootAssetsPath, oneliners, loadedDoors, loadedStrings, loadedTheme, serverConfig, messageMgr, fileMgr, confMgr, connectionTracker, loginSequence, sessionRegistry, loadedProtocols)

	// Initialize configuration file watcher for hot reload
	var serverConfigMu sync.RWMutex
	configWatcher, err := NewConfigWatcher(rootConfigPath, menuSetPath, menuExecutor, userMgr, &serverConfig, &serverConfigMu)
	if err != nil {
		slog.Warn("failed to start config file watcher, hot reload disabled", "error", err)
	} else {
		defer configWatcher.Stop()
		slog.Info("configuration hot reload enabled")
	}

	if ftnErr == nil && len(ftnConfig.Networks) > 0 && !ftnConfig.Binkd.Enabled {
		slog.Info("internal FTN tosser disabled, use v3mail for toss/scan or enable the binkd mailer")
	}

	// Load event scheduler configuration
	eventsConfig, eventsErr := config.LoadEventsConfig(rootConfigPath)
	if eventsErr != nil {
		slog.Warn("failed to load events config", "error", eventsErr)
		eventsConfig = config.EventsConfig{Enabled: false}
	}

	// Start event scheduler if enabled
	var eventScheduler *scheduler.Scheduler
	var schedulerCtx context.Context
	var schedulerCancel context.CancelFunc
	if eventsConfig.Enabled {
		historyPath := filepath.Join(dataPath, "logs", "event_history.json")
		eventScheduler = scheduler.NewScheduler(eventsConfig, historyPath)
		schedulerCtx, schedulerCancel = context.WithCancel(context.Background())
		defer func() {
			if schedulerCancel != nil {
				slog.Info("shutting down event scheduler")
				schedulerCancel()
			}
		}()

		go eventScheduler.Start(schedulerCtx)
		slog.Info("event scheduler started", "count", len(eventsConfig.Events))
	} else {
		slog.Info("event scheduler disabled")
	}

	// Load V3Net configuration from v3net.json (separate from main config, like ftn.json).
	v3netConfig, v3netCfgErr := config.LoadV3NetConfig(rootConfigPath)
	if v3netCfgErr != nil {
		slog.Error("failed to load V3Net config", "error", v3netCfgErr)
	}
	if v3netCfgErr == nil {
		v3netConfig.ConfigPath = rootConfigPath
	}

	// Start V3Net networking service if enabled
	if v3netCfgErr == nil && v3netConfig.Enabled {
		svc, v3err := v3net.New(v3netConfig)
		if v3err != nil {
			slog.Error("V3Net initialization failed", "error", v3err)
		} else {
			svc.BBSName = serverConfig.BoardName
			svc.BBSHost = serverConfig.SSHHost
			v3netService = svc

			// Auto-create message areas for V3Net subscriptions if missing.
			v3net.SyncAreas(v3netConfig.Leaves, messageMgr, confMgr)

			// Configure leaf clients for each subscribed network.
			type v3netAreaInfo struct {
				Network string
				Origin  string
			}
			v3netAreaMap := make(map[int]v3netAreaInfo) // area ID → network info
			nodeID := svc.NodeID()
			for _, lcfg := range v3netConfig.Leaves {
				router := v3net.NewJAMRouter()
				origin := lcfg.Origin
				if origin == "" {
					origin = serverConfig.BoardName
				}
				var resolvedBoards []string
				for _, tag := range lcfg.Boards {
					area, ok := messageMgr.GetAreaByTag(tag)
					if !ok {
						slog.Warn("V3Net leaf: message area not found, skipping", "network", lcfg.Network, "area", tag)
						continue
					}
					router.Add(tag, v3net.NewJAMAdapter(messageMgr, area.ID))
					resolvedBoards = append(resolvedBoards, tag)
					v3netAreaMap[area.ID] = v3netAreaInfo{Network: lcfg.Network, Origin: origin}
					svc.RegisterArea(area.ID, lcfg.Network)
				}
				if len(resolvedBoards) == 0 {
					slog.Warn("V3Net leaf: no resolvable boards, skipping", "network", lcfg.Network)
					continue
				}
				leafCfg := lcfg
				leafCfg.Boards = resolvedBoards
				if err := v3netService.AddLeaf(leafCfg, router, nil); err != nil {
					slog.Error("V3Net leaf error", "network", lcfg.Network, "error", err)
					continue
				}
			}

			// Append tearline/origin to local JAM copy for V3Net areas so
			// the user sees the origin on locally-created messages too.
			messageMgr.BodyTransform = func(areaID int, body string) string {
				info, ok := v3netAreaMap[areaID]
				if !ok {
					return body
				}
				// Skip if the body already contains a tearline (e.g. inbound
				// V3Net imports that were written by JAMAdapter with the
				// remote tearline/origin already appended).
				if strings.Contains(body, "\n--- ") || strings.HasPrefix(body, "--- ") {
					return body
				}
				return v3net.AppendV3NetOrigin(body, v3net.DefaultTearline(), info.Origin, nodeID)
			}

			// Hook message posts to forward to V3Net when posted to a networked area.
			// Skip messages imported from V3Net (contain the UUID kludge) to prevent feedback loops.
			messageMgr.OnMessagePosted = func(area *message.MessageArea, msgNum int, from, to, subject, body string) {
				info, ok := v3netAreaMap[area.ID]
				if !ok {
					return
				}
				if strings.HasPrefix(body, "\x01V3NETUUID: ") {
					return
				}
				msg := v3net.BuildWireMessage(info.Network, area.Tag, svc.NodeID(), serverConfig.BoardName, from, to, subject, body, info.Origin)
				if err := svc.SendMessage(info.Network, msg); err != nil {
					slog.Error("V3Net: failed to send message", "network", info.Network, "error", err)
					return
				}
				// Mark the local JAM copy as sent so the reader shows "V3NET SENT".
				if err := messageMgr.MarkMessageSent(area.ID, msgNum); err != nil {
					slog.Warn("V3Net: failed to mark message as sent", "msg", msgNum, "error", err)
				}
			}

			v3netCtx, v3netCancel := context.WithCancel(context.Background())
			defer func() {
				slog.Info("shutting down V3Net service")
				v3netCancel()
				if cerr := v3netService.Close(); cerr != nil {
					slog.Error("closing V3Net service", "error", cerr)
				}
			}()

			go v3netService.Start(v3netCtx)
			menuExecutor.V3NetStatus = v3netService
			menuExecutor.ChatLeaves = v3netChatProvider(v3netService)
			slog.Info("V3Net service started",
				"node_id", v3netService.NodeID(),
				"hub", v3netService.HubActive(),
				"leaves", v3netService.LeafCount())
		}
	} else if v3netCfgErr == nil {
		slog.Info("V3Net networking disabled")
	}

	// Start the integrated binkd mailer if enabled (configs/ftn.json "binkd").
	// Failures are warnings: the BBS must come up even if the mailer can't.
	if ftnErr == nil && ftnConfig.Binkd.Enabled {
		mailerFTN := ftnConfig
		mailerFTN.ResolvePaths(basePath)
		mailerSvc, mErr := mailer.New(mailer.Config{
			BBSRoot: basePath,
			FTN:     mailerFTN,
			MsgMgr:  messageMgr,
		})
		if mErr != nil {
			slog.Warn("binkd mailer disabled", "error", mErr)
		} else {
			mailerCtx, mailerCancel := context.WithCancel(context.Background())
			go mailerSvc.Start(mailerCtx)
			defer func() {
				slog.Info("shutting down binkd mailer")
				mailerCancel()
				if err := mailerSvc.Close(); err != nil {
					slog.Error("binkd mailer shutdown", "error", err)
				}
			}()
			slog.Info("binkd mailer enabled", "port", ftnConfig.Binkd.Port)
		}
	}

	// Ensure at least one protocol is enabled
	if !serverConfig.SSHEnabled && !serverConfig.TelnetEnabled {
		logging.Fatal("neither SSH nor Telnet is enabled in config")
	}

	// Start SSH server if enabled
	if serverConfig.SSHEnabled {
		hostKeyPath := filepath.Join(rootConfigPath, "ssh_host_rsa_key")
		if _, err := os.Stat(hostKeyPath); err != nil {
			logging.Fatal("host key not found", "path", hostKeyPath, "error", err)
		}
		slog.Info("host key found", "path", hostKeyPath)

		cleanup, err := startSSHServer(
			hostKeyPath,
			serverConfig.SSHHost,
			serverConfig.SSHPort,
			serverConfig.LegacySSHAlgorithms,
		)
		if err != nil {
			logging.Fatal("failed to start SSH server", "error", err)
		}
		defer cleanup()
	} else {
		slog.Info("SSH server disabled")
	}

	// Start telnet server if enabled
	if serverConfig.TelnetEnabled {
		telnetPort := serverConfig.TelnetPort
		telnetHost := serverConfig.TelnetHost
		slog.Info("configuring telnet server", "host", telnetHost, "port", telnetPort)

		telnetSrv, telnetErr := telnetserver.NewServer(telnetserver.Config{
			Port:           telnetPort,
			Host:           telnetHost,
			SessionHandler: telnetSessionHandler,
		})
		if telnetErr != nil {
			logging.Fatal("failed to create telnet server", "error", telnetErr)
		}
		defer func() { _ = telnetSrv.Close() }() // best-effort shutdown

		go func() {
			if listenErr := telnetSrv.ListenAndServe(); listenErr != nil {
				slog.Error("telnet server error", "error", listenErr)
			}
		}()

		slog.Info("telnet server ready", "host", telnetHost, "port", telnetPort)
	} else {
		slog.Info("telnet server disabled")
	}

	// Start QWK packet API if enabled (Phase 7 — experimental).
	if serverConfig.QWKAPI.Enabled {
		qwkSvc := qwkservice.New(messageMgr, menu.ResolveQWKID(serverConfig), serverConfig.BoardName, serverConfig.SysOpName, messageMgr.DataPath())
		apiSrv, apiErr := qwkapi.NewServer(qwkapi.Deps{
			Config:       serverConfig.QWKAPI,
			ConfigDir:    rootConfigPath,
			Users:        userMgr,
			Service:      qwkSvc,
			AuthorizeFor: menu.QWKWriteAuthorizer,
		})
		if apiErr != nil {
			logging.Fatal("failed to create QWK API server", "error", apiErr)
		}
		go func() {
			if err := apiSrv.Start(); err != nil {
				slog.Error("QWK API server error", "error", err)
			}
		}()
		defer func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = apiSrv.Shutdown(ctx)
		}()
		slog.Info("QWK API enabled (experimental)", "addr", serverConfig.QWKAPI.ListenAddr())
	} else {
		slog.Info("QWK API disabled")
	}

	// Wait for a shutdown signal. SSH and telnet servers run in background
	// goroutines; blocking here (rather than `select {}`) lets main() return
	// on SIGINT/SIGTERM so all deferred cleanup runs (mailer stop, scheduler
	// cancel, messageMgr close, etc.) instead of leaving them unreachable.
	slog.Info("Vision/3 BBS running")
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	sig := <-sigCh
	slog.Info("shutdown signal received, stopping", "signal", sig.String())
}

// v3netChatProvider creates a menu.ChatLeafProvider from the V3Net service.
func v3netChatProvider(svc *v3net.Service) menu.ChatLeafProvider {
	return &chatLeafAdapter{svc: svc}
}

type chatLeafAdapter struct {
	svc *v3net.Service
}

func (a *chatLeafAdapter) ActiveChatLeaves() []menu.ChatLeafInfo {
	var infos []menu.ChatLeafInfo
	for _, l := range a.svc.Leaves() {
		lCopy := l // capture for closure
		infos = append(infos, menu.ChatLeafInfo{
			NetworkName: lCopy.Network(),
			NewSession: func(handle string) chat.ChatService {
				return lCopy.NewChatSession(handle)
			},
		})
	}
	return infos
}
