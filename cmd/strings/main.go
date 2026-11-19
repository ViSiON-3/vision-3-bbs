// Command strings is the ViSiON/3 BBS string configuration editor.
// It provides a TUI for editing the BBS prompt strings stored in strings.json,
// faithfully recreating the original Turbo Pascal STRINGS.EXE from Vision/2.
//
// Usage:
//
// ./strings [--config path/to/strings.json]
// ./strings --theme NAME [--no-overlay]
//
// If no --config flag is provided, it looks for configs/strings.json
// relative to the current working directory. --theme edits the strings.json
// of theme NAME of the board in the current directory: menus.d/NAME/ by
// default, menus/NAME/ with --no-overlay.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/menuset"
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
// whose strings.json is at path, which lives in its configs directory, on
// the theme at menuSet, or the board's own when that is "". Another theme
// than the board's is drawn without the board's colour overrides, as its
// callers see it.
func loadBoardColorSlots(path, menuSet string) {
	// An absolute path, so the board root is found from a relative one
	// too, such as strings.json when run from the configs directory.
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	configDir := filepath.Dir(path)
	if menuSet != "" {
		ansi.SetColorSlotDefaults(config.ThemeColorSlotCodes(filepath.Dir(configDir), configDir, path, menuSet))
		return
	}
	ansi.SetColorSlotDefaults(config.BoardColorSlotCodes(filepath.Dir(configDir), configDir, path, ""))
}

// target is the strings.json the editor opens: path, and the board's
// configs/strings.json, board, whose colours the preview draws with those
// of the theme at menuSet ("" for the board's own). inherit is, for a
// theme's file, the files whose strings it falls back to, in order; nil
// for the board's own file.
type target struct {
	path    string
	board   string
	menuSet string
	inherit []string
}

// resolveTarget works out the file to edit from the flags, for the board
// in cwd. With no flags it is configs/strings.json; --config names a file;
// --theme names a theme whose strings.json is edited in its overlay, or in
// the theme's own directory with --no-overlay.
func resolveTarget(cwd, configPath, theme string, noOverlay bool) (target, error) {
	board := filepath.Join(cwd, "configs", "strings.json")
	switch {
	case theme == "" && noOverlay:
		return target{}, fmt.Errorf("--no-overlay needs --theme")
	case theme == "" && configPath != "":
		return target{path: configPath, board: configPath}, nil
	case theme == "":
		return target{path: board, board: board}, nil
	case configPath != "":
		return target{}, fmt.Errorf("--config and --theme can't be used together")
	}
	dir, err := menuset.CheckTheme(cwd, theme)
	if err != nil {
		return target{}, err
	}
	set := menuset.FromPath(dir)
	own := set.Path(menuset.LayerBase, config.ThemeStringsFile)
	if noOverlay || !set.HasOverlay() {
		return target{path: own, board: board, menuSet: dir, inherit: []string{board}}, nil
	}
	return target{path: set.Path(menuset.LayerOverlay, config.ThemeStringsFile), board: board, menuSet: dir, inherit: []string{board, own}}, nil
}

func main() {
	configPath := flag.String("config", "", "Path to strings.json (default: configs/strings.json)")
	theme := flag.String("theme", "", "Edit the strings.json of this theme (menus.d/NAME/strings.json)")
	noOverlay := flag.Bool("no-overlay", false, "With --theme, edit menus/NAME/strings.json instead of the overlay")
	flag.Parse()

	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	tgt, err := resolveTarget(cwd, *configPath, *theme, *noOverlay)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	// Verify the config directory exists
	dir := filepath.Dir(tgt.board)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "Error: config directory does not exist: %s\n", dir)
		os.Exit(1)
	}

	shippedDefaults := loadShippedDefaults()

	loadBoardColorSlots(tgt.board, tgt.menuSet)

	// Create the editor model
	var model stringeditor.Model
	if tgt.inherit == nil {
		model, err = stringeditor.New(tgt.path, shippedDefaults)
	} else {
		var inherited map[string]string
		inherited, err = stringeditor.InheritedStrings(tgt.inherit...)
		if err == nil {
			model, err = stringeditor.NewForTheme(tgt.path, shippedDefaults, inherited)
		}
	}
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
