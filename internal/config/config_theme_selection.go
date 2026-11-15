package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ViSiON-3/vision-3-bbs/internal/menuset"
)

// ThemeSelection is the "theme" section of config.json: which theme, a menu
// set under menus/, the board runs (#1076), and the board's own colours
// over it (#1078).
type ThemeSelection struct {
	// Name is the theme's directory under menus/. Omitted or empty means
	// menuset.DefaultTheme. One name selects artwork, templates, menus and
	// colours together, with local changes in menus.d/<name>. Changing it
	// takes effect when the BBS restarts.
	Name string `json:"name,omitempty"`
	// Colors are the board's colour overrides, over the selected theme's
	// own; see ThemeColors. Unlike Name, a change reloads without a
	// restart.
	Colors ThemeColors `json:"colors,omitzero"`
}

// ThemeName returns the theme the config selects: theme.name, or the
// default when it is omitted. The name is not checked; see SelectedMenuSet.
func (c ServerConfig) ThemeName() string {
	if c.Theme.Name == "" {
		return menuset.DefaultTheme
	}
	return c.Theme.Name
}

// ThemeNameFromConfigDir reads only theme.name from configDir's config.json,
// for the tools that need the selection and nothing else. A missing file or
// an omitted name gives the default; a file that doesn't parse is an error.
func ThemeNameFromConfigDir(configDir string) (string, error) {
	path := filepath.Join(configDir, "config.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return menuset.DefaultTheme, nil
		}
		return "", fmt.Errorf("reading %s: %w", path, err)
	}
	var raw struct {
		Theme ThemeSelection `json:"theme"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return "", fmt.Errorf("reading theme.name from %s: %w", path, err)
	}
	if raw.Theme.Name == "" {
		return menuset.DefaultTheme, nil
	}
	return raw.Theme.Name, nil
}

// SelectedMenuSet returns the menu set directory of the theme that
// configDir's config.json selects, under bbsRoot, and the theme's name. It
// is the one place the server, doctor, preflight, the editors and menuedit
// turn the selection into a path. The error says what is wrong with the
// selection (an invalid name, or no such directory under menus/); name is
// still returned with it when it could be read.
func SelectedMenuSet(bbsRoot, configDir string) (dir, name string, err error) {
	name, err = ThemeNameFromConfigDir(configDir)
	if err != nil {
		return "", "", err
	}
	dir, err = menuset.CheckTheme(bbsRoot, name)
	if err != nil {
		return "", name, fmt.Errorf("config.json theme.name: %w", err)
	}
	return dir, name, nil
}
