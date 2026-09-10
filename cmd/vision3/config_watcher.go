package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/file"
	"github.com/ViSiON-3/vision-3-bbs/internal/logging"
	"github.com/ViSiON-3/vision-3-bbs/internal/menu"
	"github.com/ViSiON-3/vision-3-bbs/internal/scheduler"
	"github.com/ViSiON-3/vision-3-bbs/internal/transfer"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// defaultPollInterval is how often configuration files are checked for
// modification. Matches Synchronet's DEFAULT_SEM_CHK_FREQ.
const defaultPollInterval = 2 * time.Second

// reloadTarget pairs a watched file with the action that re-reads it.
type reloadTarget struct {
	name   string // basename, for logging
	path   string
	reload func()
}

// ConfigWatcher polls configuration files for modification and hot-reloads
// the ones that changed.
//
// It polls modification times rather than subscribing to filesystem events.
// That is a deliberate choice, not a limitation:
//
//   - Polling is level-triggered. A tool that rewrites several config files in
//     quick succession produces one burst of events but leaves every file with
//     a new timestamp, so a single poll notices all of them. An event-driven
//     watcher has to debounce, and a debounce that collapses a burst into one
//     notification necessarily discards every filename but one (issue #320).
//   - It is self-healing. Reading a file mid-write yields a parse error, the
//     reload is skipped, and the old config stays in place; the writer's final
//     timestamp differs from the one recorded for the failed attempt, so the
//     next poll retries and succeeds. A missed event has no such recovery.
//   - A sysop can trigger a reload with `touch`, and any tool can signal one
//     without linking a file-watching library.
//
// This mirrors Synchronet's semfile mechanism (src/xpdev/semfile.c), including
// treating the config files themselves as part of the signal rather than
// maintaining a separate watch path for them.
type ConfigWatcher struct {
	rootConfigPath string
	menuSetPath    string
	menuExecutor   *menu.MenuExecutor
	userMgr        *user.UserMgr
	serverConfig   *config.ServerConfig
	serverConfigMu *sync.RWMutex // External mutex for server config
	connTracker    *ConnectionTracker
	scheduler      *scheduler.Scheduler // set via SetScheduler after startup; nil until then

	interval          time.Duration
	targets           []reloadTarget
	deferredTargets   []deferredTarget
	sentinelPath      string
	forceSentinelPath string

	mu      sync.Mutex           // guards mtimes, pending, and stop
	mtimes  map[string]time.Time // last-seen modification time per polled path
	pending map[string]bool      // deferred targets queued for the next idle window
	stop    chan struct{}
	done    chan struct{}
}

// deferredTarget is a watched file whose reload is structural: applying it
// with callers online can pull state out from under a session, so a detected
// change is validated immediately (the sysop hears about a bad file at save
// time, not hours later) but applied only once the session registry reports
// zero active sessions — or when the force sentinel demands it.
type deferredTarget struct {
	name     string // basename, for logging and pending listings
	path     string
	validate func() error // parse-only check, run at signal time
	apply    func() error // the actual reload, run at the idle window
}

