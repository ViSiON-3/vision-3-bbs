package menu

import (
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
)

// themeScriptSuffix marks a menu SCRIPT word as a script file in the theme's
// own scripts/ directory rather than a door code from doors.json. It is
// matched without regard to case; a door code never contains a dot.
const themeScriptSuffix = ".js"

// isThemeScript reports whether word, the first word of a SCRIPT, names a
// theme script: it ends in .js.
func isThemeScript(word string) bool {
	return strings.HasSuffix(strings.ToLower(word), themeScriptSuffix)
}

// themeScriptDoor returns the v3_script door that runs the theme script
// name: the file scripts/<name> of the executor's menu set, the overlay
// (menus.d/<theme>/scripts) searched first. The door runs in that scripts/
// directory with no access rule and no run limit: a menu script waits on its
// caller for as long as they stay on the menu, and still ends when they are
// idle too long, run out of time or hang up. ok is false when name is not a single file name or
// no layer has the file.
func (e *MenuExecutor) themeScriptDoor(name string) (door config.DoorConfig, ok bool) {
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, `/\`) || filepath.IsAbs(name) || filepath.VolumeName(name) != "" {
		return config.DoorConfig{}, false
	}
	path, _, found, err := e.Menus().Locate("scripts", name)
	if err != nil {
		slog.Warn("resolving theme script", "path", path, "error", err)
		return config.DoorConfig{}, false
	}
	if !found {
		return config.DoorConfig{}, false
	}
	return config.DoorConfig{
		Type:             "v3_script",
		Name:             name,
		Script:           name,
		WorkingDirectory: filepath.Dir(path),
		MaxRunMinutes:    -1,
	}, true
}

// themeScriptDataDir is the data directory of a theme script run by door:
// data/ beside the scripts/ directory that holds it, in the same layer.
func themeScriptDataDir(door config.DoorConfig) string {
	return filepath.Join(filepath.Dir(door.WorkingDirectory), "data")
}
