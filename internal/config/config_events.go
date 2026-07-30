package config

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
)

// EventConfig defines a scheduled event configuration
type EventConfig struct {
	ID                string            `json:"id"`
	Name              string            `json:"name"`
	Schedule          string            `json:"schedule"` // Cron syntax (empty for startup-only events)
	Command           string            `json:"command"`
	Args              []string          `json:"args"`
	WorkingDirectory  string            `json:"working_directory"`
	TimeoutSeconds    int               `json:"timeout_seconds"` // 0 = no timeout (for daemons)
	Enabled           bool              `json:"enabled"`
	RunAtStartup      bool              `json:"run_at_startup,omitempty"` // Launch immediately when BBS starts
	EnvironmentVars   map[string]string `json:"environment_vars,omitempty"`
	RunAfter          string            `json:"run_after,omitempty"`           // Event ID to run after
	DelayAfterSeconds int               `json:"delay_after_seconds,omitempty"` // Delay after RunAfter completes
}

// EventsConfig is the root configuration for the event scheduler
type EventsConfig struct {
	// Enabled is deprecated and ignored: the scheduler always runs, and
	// events are enabled/disabled individually. The field is kept so older
	// binaries reading the same events.json still start their scheduler.
	Enabled             bool          `json:"enabled"`
	MaxConcurrentEvents int           `json:"max_concurrent_events"`
	Events              []EventConfig `json:"events"`
}

// LoadEventsConfig loads the event scheduler configuration from events.json
func LoadEventsConfig(configPath string) (EventsConfig, error) {
	filePath := filepath.Join(configPath, "events.json")
	slog.Info("loading event scheduler configuration", "path", filePath)

	defaultConfig := EventsConfig{
		Enabled:             true,
		MaxConcurrentEvents: 3,
		Events:              []EventConfig{},
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			slog.Info("events.json not found, no events to schedule", "path", filePath)
			return defaultConfig, nil
		}
		return defaultConfig, fmt.Errorf("failed to read events config file %s: %w", filePath, err)
	}

	var config EventsConfig
	err = json.Unmarshal(data, &config)
	if err != nil {
		slog.Error("failed to parse events config JSON", "path", filePath, "error", err)
		return defaultConfig, fmt.Errorf("failed to parse events config JSON from %s: %w", filePath, err)
	}

	// Set default max concurrent events if not specified
	if config.MaxConcurrentEvents <= 0 {
		config.MaxConcurrentEvents = 3
	}

	enabledCount := 0
	for _, event := range config.Events {
		if event.Enabled {
			enabledCount++
		}
	}

	slog.Info("loaded event scheduler configuration", "events", len(config.Events), "enabled", enabledCount)

	return config, nil
}
