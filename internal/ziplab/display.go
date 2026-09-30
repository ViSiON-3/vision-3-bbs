package ziplab

import (
	"bufio"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
)

// Status represents the D/P/F state of a ZipLab step.
type Status string

// Step states, written as the single letter used for the state in
// ZIPLAB.NFO entry keys (for example "1D" for step 1 in progress).
const (
	StatusDoing Status = "D" // step is running
	StatusPass  Status = "P" // step finished without error
	StatusFail  Status = "F" // step returned an error
)

// NFOEntry holds the parsed display coordinates and colors for one step+status.
type NFOEntry struct {
	Step         int
	Status       Status
	Col          int    // X position (1-based)
	Row          int    // Y position (1-based)
	NormalColor  int    // DOS color attribute (bg*16 + fg)
	HiColor      int    // DOS highlight color attribute
	DisplayChars string // Characters to display (e.g., "███")
}

// NFOConfig holds all parsed entries from ZIPLAB.NFO.
type NFOConfig struct {
	Entries map[string]NFOEntry // key = "1D", "1P", "1F", etc.
}

// entryKey builds the map key for a step+status combination.
func entryKey(step int, status Status) string {
	return fmt.Sprintf("%d%s", step, status)
}

// GetEntry returns the NFO entry for a given step and status.
func (n *NFOConfig) GetEntry(step int, status Status) (NFOEntry, bool) {
	entry, ok := n.Entries[entryKey(step, status)]
	return entry, ok
}

// HasStep returns true if the NFO has any entries for the given step number.
func (n *NFOConfig) HasStep(step int) bool {
	_, ok := n.Entries[entryKey(step, StatusDoing)]
	return ok
}

// BuildStatusSequence generates an ANSI escape sequence that positions the cursor
// and renders the status indicator for a given step+status.
func (n *NFOConfig) BuildStatusSequence(step int, status Status) string {
	entry, ok := n.GetEntry(step, status)
	if !ok {
		return ""
	}

	// Choose color based on status
	dosColor := entry.NormalColor
	if status == StatusPass || status == StatusFail {
		dosColor = entry.HiColor
	}

	fg, bg, bold := DOSColorToANSI(dosColor)

	var sb strings.Builder
	// Cursor position
	fmt.Fprintf(&sb, "\x1b[%d;%dH", entry.Row, entry.Col)
	// Color
	if bold {
		fmt.Fprintf(&sb, "\x1b[1;%d;%dm", 30+(fg%8), 40+bg)
	} else {
		fmt.Fprintf(&sb, "\x1b[0;%d;%dm", 30+fg, 40+bg)
	}
	// Display chars
	sb.WriteString(entry.DisplayChars)
	// Reset
	sb.WriteString("\x1b[0m")

	return sb.String()
}

// ParseNFO reads and parses a ZIPLAB.NFO file.
// Format: {step}{status} = X,Y,NormalColor,HiColor,DisplayChars
func ParseNFO(filePath string) (*NFOConfig, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open NFO file %s: %w", filePath, err)
	}
	defer func() { _ = f.Close() }() // read-only

	nfo := &NFOConfig{
		Entries: make(map[string]NFOEntry),
	}

	scanner := bufio.NewScanner(f)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())

		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, ";") {
			continue
		}

		// Parse: "1D = 45,10,112,116,███"
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			slog.Warn("nfo invalid format (no '=')", "line", lineNum, "content", line)
			continue
		}

		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])

		if len(key) < 2 {
			slog.Warn("nfo key too short", "line", lineNum, "key", key)
			continue
		}

		// Parse key: digit(s) + status letter
		statusChar := key[len(key)-1:]
		stepStr := key[:len(key)-1]

		step, err := strconv.Atoi(stepStr)
		if err != nil {
			slog.Warn("nfo invalid step number", "line", lineNum, "step", stepStr)
			continue
		}

		var status Status
		switch strings.ToUpper(statusChar) {
		case "D":
			status = StatusDoing
		case "P":
			status = StatusPass
		case "F":
			status = StatusFail
		default:
			slog.Warn("nfo unknown status", "line", lineNum, "status", statusChar)
			continue
		}

		// Parse value: X,Y,NormalColor,HiColor,DisplayChars
		valueParts := strings.SplitN(value, ",", 5)
		if len(valueParts) < 5 {
			slog.Warn("nfo expected 5 comma-separated values", "line", lineNum, "count", len(valueParts))
			continue
		}

		col, err := strconv.Atoi(strings.TrimSpace(valueParts[0]))
		if err != nil {
			slog.Warn("nfo invalid X coordinate", "line", lineNum, "value", valueParts[0])
			continue
		}
		row, err := strconv.Atoi(strings.TrimSpace(valueParts[1]))
		if err != nil {
			slog.Warn("nfo invalid Y coordinate", "line", lineNum, "value", valueParts[1])
			continue
		}
		normalColor, err := strconv.Atoi(strings.TrimSpace(valueParts[2]))
		if err != nil {
			slog.Warn("nfo invalid normal color", "line", lineNum, "value", valueParts[2])
			continue
		}
		hiColor, err := strconv.Atoi(strings.TrimSpace(valueParts[3]))
		if err != nil {
			slog.Warn("nfo invalid hi color", "line", lineNum, "value", valueParts[3])
			continue
		}
		displayChars := strings.TrimSpace(valueParts[4])

		nfo.Entries[entryKey(step, status)] = NFOEntry{
			Step:         step,
			Status:       status,
			Col:          col,
			Row:          row,
			NormalColor:  normalColor,
			HiColor:      hiColor,
			DisplayChars: displayChars,
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading NFO file %s: %w", filePath, err)
	}

	slog.Debug("parsed nfo entries", "count", len(nfo.Entries), "path", filePath)
	return nfo, nil
}

// MaxRow returns the highest row number used by any NFO entry.
func (n *NFOConfig) MaxRow() int {
	maxRow := 0
	for _, entry := range n.Entries {
		if entry.Row > maxRow {
			maxRow = entry.Row
		}
	}
	return maxRow
}

// dosToANSIColor maps a DOS palette index (black, blue, green, cyan, red,
// magenta, brown, grey) to the ANSI SGR colour with the same appearance
// (ANSI orders them black, red, green, yellow, blue, magenta, cyan, white).
var dosToANSIColor = [8]int{0, 4, 2, 6, 1, 5, 3, 7}

// DOSColorToANSI converts a DOS color attribute (bg*16 + fg) to ANSI components.
// Returns the foreground as an ANSI colour index (0-7, plus 8 when bright),
// the background as an ANSI colour index (0-7), and whether bold is needed.
// For example, attribute 30 (bright yellow on blue) gives fg 11, bg 4, bold.
func DOSColorToANSI(dosAttr int) (fg int, bg int, bold bool) {
	dosFG := dosAttr & 0x0F        // Lower 4 bits = foreground (0-15)
	dosBG := (dosAttr >> 4) & 0x07 // Bits 4-6 = background (0-7)
	bold = dosFG >= 8              // High bit of foreground = bold/bright
	fg = dosToANSIColor[dosFG&0x07] + dosFG&0x08
	bg = dosToANSIColor[dosBG]
	return fg, bg, bold
}
