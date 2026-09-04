package config

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// LoginItem defines a single step in the configurable login sequence.
type LoginItem struct {
	Command     string `json:"command"`                // Required: LASTCALLS, ONELINERS, USERSTATS, NMAILSCAN, DISPLAYFILE, RUNDOOR, FASTLOGIN
	Data        string `json:"data,omitempty"`         // Optional: command-specific data (filename, script path, etc.)
	ClearScreen bool   `json:"clear_screen,omitempty"` // Optional: clear screen before this item (default false)
	PauseAfter  bool   `json:"pause_after,omitempty"`  // Optional: show pause prompt after this item (default false)
	SecLevel    int    `json:"sec_level,omitempty"`    // Optional: minimum security level required (default 0 = everyone)
}

// LoadLoginSequence loads the login sequence configuration from login.json.
// If the file is missing, returns a default sequence matching legacy behavior.
func LoadLoginSequence(configPath string) ([]LoginItem, error) {
	filePath := filepath.Join(configPath, "login.json")
	slog.Info("loading login sequence", "path", filePath)

	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			slog.Info("login.json not found, using default login sequence", "path", filePath)
			return defaultLoginSequence(), nil
		}
		return nil, fmt.Errorf("failed to read login sequence file %s: %w", filePath, err)
	}

	var items []LoginItem
	if err := json.Unmarshal(data, &items); err != nil {
		return nil, fmt.Errorf("failed to parse login sequence JSON from %s: %w", filePath, err)
	}

	// Normalize command names to uppercase
	for i := range items {
		items[i].Command = strings.ToUpper(items[i].Command)
	}

	slog.Info("loaded login sequence", "count", len(items))
	return items, nil
}

// defaultLoginSequence returns the built-in default login sequence used when
// login.json is missing: system news first (it clears the screen via
// NEWSHDR.ANS and is silent when there is nothing new), then the legacy
// last callers / oneliners / user stats trio.
func defaultLoginSequence() []LoginItem {
	return []LoginItem{
		{Command: "PRINTNEWS"},
		{Command: "LASTCALLS"},
		{Command: "ONELINERS"},
		{Command: "USERSTATS"},
	}
}

// LoadOneLiners loads oneliner strings from a JSON file.
func LoadOneLiners(dataPath string) ([]string, error) { // Changed configPath to dataPath for clarity
	filePath := filepath.Join(dataPath, "oneliners.dat") // Assume loading .dat from data path
	slog.Info("loading oneliners", "path", filePath)

	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			slog.Warn("oneliners.dat not found, no oneliners loaded", "path", filePath)
			return []string{}, nil // Return empty slice, not an error
		}
		slog.Error("failed to read oneliners file", "path", filePath, "error", err)
		return nil, fmt.Errorf("failed to read oneliners file %s: %w", filePath, err)
	}

	// Assuming oneliners.dat is plain text, one per line
	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	// Filter out potential empty lines
	var oneLiners []string
	for _, line := range lines {
		if strings.TrimSpace(line) != "" {
			oneLiners = append(oneLiners, line)
		}
	}

	slog.Info("loaded oneliners", "count", len(oneLiners))
	return oneLiners, nil
}
