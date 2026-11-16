// Command strings is the ViSiON/3 BBS string configuration editor.
// It provides a TUI for editing the BBS prompt strings stored in strings.json,
// faithfully recreating the original Turbo Pascal STRINGS.EXE from Vision/2.
//
// Usage:
//
// ./strings [--config path/to/strings.json]
//
// If no --config flag is provided, it looks for configs/strings.json
// relative to the current working directory.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/stringeditor"
	configtemplates "github.com/ViSiON-3/vision-3-bbs/templates/configs"
)

// loadShippedDefaults returns the shipped strings built into this binary,
// the same ones the BBS prints for every key strings.json leaves out, so the
// editor and the BBS never disagree on what "default" means.
func loadShippedDefaults() map[string]string {
	defaults, err := configtemplates.StringDefaults()
	if err != nil {
		// Unreachable unless the embedded asset itself is corrupt.
		fmt.Fprintf(os.Stderr, "Warning: no factory defaults available: %v\n", err)
		return nil
	}
	return defaults
}

// loadBoardColorSlots makes the preview draw |C1-|C7 in the board's
// defaults, as the BBS draws them, rather than the built-in ones: the board
// whose strings.json is at path, which lives in its configs directory.
func loadBoardColorSlots(path string) {
	// An absolute path, so the board root is found from a relative one
	// too, such as strings.json when run from the configs directory.
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	configDir := filepath.Dir(path)
	ansi.SetColorSlotDefaults(config.BoardColorSlotCodes(filepath.Dir(configDir), configDir, ""))
}

func main() {
	configPath := flag.String("config", "", "Path to strings.json (default: configs/strings.json)")
	flag.Parse()

	// Resolve config path
	path := *configPath
	if path == "" {
		cwd, err := os.Getwd()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		path = filepath.Join(cwd, "configs", "strings.json")
	}

	// Verify the config directory exists
	dir := filepath.Dir(path)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "Error: config directory does not exist: %s\n", dir)
		os.Exit(1)
	}

	shippedDefaults := loadShippedDefaults()

	loadBoardColorSlots(path)

	// Create the editor model
	model, err := stringeditor.New(path, shippedDefaults)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error initializing editor: %v\n", err)
		os.Exit(1)
	}

	// Run the BubbleTea TUI
	p := tea.NewProgram(model, tea.WithAltScreen(), tea.WithInputTTY())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
