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
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/configeditor"
	"github.com/ViSiON-3/vision-3-bbs/internal/ftn"
	"github.com/ViSiON-3/vision-3-bbs/internal/logging"
)

func main() {
	// The body lives in run so its defers — above all the log flush — happen on
	// every path. logging.Init caches non-error records and flushes on Close, so
	// an os.Exit from inside the body would discard whatever had not been
	// written yet, which is exactly the tail you want after a failure.
	os.Exit(run())
}

func run() int {
	configPath := flag.String("config", "", "Path to configs directory (default: configs/)")
	flag.Parse()

	// Resolve config path
	path := *configPath
	if path == "" {
		cwd, err := os.Getwd()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			return 1
		}
		path = filepath.Join(cwd, "configs")
	}

	// Verify the directory exists
	info, err := os.Stat(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: config directory not found: %s\n", path)
		return 1
	}
	if !info.IsDir() {
		fmt.Fprintf(os.Stderr, "Error: %s is not a directory\n", path)
		return 1
	}

	// Silence slog before anything else logs. Loading the server config below
	// emits two Info lines, and this is a TUI: they must not reach the terminal.
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))

	bbsRoot := filepath.Dir(path)
	if abs, err := filepath.Abs(path); err == nil {
		bbsRoot = filepath.Dir(abs) // regenerated conf paths must be absolute
	}
	serverCfg, serverCfgErr := config.LoadServerConfig(path)

	// Now swap the discard for a rolling log file — still never the terminal.
	// Discarding for the whole run also threw away every warning the editor
	// produces (an FTN link saved with no hostname, a network declared in
	// binkd.conf, a rejected outbound path) and left a config save with no
	// record anywhere that it had happened. A logging failure keeps the
	// discard, and is shown in the editor's status line: it cannot go to
	// stderr, which the alternate screen is about to clear, and silently
	// losing every lifecycle record is the thing this log exists to stop.
	// There is one status line, so messages accumulate rather than replace:
	// the log warning must not be lost behind a binkd.conf notice.
	startupMsg := ""
	addStartupMsg := func(msg string) {
		if startupMsg != "" {
			startupMsg += " | "
		}
		startupMsg += msg
	}
	if closeLog, err := initEditorLog(serverCfg, bbsRoot); err == nil {
		defer func() { _ = closeLog() }() // best-effort flush at exit
		slog.Info("config editor started", "configs", path, "bbs_root", bbsRoot)
	} else {
		addStartupMsg(fmt.Sprintf("Warning: editor log unavailable, nothing will be logged this session: %v", err))
	}

	// Regenerate a missing binkd.conf from configuration before the editor
	// starts (best-effort): the FTN Setup Wizard refuses to re-run for an
	// existing network, so this is the recovery path after a manual delete.
	if ftnCfg, ftnErr := config.LoadFTNConfig(path); ftnErr == nil && serverCfgErr == nil {
		if created, err := ftn.EnsureBinkdConf(bbsRoot, ftnCfg, serverCfg); err != nil {
			addStartupMsg(fmt.Sprintf("Warning: binkd.conf regeneration failed: %v", err))
		} else if created {
			addStartupMsg("binkd.conf was missing - regenerated from configuration")
		}
	}

	// Create the editor model
	model, err := configeditor.New(path)
	if err != nil {
		slog.Error("editor initialisation failed", "error", err)
		fmt.Fprintf(os.Stderr, "Error initializing editor: %v\n", err)
		return 1
	}
	model = model.WithStartupSplash()
	if startupMsg != "" {
		model = model.WithStartupMessage(startupMsg)
	}

	// Run the BubbleTea TUI
	p := tea.NewProgram(model, tea.WithAltScreen(), tea.WithInputTTY())
	if _, err := p.Run(); err != nil {
		slog.Error("editor exited with an error", "error", err)
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 1
	}
	slog.Info("config editor exited")
	return 0
}

// initEditorLog sends the editor's slog to a rolling log file, with no console
// echo. It returns the close function the caller should defer.
//
// The log directory comes from config.json like every other binary's, but is
// resolved against the BBS root rather than the working directory: vision3 and
// v3mail are launched from the BBS root so a relative "data/logs" works for
// them, whereas the editor is typically run as `config --config /path/to/configs`
// from wherever the sysop happens to be standing.
func initEditorLog(serverCfg config.ServerConfig, bbsRoot string) (func() error, error) {
	logCfg := serverCfg.Logging
	if strings.TrimSpace(logCfg.Dir) == "" {
		logCfg.Dir = config.DefaultLogDir
	}
	if !filepath.IsAbs(logCfg.Dir) {
		logCfg.Dir = filepath.Join(bbsRoot, logCfg.Dir)
	}
	// console=false: the TUI owns the terminal.
	_, closeLog, err := logging.Init(logCfg, "config.log", false)
	if err != nil {
		return nil, err
	}
	return closeLog, nil
}
