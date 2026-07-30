package config

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
)

// ThemeConfig holds theme-related settings, loaded per menu set.
// Colors are standard DOS color codes (0-255).
type ThemeConfig struct {
	YesNoHighlightColor int `json:"yesNoHighlightColor"`
	YesNoRegularColor   int `json:"yesNoRegularColor"`
	// Add other theme elements here as needed (e.g., default menu colors)
}

// LoadThemeConfig loads theme settings from theme.json within a specific menu set path.
func LoadThemeConfig(menuSetPath string) (ThemeConfig, error) {
	filePath := filepath.Join(menuSetPath, "theme.json")
	slog.Info("loading theme configuration", "path", filePath)

	// Default theme settings
	defaultTheme := ThemeConfig{
		YesNoHighlightColor: 112, // White on Black (inverse)
		YesNoRegularColor:   15,  // Bright White on Black
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
