package config

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"

	"github.com/ViSiON-3/vision-3-bbs/internal/menuset"
)

// ThemeConfig holds theme-related settings, loaded per menu set.
// Colors are standard DOS color codes (0-255).
type ThemeConfig struct {
	YesNoHighlightColor int `json:"yesNoHighlightColor"`
	YesNoRegularColor   int `json:"yesNoRegularColor"`
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
