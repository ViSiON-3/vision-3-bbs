package main

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/menu"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// ConfigWatcher watches configuration files for changes and hot-reloads them.
type ConfigWatcher struct {
	mu             sync.RWMutex
	watcher        *fsnotify.Watcher
	watcherDone    chan bool
	rootConfigPath string
	menuSetPath    string
	menuExecutor   *menu.MenuExecutor
	userMgr        *user.UserMgr
	serverConfig   *config.ServerConfig
	serverConfigMu *sync.RWMutex // External mutex for server config
}

// NewConfigWatcher creates a new configuration file watcher.
func NewConfigWatcher(rootConfigPath, menuSetPath string, menuExecutor *menu.MenuExecutor, userMgr *user.UserMgr, serverConfig *config.ServerConfig, serverConfigMu *sync.RWMutex) (*ConfigWatcher, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("failed to create file watcher: %w", err)
	}

	cw := &ConfigWatcher{
		watcher:        watcher,
		watcherDone:    make(chan bool),
		rootConfigPath: rootConfigPath,
		menuSetPath:    menuSetPath,
		menuExecutor:   menuExecutor,
		userMgr:        userMgr,
		serverConfig:   serverConfig,
		serverConfigMu: serverConfigMu,
	}

	// Watch the configs directory
	if err := watcher.Add(rootConfigPath); err != nil {
		watcher.Close()
		return nil, fmt.Errorf("failed to watch %s: %w", rootConfigPath, err)
	}
	slog.Info("watching for config changes", "path", rootConfigPath)

	// Watch the menu set path for theme.json
	themePath := filepath.Join(menuSetPath, "theme.json")
	if _, err := os.Stat(themePath); err == nil {
		if err := watcher.Add(themePath); err != nil {
			slog.Warn("failed to watch theme path", "path", themePath, "error", err)
		} else {
			slog.Info("watching for theme changes", "path", themePath)
		}
	}

	// Start watching in a goroutine
	go cw.watchLoop(watcher)

	return cw, nil
}

// Stop stops the configuration file watcher.
func (cw *ConfigWatcher) Stop() {
	cw.mu.Lock()
	defer cw.mu.Unlock()

	if cw.watcher == nil {
		return
	}

	select {
	case <-cw.watcherDone:
		// already closed
	default:
		close(cw.watcherDone)
	}
	cw.watcherDone = nil

	cw.watcher.Close()
	cw.watcher = nil
	slog.Info("configuration file watcher stopped")
}

// watchLoop handles file system events for configuration files.
func (cw *ConfigWatcher) watchLoop(w *fsnotify.Watcher) {
	// Debounce timer to avoid reloading on rapid successive writes
	var debounceTimer *time.Timer
	debounceDuration := 500 * time.Millisecond

	for {
		select {
		case event, ok := <-w.Events:
			if !ok {
				return
			}

			// Only care about Write and Create events
			if event.Op&fsnotify.Write == fsnotify.Write || event.Op&fsnotify.Create == fsnotify.Create {
				// Cancel existing debounce timer
				if debounceTimer != nil {
					debounceTimer.Stop()
				}
				// Schedule reload after debounce period
				debounceTimer = time.AfterFunc(debounceDuration, func() {
					cw.handleConfigChange(event.Name)
				})
			}

		case err, ok := <-w.Errors:
			if !ok {
				return
			}
			slog.Error("config file watcher error", "error", err)

		case <-cw.watcherDone:
			slog.Info("stopping config file watcher")
			return
		}
	}
}

// handleConfigChange identifies which config file changed and reloads it.
func (cw *ConfigWatcher) handleConfigChange(path string) {
	filename := filepath.Base(path)
	slog.Info("config file change detected", "file", filename)

	switch strings.ToLower(filename) {
	case "doors.json":
		cw.reloadDoors()
	case "login.json":
		cw.reloadLoginSequence()
	case "strings.json":
		cw.reloadStrings()
	case "theme.json":
		cw.reloadTheme()
	case "config.json":
		cw.reloadServerConfig()
	case "events.json":
		// Events config reload would require restarting the scheduler
		// For now, just log that a restart is needed
		slog.Warn("events.json changed — restart required")
	case "ftn.json":
		// FTN config reload would require restarting the message manager
		slog.Warn("ftn.json changed — restart required")
	default:
		// Ignore other files
		slog.Debug("ignoring config file change", "file", filename)
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
	}

	slog.Info("config.json reloaded")
	slog.Warn("some config.json changes require a full restart", "fields", "ports, keys, IP limits")
}
