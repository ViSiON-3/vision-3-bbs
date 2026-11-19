package config

import (
	"path/filepath"
	"strings"
	"unicode"
)

// MaxThemeDescription and MaxThemeName are the most columns Konfig shows
// of a theme's description and name; doctor --theme warns about longer
// ones.
const (
	MaxThemeDescription = 60
	MaxThemeName        = 20
)

// ThemeLabel is how a theme is shown where callers choose one: its
// name, in lists and as Konfig's Theme value, and a one-line description.
// A caller's saved choice is the theme's directory, never its name.
type ThemeLabel struct {
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
}

// Where a field of a ThemeLabel comes from (ThemeLabelSources).
const (
	ThemeLabelFromTheme = "theme.json"
	ThemeLabelFromBoard = "configs/config.json theme.labels"
)

// ThemeLabelSources says where each field of a theme's label comes from:
// ThemeLabelFromTheme, ThemeLabelFromBoard, or "" when neither sets it
// (the name is then the directory's, and there is no description).
type ThemeLabelSources struct {
	Name, Description string
}

// ThemeLabelFor is the label of the theme at dir on a board whose theme
// section is board: each field from board.Labels for the theme's directory
// name when set there, else from the theme's theme.json (with its
// overlay). A theme with no name is shown by its directory name; one with
// no description has none. Control characters are dropped and the ends
// trimmed, so a label can't move the cursor or clear the screen.
func ThemeLabelFor(dir string, board ThemeSelection) ThemeLabel {
	label, _ := ThemeLabelWithSources(dir, board)
	return label
}

// ThemeLabelWithSources is ThemeLabelFor with where each field came from.
func ThemeLabelWithSources(dir string, board ThemeSelection) (ThemeLabel, ThemeLabelSources) {
	var label ThemeLabel
	var from ThemeLabelSources
	set := func(field, src *string, value, source string) {
		if v := cleanThemeLabel(value); v != "" {
			*field, *src = v, source
		}
	}
	if theme, err := readThemeConfig(dir); err == nil {
		set(&label.Name, &from.Name, theme.Name, ThemeLabelFromTheme)
		set(&label.Description, &from.Description, theme.Description, ThemeLabelFromTheme)
	}
	if over, ok := board.Labels[filepath.Base(dir)]; ok {
		set(&label.Name, &from.Name, over.Name, ThemeLabelFromBoard)
		set(&label.Description, &from.Description, over.Description, ThemeLabelFromBoard)
	}
	if label.Name == "" {
		label.Name = filepath.Base(dir)
	}
	return label, from
}

// cleanThemeLabel drops control characters from s and trims its ends.
func cleanThemeLabel(s string) string {
	return strings.TrimSpace(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, s))
}

// ThemeSelectionFromConfigDir reads only the theme section of configDir's
// config.json, unchecked, for a tool that needs theme.labels. A missing
// file gives an empty section.
func ThemeSelectionFromConfigDir(configDir string) (ThemeSelection, error) {
	return themeSelectionFromConfigDir(configDir)
}
