package menu

import (
	"bufio"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
	"golang.org/x/term"
)

// loadLightbarOptions loads and parses lightbar options from configuration files
func loadLightbarOptions(menuName string, e *MenuExecutor) ([]LightbarOption, error) {
	// Determine paths using MenuSetPath
	cfgFilename := menuName + ".CFG"
	barFilename := menuName + ".BAR"
	cfgPath := filepath.Join(e.MenuSetPath, "cfg", cfgFilename)
	barPath := filepath.Join(e.MenuSetPath, "bar", barFilename)

	slog.Debug("loading CFG", "path", cfgPath)
	slog.Debug("loading BAR", "path", barPath)

	// Load commands from CFG file using the proper JSON loader
	commandsByHotkey := make(map[string]string)
	configPath := filepath.Join(e.MenuSetPath, "cfg")
	commands, err := LoadCommands(menuName, configPath)
	if err != nil {
		slog.Warn("failed to load CFG file", "path", cfgPath, "error", err)
	} else {
		// Build hotkey -> command mapping for validation
		for _, cmd := range commands {
			hotkey := strings.ToUpper(strings.TrimSpace(cmd.Keys))
			commandsByHotkey[hotkey] = cmd.Command
		}
	}

	// Parse BAR file
	barFile, err := os.Open(barPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open BAR file %s: %w", barPath, err)
	}
	defer func() { _ = barFile.Close() }() // read-only

	var options []LightbarOption
	scanner := bufio.NewScanner(barFile)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, ";") {
			continue // Skip empty lines and comments
		}

		// Parse record in format: X,Y,HotKey,DisplayText // OLD Format
		// Parse record in format: X,Y,HiLitedColor,RegularColor,HotKey,ReturnValue,HiLitedString // NEW Format
		parts := strings.SplitN(line, ",", 7) // Split into 7 parts
		if len(parts) != 7 {                  // Check for 7 parts
			slog.Warn("malformed BAR line (expected 7 fields)", "line", line)
			continue
		}

		x, xerr := strconv.Atoi(strings.TrimSpace(parts[0]))
		y, yerr := strconv.Atoi(strings.TrimSpace(parts[1]))
		if xerr != nil || yerr != nil {
			slog.Warn("invalid coordinates in BAR line", "line", line)
			continue
		}

		// Parse color codes
		highlightColor, hcErr := strconv.Atoi(strings.TrimSpace(parts[2]))
		regularColor, rcErr := strconv.Atoi(strings.TrimSpace(parts[3]))
		if hcErr != nil || rcErr != nil {
			slog.Warn("invalid color codes in BAR line", "line", line)
			// Default colors? Or skip?
			highlightColor = 7 // Default: White on Black (inverse)
			regularColor = 15  // Default: Bright White on Black
		}

		hotkey := strings.ToUpper(strings.TrimSpace(parts[4])) // HotKey is the 5th field (index 4)
		returnValue := strings.TrimSpace(parts[5])             // ReturnValue is the 6th field (index 5)
		displayText := strings.TrimSpace(parts[6])             // DisplayText is the 7th field (index 6)

		// Verify the hotkey maps to a command
		if _, exists := commandsByHotkey[hotkey]; !exists {
			slog.Warn("hotkey in BAR file has no matching command in CFG", "hotkey", hotkey)
		}

		options = append(options, LightbarOption{
			X:              x,
			Y:              y,
			Text:           displayText,
			HotKey:         hotkey,
			ReturnValue:    returnValue,
			HighlightColor: highlightColor,
			RegularColor:   regularColor,
		})
	}

	return options, nil
}

