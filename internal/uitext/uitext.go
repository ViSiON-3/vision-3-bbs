// Package uitext provides small text helpers shared by the terminal UI editors
// (configeditor, usereditor, menueditor). These were previously duplicated
// verbatim in each package.
package uitext

import "strings"

// BoolToYN converts a bool to "Y" or "N".
func BoolToYN(b bool) string {
	if b {
		return "Y"
	}
	return "N"
}

// YNToBool converts "Y"/"y" to true, anything else to false.
func YNToBool(s string) bool {
	return strings.ToUpper(s) == "Y"
}

// ApproximateVisibleLen estimates the visible width of a styled string by
// counting runes outside of ANSI escape sequences (ESC ... terminating letter).
func ApproximateVisibleLen(s string) int {
	inEsc := false
	count := 0
	for _, r := range s {
		if r == '\x1b' {
			inEsc = true
			continue
		}
		if inEsc {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				inEsc = false
			}
			continue
		}
		count++
	}
	return count
}

// TruncateToVisual truncates s to n visible characters, preserving (and not
// counting) ANSI escape sequences.
func TruncateToVisual(s string, n int) string {
	var b strings.Builder
	inEsc := false
	count := 0
	for _, r := range s {
		if count >= n && !inEsc {
			break
		}
		b.WriteRune(r)
		if r == '\x1b' {
			inEsc = true
			continue
		}
		if inEsc {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				inEsc = false
			}
			continue
		}
		count++
	}
	return b.String()
}
