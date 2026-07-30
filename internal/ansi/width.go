package ansi

import (
	"strings"
	"unicode/utf8"
)

// TruncateRunes clamps s to maxRunes columns, appending ellipsis when it cuts.
// Counted and cut in runes so multi-byte text is never split. When maxRunes is
// not larger than the ellipsis itself, the value is hard-cut with no ellipsis.
//
// This is a bare rune-counting primitive: unlike TruncateVisible, it does not
// understand ANSI escape sequences or pipe color codes. Use it for plain text
// (labels, names, free-form user input); use TruncateVisible for strings that
// may carry ANSI escapes.
func TruncateRunes(s string, maxRunes int, ellipsis string) string {
	if maxRunes < 0 {
		maxRunes = 0
	}
	if utf8.RuneCountInString(s) <= maxRunes {
		return s
	}
	runes := []rune(s)
	e := utf8.RuneCountInString(ellipsis)
	if maxRunes <= e {
		return string(runes[:maxRunes])
	}
	return string(runes[:maxRunes-e]) + ellipsis
}

// PadRight pads s with spaces on the right to width columns, counted in
// runes. It never truncates: if s is already at or beyond width, it is
// returned unchanged. Compose with TruncateRunes when the caller needs a hard
// column budget.
func PadRight(s string, width int) string {
	n := utf8.RuneCountInString(s)
	if n >= width {
		return s
	}
	return s + strings.Repeat(" ", width-n)
}

// PadLeft pads s with spaces on the left to width columns, counted in runes.
// It never truncates: if s is already at or beyond width, it is returned
// unchanged. Compose with TruncateRunes when the caller needs a hard column
// budget.
func PadLeft(s string, width int) string {
	n := utf8.RuneCountInString(s)
	if n >= width {
		return s
	}
	return strings.Repeat(" ", width-n) + s
}

// Center centers s within width columns, counted in runes. It never
// truncates: if s is already at or beyond width, it is returned unchanged.
// Compose with TruncateRunes when the caller needs a hard column budget. When
// the padding is odd, the extra space goes on the right.
func Center(s string, width int) string {
	n := utf8.RuneCountInString(s)
	if n >= width {
		return s
	}
	pad := (width - n) / 2
	return strings.Repeat(" ", pad) + s + strings.Repeat(" ", width-pad-n)
}
