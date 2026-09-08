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
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ViSiON-3/vision-3-bbs/internal/stringeditor"
	configtemplates "github.com/ViSiON-3/vision-3-bbs/templates/configs"
)

// loadShippedDefaults returns the factory default strings used by F4 restore
// and by the creation of a missing strings.json.
//
// An on-disk template wins so a distribution can ship adjusted defaults, but
// the templates directory is not present in every installation, so the copy
// embedded in this binary is the guaranteed fallback rather than giving up.
func loadShippedDefaults() map[string]string {
	candidates := []string{
		"templates/configs/strings.json",
	}
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		candidates = append(candidates,
			filepath.Join(dir, "templates", "configs", "strings.json"),
			filepath.Join(dir, "..", "templates", "configs", "strings.json"),
		)
	}
	for _, path := range candidates {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var defaults map[string]string
		if err := json.Unmarshal(data, &defaults); err == nil {
			return defaults
		}
	}

	defaults, err := configtemplates.StringDefaults()
	if err != nil {
		// Unreachable unless the embedded asset itself is corrupt.
		fmt.Fprintf(os.Stderr, "Warning: no factory defaults available: %v\n", err)
		return nil
	}
	return defaults
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

	// Load factory defaults from templates/, falling back to the embedded copy
	shippedDefaults := loadShippedDefaults()

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
