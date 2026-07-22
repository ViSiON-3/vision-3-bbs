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

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/configeditor"
	"github.com/ViSiON-3/vision-3-bbs/internal/ftn"
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

	// Regenerate a missing binkd.conf from configuration before the editor
	// starts (best-effort): the FTN Setup Wizard refuses to re-run for an
	// existing network, so this is the recovery path after a manual delete.
	startupMsg := ""
	bbsRoot := filepath.Dir(path)
	if abs, err := filepath.Abs(path); err == nil {
		bbsRoot = filepath.Dir(abs) // regenerated conf paths must be absolute
	}
	if ftnCfg, ftnErr := config.LoadFTNConfig(path); ftnErr == nil {
		if serverCfg, svErr := config.LoadServerConfig(path); svErr == nil {
			if created, err := ftn.EnsureBinkdConf(bbsRoot, ftnCfg, serverCfg); err != nil {
				startupMsg = fmt.Sprintf("Warning: binkd.conf regeneration failed: %v", err)
			} else if created {
				startupMsg = "binkd.conf was missing - regenerated from configuration"
			}
		}
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
	if startupMsg != "" {
		model = model.WithStartupMessage(startupMsg)
	}

	// Run the BubbleTea TUI
	p := tea.NewProgram(model, tea.WithAltScreen(), tea.WithInputTTY())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
