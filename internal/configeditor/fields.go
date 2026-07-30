package configeditor

import (
	"strings"
	"unicode/utf8"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
)

// fieldType defines the edit behavior for a field.
type fieldType int

const (
	ftString  fieldType = iota // Free-text string input
	ftInteger                  // Integer with min/max validation
	ftYesNo                    // Y/N boolean toggle
	ftDisplay                  // Read-only display
	ftLookup                   // Lookup picker (select from list)
)

// LookupItem represents a selectable item in a lookup picker.
type LookupItem struct {
	Value   string // stored value (e.g. "1")
	Display string // shown in picker list (e.g. "Local Conferences (LOCAL)")
}

// fieldDef defines a single editable field on a config screen.
type fieldDef struct {
	Label       string    // Display label
	Help        string    // 1-line help text shown when field is active
	Type        fieldType // Edit type
	Col         int       // Column position (x) inside box
	Row         int       // Row position (y) inside box
	Width       int       // Input field width
	Min         int       // Minimum value (for ftInteger)
	Max         int       // Maximum value (for ftInteger)
	Masked      bool      // Show '*' characters when not actively editing
	Get         func() string
	Set         func(val string) error
	AfterSet    func(m *Model, val string) // called after Set succeeds, on the current model
	LookupItems func() []LookupItem        // provider for ftLookup
}

// maskValue replaces each rune in s with '*'.
func maskValue(s string) string {
	if s == "" {
		return s
	}
	return strings.Repeat("*", utf8.RuneCountInString(s))
}

// padRight pads a string to width with spaces, truncating if longer.
func padRight(s string, width int) string {
	return ansi.PadRight(ansi.TruncateRunes(s, width, ""), width)
}

// padLeft pads a string on the left to width.
func padLeft(s string, width int) string {
	return ansi.PadLeft(ansi.TruncateRunes(s, width, ""), width)
}

// centerText centers a string within a given width using visual (rune) width.
func centerText(s string, width int) string {
	// Truncate before centring so the box holds its shape: an over-long title
	// is cut to the column budget rather than pushing the border out. Matches
	// menueditor and usereditor. Editable values are not affected — the
	// textinput fields scroll horizontally while editing.
	return ansi.Center(ansi.TruncateRunes(s, width, ""), width)
}
