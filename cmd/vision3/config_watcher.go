package main

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/menu"
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

	interval     time.Duration
	targets      []reloadTarget
	sentinelPath string

	mu     sync.Mutex           // guards mtimes and stop
	mtimes map[string]time.Time // last-seen modification time per polled path
	stop   chan struct{}
	done   chan struct{}
}

// NewConfigWatcher creates a configuration file watcher and starts polling.
func NewConfigWatcher(rootConfigPath, menuSetPath string, menuExecutor *menu.MenuExecutor, userMgr *user.UserMgr, serverConfig *config.ServerConfig, serverConfigMu *sync.RWMutex) (*ConfigWatcher, error) {
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
		rootConfigPath: rootConfigPath,
		menuSetPath:    menuSetPath,
		menuExecutor:   menuExecutor,
		userMgr:        userMgr,
		serverConfig:   serverConfig,
		serverConfigMu: serverConfigMu,
		interval:       defaultPollInterval,
		sentinelPath:   config.ReloadSentinelPath(rootConfigPath),
		mtimes:         make(map[string]time.Time),
		stop:           make(chan struct{}),
		done:           make(chan struct{}),
	}

	cw.targets = []reloadTarget{
		{name: "config.json", path: filepath.Join(rootConfigPath, "config.json"), reload: cw.reloadServerConfig},
		{name: "doors.json", path: filepath.Join(rootConfigPath, "doors.json"), reload: cw.reloadDoors},
		{name: "login.json", path: filepath.Join(rootConfigPath, "login.json"), reload: cw.reloadLoginSequence},
		{name: "strings.json", path: filepath.Join(rootConfigPath, "strings.json"), reload: cw.reloadStrings},
		{name: "theme.json", path: filepath.Join(menuSetPath, "theme.json"), reload: cw.reloadTheme},
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

	paths := make([]string, 0, len(cw.targets)+1)
	paths = append(paths, cw.sentinelPath)
	for _, t := range cw.targets {
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
		return
	}

	for _, t := range cw.targets {
		if cw.changed(t.path) {
			slog.Info("config file change detected", "file", t.name)
			t.reload()
		}
	}
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

	slog.Info("config.json reloaded")
	slog.Warn("some config.json changes require a full restart", "fields", "ports, keys, IP limits")
}
