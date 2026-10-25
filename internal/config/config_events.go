package config

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
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

// RunsMessagePurge reports whether an event the scheduler will run invokes
// "v3mail purge --all", the only thing that enforces message areas'
// max_messages and max_age. The command may be the v3mail binary or
// "go run .../cmd/v3mail". A --dry-run purge deletes nothing, so it does
// not count.
func (c EventsConfig) RunsMessagePurge() bool {
	byID := make(map[string]EventConfig, len(c.Events))
	for _, ev := range c.Events {
		byID[ev.ID] = ev
	}
	chaining := !c.hasChainCycle(byID)
	for _, ev := range c.Events {
		if !triggered(ev, byID, chaining) {
			continue
		}
		if args, ok := v3mailArgs(ev.Command, ev.Args); ok && len(args) > 0 && args[0] == "purge" && purgeEnforcesLimits(args[1:]) {
			return true
		}
	}
	return false
}

// triggered reports whether the scheduler will ever start ev: it is enabled
// and has a schedule or runs at startup, or follows (run_after) an event
// that is triggered itself. A missing or disabled event in the chain, or
// chaining switched off (a cycle anywhere), means a chained event never
// runs.
func triggered(ev EventConfig, byID map[string]EventConfig, chaining bool) bool {
	seen := map[string]bool{}
	for {
		if !ev.Enabled {
			return false
		}
		if ev.Schedule != "" || ev.RunAtStartup {
			return true
		}
		if !chaining || ev.RunAfter == "" || seen[ev.ID] {
			return false
		}
		seen[ev.ID] = true
		parent, ok := byID[ev.RunAfter]
		if !ok {
			return false
		}
		ev = parent
	}
}

// hasChainCycle reports whether the enabled events' run_after links form a
// cycle, which makes the scheduler switch chaining off for every event.
func (c EventsConfig) hasChainCycle(byID map[string]EventConfig) bool {
	for _, ev := range c.Events {
		seen := map[string]bool{}
		for cur := ev; cur.Enabled && cur.RunAfter != ""; {
			if seen[cur.ID] {
				return true
			}
			seen[cur.ID] = true
			next, ok := byID[cur.RunAfter]
			if !ok {
				break
			}
			cur = next
		}
	}
	return false
}

// purgeEnforcesLimits reports whether v3mail purge, given args (after the
// subcommand), applies the areas' configured limits: --all is set and
// --dry-run is not. The flags mirror cmdPurge's in cmd/v3mail, parsed the
// way it parses them, so the scan stops where v3mail's does (the first
// positional argument, or "--"); arguments it would reject count as no
// purge, since v3mail exits on them.
func purgeEnforcesLimits(args []string) bool {
	fs := flag.NewFlagSet("purge", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	all := fs.Bool("all", false, "")
	fs.String("config", "", "")
	fs.String("data", "", "")
	fs.Bool("q", false, "")
	fs.Int("days", 0, "")
	fs.Int("keep", 0, "")
	dryRun := fs.Bool("dry-run", false, "")
	if err := fs.Parse(args); err != nil {
		return false
	}
	return *all && !*dryRun
}

// v3mailArgs returns v3mail's own arguments when command runs v3mail,
// either directly or as "go run <path>/v3mail".
func v3mailArgs(command string, args []string) ([]string, bool) {
	if isV3mailPath(command) {
		return args, true
	}
	if commandBase(command) == "go" && len(args) >= 2 && args[0] == "run" && isV3mailPath(args[1]) {
		return args[2:], true
	}
	return nil, false
}

// isV3mailPath reports whether path names the v3mail binary or package.
func isV3mailPath(path string) bool {
	base := commandBase(path)
	return base == "v3mail" || base == "v3mail.exe"
}

// commandBase is the lower-cased last element of path. It splits on both
// separators, so a Windows path is read on any OS.
func commandBase(path string) string {
	path = strings.TrimRight(path, `/\`)
	return strings.ToLower(path[strings.LastIndexAny(path, `/\`)+1:])
}