// loadBarFile loads and parses a standalone BAR file (no matching CFG required).
// Returns nil, nil if the file does not exist.
func loadBarFile(barName string, e *MenuExecutor) ([]LightbarOption, error) {
	barPath := filepath.Join(e.MenuSetPath, "bar", barName+".BAR")

	barFile, err := os.Open(barPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to open BAR file %s: %w", barPath, err)
	}
	defer func() { _ = barFile.Close() }() // read-only

	var options []LightbarOption
	scanner := bufio.NewScanner(barFile)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, ";") {
			continue
		}

		parts := strings.SplitN(line, ",", 7)
		if len(parts) != 7 {
			slog.Warn("malformed BAR line (expected 7 fields)", "bar", barName, "line", line)
			continue
		}

		x, xerr := strconv.Atoi(strings.TrimSpace(parts[0]))
		y, yerr := strconv.Atoi(strings.TrimSpace(parts[1]))
		if xerr != nil || yerr != nil {
			slog.Warn("invalid coordinates in BAR line", "bar", barName, "line", line)
			continue
		}

		highlightColor, hcErr := strconv.Atoi(strings.TrimSpace(parts[2]))
		regularColor, rcErr := strconv.Atoi(strings.TrimSpace(parts[3]))
		if hcErr != nil || rcErr != nil {
			slog.Warn("invalid color codes in BAR line", "bar", barName, "line", line)
			highlightColor = 7
			regularColor = 15
		}

		hotkey := strings.ToUpper(strings.TrimSpace(parts[4]))
		returnValue := strings.TrimSpace(parts[5])
		displayText := strings.TrimSpace(parts[6])

		options = append(options, LightbarOption{
			X:              x,
			Y:              y,
			Text:           displayText,
			HotKey:         hotkey,
			ReturnValue:    returnValue,
			HighlightColor: highlightColor,
			RegularColor:   regularColor,
		})
	}

	return options, nil
}

// drawLightbarMenu draws the lightbar menu with the specified option selected
func drawLightbarOption(terminal *term.Terminal, opt LightbarOption, selected bool, outputMode ansi.OutputMode) error {
	posCmd := fmt.Sprintf("\x1b[%d;%dH", opt.Y, opt.X)
	err := terminalio.WriteProcessedBytes(terminal, []byte(posCmd), outputMode)
	if err != nil {
		return fmt.Errorf("failed positioning cursor for lightbar option: %w", err)
	}

	colorCode := opt.RegularColor
	if selected {
		colorCode = opt.HighlightColor
	}
	ansiColorSequence := colorCodeToAnsi(colorCode)
	err = terminalio.WriteProcessedBytes(terminal, []byte(ansiColorSequence), outputMode)
	if err != nil {
		return fmt.Errorf("failed setting color for lightbar option: %w", err)
	}

	err = terminalio.WriteProcessedBytes(terminal, []byte(opt.Text), outputMode)
	if err != nil {
		return fmt.Errorf("failed writing lightbar option text: %w", err)
	}

	err = terminalio.WriteProcessedBytes(terminal, []byte(attrReset), outputMode)
	if err != nil {
		return fmt.Errorf("failed resetting attributes after lightbar option: %w", err)
	}

	return nil
}

func drawLightbarMenu(terminal *term.Terminal, backgroundBytes []byte, options []LightbarOption, selectedIndex int, outputMode ansi.OutputMode, drawBackground bool) error {
	if drawBackground {
		// For CP437 mode, write raw bytes directly to avoid UTF-8 false positives
		var err error
		if outputMode == ansi.OutputModeCP437 {
			_, err = terminal.Write(backgroundBytes)
		} else {
			err = terminalio.WriteProcessedBytes(terminal, backgroundBytes, outputMode)
		}
		if err != nil {
			return fmt.Errorf("failed writing lightbar background: %w", err)
		}
	}

	for i, opt := range options {
		if err := drawLightbarOption(terminal, opt, i == selectedIndex, outputMode); err != nil {
			return err
		}
	}

	return nil
}

// Define needed ANSI attributes
const (
	attrInverse = "\x1b[7m" // Inverse video - Keep for fallback?
	attrReset   = "\x1b[0m" // Reset attributes
)

// LightbarOption represents a single option in a lightbar menu
type LightbarOption struct {
	X, Y           int    // Screen coordinates
	Text           string // Display text
	HotKey         string // Command hotkey
	ReturnValue    string // Return value (often same as hotkey, but can differ)
	HighlightColor int    // Color code when highlighted
	RegularColor   int    // Color code when not highlighted
}
