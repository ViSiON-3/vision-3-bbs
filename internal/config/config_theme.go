package config

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/ViSiON-3/vision-3-bbs/internal/atomicfile"
	"github.com/ViSiON-3/vision-3-bbs/internal/menuset"
)

// ThemeConfig holds theme-related settings, loaded per menu set.
// Colors are standard DOS color codes (0-255).
type ThemeConfig struct {
	YesNoHighlightColor int `json:"yesNoHighlightColor"`
	YesNoRegularColor   int `json:"yesNoRegularColor"`
	ChatSysopColor      int `json:"chatSysopColor"` // sysop pane in split-screen chat
	ChatUserColor       int `json:"chatUserColor"`  // caller pane in split-screen chat
	// SysopChatScreen names the chat screen split-screen chat opens with:
	// cfg/NAME.json and ansi/NAME.ANS in the menu set. Empty means the
	// built-in layout, and "random" one of the set's screens each chat. The
	// sysop can pick another from the WFC.
	SysopChatScreen string `json:"sysopChatScreen"`
	// Add other theme elements here as needed (e.g., default menu colors)
}

// LoadThemeConfig loads theme settings from theme.json within a specific menu
// set path. A copy in the set's overlay (menus.d/<set>/theme.json) takes
// precedence over the shipped one.
func LoadThemeConfig(menuSetPath string) (ThemeConfig, error) {
	filePath, resolveErr := menuset.FromPath(menuSetPath).Resolve("theme.json")
	slog.Info("loading theme configuration", "path", filePath)

	// Default theme settings
	defaultTheme := ThemeConfig{
		YesNoHighlightColor: 112, // White on Black (inverse)
		YesNoRegularColor:   15,  // Bright White on Black
		ChatSysopColor:      11,  // Light cyan
		ChatUserColor:       10,  // Light green
	}

	if resolveErr != nil {
		return defaultTheme, resolveErr
	}
	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			slog.Warn("theme.json not found, using default theme settings", "path", filePath)
			return defaultTheme, nil // Return defaults if file doesn't exist
		}
		slog.Error("failed to read theme file", "path", filePath, "error", err)
		return defaultTheme, fmt.Errorf("failed to read theme file %s: %w", filePath, err)
	}

	// Initialize theme with defaults before unmarshalling
	theme := defaultTheme
	err = json.Unmarshal(data, &theme)
	if err != nil {
		slog.Error("failed to parse theme JSON, using default theme settings", "path", filePath, "error", err)
		return defaultTheme, fmt.Errorf("failed to parse theme JSON from %s: %w", filePath, err)
	}

	slog.Info("loaded theme configuration", "path", filePath)
	return theme, nil
}

// SaveThemeConfig writes theme to the menu set's theme.json in its overlay
// (menus.d/<set>/theme.json), so an upgrade of the shipped set leaves it
// alone. It starts from the theme.json in use, so keys ThemeConfig does not
// know are kept.
func SaveThemeConfig(menuSetPath string, theme ThemeConfig) error {
	set := menuset.FromPath(menuSetPath)
	current, err := set.Resolve("theme.json")
	if err != nil {
		return err
	}
	doc := map[string]json.RawMessage{}
	data, err := os.ReadFile(current)
	switch {
	case err == nil:
		if err := json.Unmarshal(data, &doc); err != nil {
			return fmt.Errorf("parse %s: %w", current, err)
		}
	case !os.IsNotExist(err):
		return err
	}
	known, err := json.Marshal(theme)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(known, &doc); err != nil {
		return err
	}
	out, err := json.MarshalIndent(doc, "", "    ")
	if err != nil {
		return err
	}
	path := set.WritePath("theme.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return atomicfile.WriteFile(path, append(out, '\n'), 0o644)
}
