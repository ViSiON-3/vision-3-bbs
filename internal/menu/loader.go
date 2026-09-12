package menu

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"

	"github.com/ViSiON-3/vision-3-bbs/internal/menuset"
)

// LoadMenu reads a .MNU file (assumed JSON) for the given menu name from the
// menu set's mnu/ directory, overlay first.
func LoadMenu(menuName string, menus menuset.Set) (*MenuRecord, error) {
	filePath := menus.Resolve("mnu", menuName+".MNU")
	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("menu file not found: %s", filePath)
		}
		return nil, fmt.Errorf("failed to read menu file %s: %w", filePath, err)
	}

	var menuRec MenuRecord
	// Unmarshal the JSON data
	if err := json.Unmarshal(data, &menuRec); err != nil {
		// Provide more context in the error message
		return nil, fmt.Errorf("failed to decode menu file %s (is this a valid JSON .MNU file?): %w", filePath, err)
	}

	// Optional: Add validation if needed

	return &menuRec, nil
}

// LoadCommands reads a .CFG file (assumed JSON) for the given menu name from
// the menu set's cfg/ directory, overlay first.
func LoadCommands(menuName string, menus menuset.Set) ([]CommandRecord, error) {
	filePath := menus.Resolve("cfg", menuName+".CFG")
	slog.Debug("attempting to load command file", "file", filePath, "menu", menuName)
	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			// It's valid for a menu to have no commands, return empty slice
			slog.Warn("command file does not exist, menu will have no commands", "file", filePath)
			return []CommandRecord{}, nil
		}
		return nil, fmt.Errorf("failed to read command file %s: %w", filePath, err)
	}

	// Handle empty file case explicitly
	if len(data) == 0 {
		slog.Debug("command file is empty", "file", filePath)
		return []CommandRecord{}, nil
	}

	var commands []CommandRecord
	// Unmarshal the JSON data (expecting an array of CommandRecord)
	if err := json.Unmarshal(data, &commands); err != nil {
		// Provide more context in the error message
		return nil, fmt.Errorf("failed to decode command file %s (is this a valid JSON array of commands?): %w", filePath, err)
	}

	// Optional: Log loaded commands for tracing
	for _, cmd := range commands {
		slog.Debug("loaded command", "keys", cmd.Keys, "command", cmd.Command, "acs", cmd.ACS, "hidden", cmd.Hidden)
	}

	return commands, nil
}

// HasBarFile returns true if a .BAR lightbar definition file exists for the
// given menu name inside the menu set's bar/ directory, in either layer.
func HasBarFile(menuName string, menus menuset.Set) bool {
	return menus.Exists("bar", menuName+".BAR")
}