// NewConfigWatcher creates a configuration file watcher and starts polling.
func NewConfigWatcher(rootConfigPath, menuSetPath string, menuExecutor *menu.MenuExecutor, userMgr *user.UserMgr, serverConfig *config.ServerConfig, serverConfigMu *sync.RWMutex, connTracker *ConnectionTracker) (*ConfigWatcher, error) {
	// Polling a directory that does not exist would silently never fire, so
	// fail loudly instead of starting a watcher that can never do anything.
	info, err := os.Stat(rootConfigPath)
	if err != nil {
		return nil, fmt.Errorf("cannot watch config directory %s: %w", rootConfigPath, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("config path %s is not a directory", rootConfigPath)
	}

	cw := &ConfigWatcher{
		rootConfigPath:    rootConfigPath,
		menuSetPath:       menuSetPath,
		menuExecutor:      menuExecutor,
		userMgr:           userMgr,
		serverConfig:      serverConfig,
		serverConfigMu:    serverConfigMu,
		connTracker:       connTracker,
		interval:          defaultPollInterval,
		sentinelPath:      config.ReloadSentinelPath(rootConfigPath),
		forceSentinelPath: config.ReloadForceSentinelPath(rootConfigPath),
		mtimes:            make(map[string]time.Time),
		pending:           make(map[string]bool),
		stop:              make(chan struct{}),
		done:              make(chan struct{}),
	}

	cw.targets = []reloadTarget{
		{name: "config.json", path: filepath.Join(rootConfigPath, "config.json"), reload: cw.reloadServerConfig},
		{name: "doors.json", path: filepath.Join(rootConfigPath, "doors.json"), reload: cw.reloadDoors},
		{name: "login.json", path: filepath.Join(rootConfigPath, "login.json"), reload: cw.reloadLoginSequence},
		{name: "strings.json", path: filepath.Join(rootConfigPath, "strings.json"), reload: cw.reloadStrings},
		{name: "theme.json", path: filepath.Join(menuSetPath, "theme.json"), reload: cw.reloadTheme},
		{name: "protocols.json", path: filepath.Join(rootConfigPath, "protocols.json"), reload: cw.reloadProtocols},
		{name: "events.json", path: filepath.Join(rootConfigPath, "events.json"), reload: cw.reloadEvents},
		{name: "conferences.json", path: filepath.Join(rootConfigPath, "conferences.json"), reload: cw.reloadConferences},
	}
	cw.deferredTargets = []deferredTarget{
		{
			name:     "file_areas.json",
			path:     filepath.Join(rootConfigPath, "file_areas.json"),
			validate: cw.validateFileAreas,
			apply:    cw.applyFileAreas,
		},
	}

	// Record current timestamps so the first poll does not reload everything
	// that already loaded cleanly at startup.
	cw.seed()

	go cw.pollLoop()

	slog.Info("watching for config changes",
		"path", rootConfigPath, "sentinel", cw.sentinelPath, "interval", cw.interval)
	return cw, nil
}

// seed records the current modification time of every polled file without
// triggering a reload.
func (cw *ConfigWatcher) seed() {
	cw.mu.Lock()
	defer cw.mu.Unlock()

	paths := make([]string, 0, len(cw.targets)+len(cw.deferredTargets)+2)
	paths = append(paths, cw.sentinelPath, cw.forceSentinelPath)
	for _, t := range cw.targets {
		paths = append(paths, t.path)
	}
	for _, t := range cw.deferredTargets {
		paths = append(paths, t.path)
	}
	for _, p := range paths {
		if info, err := os.Stat(p); err == nil {
			cw.mtimes[p] = info.ModTime()
		}
	}
}

// Stop halts polling. It is safe to call more than once.
func (cw *ConfigWatcher) Stop() {
	cw.mu.Lock()
	select {
	case <-cw.stop:
		cw.mu.Unlock()
		return // already stopped
	default:
		close(cw.stop)
	}
	cw.mu.Unlock()

	<-cw.done
	slog.Info("configuration file watcher stopped")
}

// pollLoop checks for modified configuration files until Stop is called.
func (cw *ConfigWatcher) pollLoop() {
	defer close(cw.done)

	ticker := time.NewTicker(cw.interval)
	defer ticker.Stop()

	for {
		select {
		case <-cw.stop:
			slog.Info("stopping config file watcher")
			return
		case <-ticker.C:
			cw.poll()
		}
	}
}

// poll reloads every configuration file whose timestamp changed since the last
// check. A touched sentinel reloads everything regardless of timestamps.
func (cw *ConfigWatcher) poll() {
	if cw.changed(cw.forceSentinelPath) {
		slog.Warn("force sentinel touched: applying all configuration now, including deferred structural changes",
			"path", cw.forceSentinelPath, "active_sessions", cw.activeSessions())
		for _, t := range cw.targets {
			cw.changed(t.path)
		}
		cw.ReloadAll()
		for _, t := range cw.deferredTargets {
			cw.changed(t.path)
			cw.applyDeferred(t)
		}
		return
	}

	if cw.changed(cw.sentinelPath) {
		slog.Info("reload sentinel touched, reloading all configuration",
			"path", cw.sentinelPath)
		// Take the current timestamps first: the files being reloaded here are
		// the same ones the sentinel is announcing, and without this each would
		// reload a second time on the next poll.
		for _, t := range cw.targets {
			cw.changed(t.path)
		}
		cw.ReloadAll()
		// Deferred targets covered by the save are queued, not applied: the
		// sentinel is touched automatically by v3config on every save, and
		// automatic saves must not bypass the idle gate.
		for _, t := range cw.deferredTargets {
			if cw.changed(t.path) {
				cw.queueDeferred(t)
			}
		}
		cw.applyPendingIfIdle()
		return
	}

	for _, t := range cw.targets {
		if cw.changed(t.path) {
			slog.Info("config file change detected", "file", t.name)
			t.reload()
		}
	}
	for _, t := range cw.deferredTargets {
		if cw.changed(t.path) {
			cw.queueDeferred(t)
		}
	}
	cw.applyPendingIfIdle()
}

// activeSessions reports how many callers are online, or 0 when no session
// registry is wired (tests, tools) — in which case there is nobody to
// protect and deferral degrades to immediate application.
func (cw *ConfigWatcher) activeSessions() int {
	if cw.menuExecutor == nil || cw.menuExecutor.SessionRegistry == nil {
		return 0
	}
	return cw.menuExecutor.SessionRegistry.ActiveCount()
}

// queueDeferred validates a changed structural config now and, if it parses,
// queues it for the next idle window. A file that fails validation is not
// queued: the error is the sysop's immediate feedback, and fixing the file
// changes its timestamp, which queues it afresh.
func (cw *ConfigWatcher) queueDeferred(t deferredTarget) {
	if err := t.validate(); err != nil {
		slog.Error("changed config failed validation and will not be applied",
			"file", t.name, "error", err)
		return
	}
	cw.mu.Lock()
	cw.pending[t.name] = true
	cw.mu.Unlock()
	if n := cw.activeSessions(); n > 0 {
		slog.Info("structural config change queued; applies when no callers are online",
			"file", t.name, "active_sessions", n,
			"hint", "touch configs/"+config.ReloadForceSentinelName+" to apply now")
	}
}

// applyPendingIfIdle applies every queued structural reload once the board
// is idle. Called on each poll tick, so the reload lands within one poll
// interval of the last caller logging off.
func (cw *ConfigWatcher) applyPendingIfIdle() {
	cw.mu.Lock()
	anyPending := len(cw.pending) > 0
	cw.mu.Unlock()
	if !anyPending || cw.activeSessions() > 0 {
		return
	}
	for _, t := range cw.deferredTargets {
		cw.mu.Lock()
		isPending := cw.pending[t.name]
		cw.mu.Unlock()
		if isPending {
			cw.applyDeferred(t)
		}
	}
}

// applyDeferred runs a deferred target's reload and clears its pending mark.
// The mark is cleared even on failure: the apply re-reads the file, so a
// failure means the file changed again and went bad, and the fix will arrive
// as a fresh timestamp change that re-queues it.
func (cw *ConfigWatcher) applyDeferred(t deferredTarget) {
	cw.mu.Lock()
	delete(cw.pending, t.name)
	cw.mu.Unlock()
	if err := t.apply(); err != nil {
		slog.Error("failed to apply deferred config reload", "file", t.name, "error", err)
		return
	}
	slog.Info("deferred config reload applied", "file", t.name)
}

// PendingReloads lists the structural configs queued for the next idle
// window, for surfacing on the WFC console.
func (cw *ConfigWatcher) PendingReloads() []string {
	cw.mu.Lock()
	defer cw.mu.Unlock()
	if len(cw.pending) == 0 {
		return nil
	}
	names := make([]string, 0, len(cw.pending))
	for _, t := range cw.deferredTargets {
		if cw.pending[t.name] {
			names = append(names, t.name)
		}
	}
	return names
}

// validateFileAreas is the signal-time check for file_areas.json: parse only,
// touch nothing.
func (cw *ConfigWatcher) validateFileAreas() error {
	data, err := os.ReadFile(filepath.Join(cw.rootConfigPath, "file_areas.json"))
	if err != nil {
		return err
	}
	var areas []file.FileArea
	if err := json.Unmarshal(data, &areas); err != nil {
		return fmt.Errorf("parsing file_areas.json: %w", err)
	}
	return nil
}

// applyFileAreas reloads the file manager from disk. Runs only at an idle
// window (or under the force sentinel), per the reload constraint documented
// on FileManager.
func (cw *ConfigWatcher) applyFileAreas() error {
	if cw.menuExecutor == nil || cw.menuExecutor.FileMgr == nil {
		return fmt.Errorf("file manager not running")
	}
	return cw.menuExecutor.FileMgr.Reload()
}

// changed reports whether path's modification time differs from the one last
// recorded, updating the record. A file that does not exist is not a change;
// its record is dropped so that re-creating it registers as one.
//
// Any difference counts, not just a newer timestamp, so that restoring a config
// file from a backup — which can move the timestamp backwards — still reloads.
func (cw *ConfigWatcher) changed(path string) bool {
	info, err := os.Stat(path)

	cw.mu.Lock()
	defer cw.mu.Unlock()

	if err != nil {
		delete(cw.mtimes, path)
		return false
	}

	mt := info.ModTime()
	prev, seen := cw.mtimes[path]
	cw.mtimes[path] = mt
	return !seen || !mt.Equal(prev)
}

// ReloadAll re-reads every watched configuration file. Used by the reload
// sentinel and by SIGHUP.
func (cw *ConfigWatcher) ReloadAll() {
	for _, t := range cw.targets {
		t.reload()
	}
}

// reloadDoors reloads the door configurations.
func (cw *ConfigWatcher) reloadDoors() {
	slog.Info("reloading doors.json")

	doorsPath := filepath.Join(cw.rootConfigPath, "doors.json")
	newDoors, err := config.LoadDoors(doorsPath)
	if err != nil {
		slog.Error("failed to reload doors.json", "error", err)
		return
	}

	// Update MenuExecutor's DoorRegistry atomically
	cw.menuExecutor.SetDoorRegistry(newDoors)
	slog.Info("doors.json reloaded", "count", len(newDoors))
}

// reloadLoginSequence reloads the login sequence configuration.
func (cw *ConfigWatcher) reloadLoginSequence() {
	slog.Info("reloading login.json")

	newSequence, err := config.LoadLoginSequence(cw.rootConfigPath)
	if err != nil {
		slog.Error("failed to reload login.json", "error", err)
		return
	}

	// Update MenuExecutor's LoginSequence atomically
	cw.menuExecutor.SetLoginSequence(newSequence)
	slog.Info("login.json reloaded", "steps", len(newSequence))
}

// reloadStrings reloads the strings configuration.
func (cw *ConfigWatcher) reloadStrings() {
	slog.Info("reloading strings.json")

	newStrings, err := config.LoadStrings(cw.rootConfigPath)
	if err != nil {
		slog.Error("failed to reload strings.json", "error", err)
		return
	}

	// Update MenuExecutor's LoadedStrings atomically
	cw.menuExecutor.SetStrings(newStrings)
	slog.Info("strings.json reloaded")
}

// reloadTheme reloads the theme configuration.
func (cw *ConfigWatcher) reloadTheme() {
	slog.Info("reloading theme.json")

	newTheme, err := config.LoadThemeConfig(cw.menuSetPath)
	if err != nil {
		slog.Error("failed to reload theme.json", "error", err)
		return
	}

	// Update MenuExecutor's Theme atomically
	cw.menuExecutor.SetTheme(newTheme)
	slog.Info("theme.json reloaded")
}

// reloadProtocols reloads the transfer protocol configurations.
func (cw *ConfigWatcher) reloadProtocols() {
	slog.Info("reloading protocols.json")

	protocolsPath := filepath.Join(cw.rootConfigPath, "protocols.json")
	newProtocols, err := transfer.LoadProtocols(protocolsPath)
	if err != nil {
		slog.Error("failed to reload protocols.json", "error", err)
		return
	}

	cw.menuExecutor.SetProtocols(newProtocols)
	slog.Info("protocols.json reloaded", "count", len(newProtocols))
}

// reloadConferences reloads the conference definitions.
func (cw *ConfigWatcher) reloadConferences() {
	slog.Info("reloading conferences.json")

	confMgr := cw.menuExecutor.ConferenceMgr
	if confMgr == nil {
		// Conferences were disabled at boot (conferences.json failed to load),
		// so there is no manager to reload into.
		slog.Warn("conference manager not running, restart required to apply conferences.json")
		return
	}
	if err := confMgr.Reload(); err != nil {
		slog.Error("failed to reload conferences.json", "error", err)
		return
	}
	slog.Info("conferences.json reloaded")
}

// SetScheduler hands the watcher the event scheduler once main has created
// it. The scheduler is constructed after the watcher, so it cannot be a
// constructor argument; until this is called, events.json changes are noted
// as requiring a restart.
func (cw *ConfigWatcher) SetScheduler(s *scheduler.Scheduler) {
	cw.mu.Lock()
	cw.scheduler = s
	cw.mu.Unlock()
}

// reloadEvents reloads the event scheduler configuration.
func (cw *ConfigWatcher) reloadEvents() {
	slog.Info("reloading events.json")

	cw.mu.Lock()
	sched := cw.scheduler
	cw.mu.Unlock()
	if sched == nil {
		// Either main has not reached scheduler startup yet, or the scheduler
		// was never created because events.json failed to load at boot.
		slog.Warn("event scheduler not running, restart required to apply events.json")
		return
	}

	newEvents, err := config.LoadEventsConfig(cw.rootConfigPath)
	if err != nil {
		slog.Error("failed to reload events.json", "error", err)
		return
	}

	sched.Reload(newEvents)
	slog.Info("events.json reloaded", "count", len(newEvents.Events))
}

// reloadServerConfig reloads the server configuration.
func (cw *ConfigWatcher) reloadServerConfig() {
	slog.Info("reloading config.json")

	newServerConfig, err := config.LoadServerConfig(cw.rootConfigPath)
	if err != nil {
		slog.Error("failed to reload config.json", "error", err)
		return
	}

	// Update server config atomically
	if cw.serverConfigMu != nil {
		cw.serverConfigMu.Lock()
		*cw.serverConfig = newServerConfig
		cw.serverConfigMu.Unlock()
	} else {
		// Fallback if no mutex provided (not thread-safe)
		*cw.serverConfig = newServerConfig
	}

	// Also update MenuExecutor's ServerCfg
	cw.menuExecutor.SetServerConfig(newServerConfig)

	// Update UserManager's new user level
	if cw.userMgr != nil {
		cw.userMgr.SetNewUserLevel(newServerConfig.NewUserLevel)
		slog.Info("updated new user level", "level", newServerConfig.NewUserLevel)
		cw.userMgr.SetAutoValidateNewUsers(newServerConfig.AutoValidateNewUsers)
		slog.Info("updated auto-validate new users", "enabled", newServerConfig.AutoValidateNewUsers)
	}

	// Push connection-security settings into the tracker. Without this the
	// node/IP/lockout limits and the connection rate limiter would be updated
	// in the config struct but never reach the code that enforces them.
	if cw.connTracker != nil {
		cw.connTracker.ApplyServerConfig(newServerConfig)
	}

	// Apply the log level live. Only the level is hot — the log directory and
	// rolling settings belong to the writer built at startup. Normalize a
	// local copy first, exactly as Init does: an absent logging block means
	// the default level, not an invalid one to warn about on every reload.
	logCfg := newServerConfig.Logging
	logCfg.Normalize()
	before := logging.Level()
	if err := logging.SetLevel(logCfg.Level); err != nil {
		slog.Warn("config.json logging.level not applied", "level", logCfg.Level, "error", err)
	} else if after := logging.Level(); after != before {
		// Log at the new threshold (or INFO, whichever is higher) so the
		// message announcing the change survives the change it announces:
		// at a fixed INFO it would be filtered out the moment the level
		// rises past INFO, making a successful change look like a silent
		// failure.
		msgLevel := slog.LevelInfo
		if after > msgLevel {
			msgLevel = after
		}
		slog.Log(context.Background(), msgLevel, "log level changed", "from", before.String(), "to", after.String())
	}

	slog.Info("config.json reloaded")
	slog.Info("note: listener ports/hosts, sshEnabled/telnetEnabled, SSH host keys, QWK API, and logging dir/rolling changes still require a restart")
}
