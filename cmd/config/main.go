// Command config is the ViSiON/3 BBS Configuration Editor.
// It provides a TUI for managing all system configuration files,
// faithfully recreating the original Turbo Pascal CONFIG.EXE from Vision/2.
//
// Usage:
//
//	./config [--config path/to/configs/directory]
//
// If no --config flag is provided, it looks for configs/
// relative to the current working directory.
package main

import (
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ViSiON-3/vision-3-bbs/internal/configeditor"
)

func main() {
	configPath := flag.String("config", "", "Path to configs directory (default: configs/)")
	flag.Parse()

	// Resolve config path
	path := *configPath
	if path == "" {
		cwd, err := os.Getwd()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		path = filepath.Join(cwd, "configs")
	}

	// Verify the directory exists
	info, err := os.Stat(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: config directory not found: %s\n", path)
		os.Exit(1)
	}
	if !info.IsDir() {
		fmt.Fprintf(os.Stderr, "Error: %s is not a directory\n", path)
		os.Exit(1)
	}

	// Suppress slog output during TUI operation — the alternate-screen terminal
	// cannot tolerate interleaved log lines.
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))

	// Create the editor model
	model, err := configeditor.New(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error initializing editor: %v\n", err)
		os.Exit(1)
	}
	model = model.WithStartupSplash()

	// Run the BubbleTea TUI
	p := tea.NewProgram(model, tea.WithAltScreen(), tea.WithInputTTY())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
