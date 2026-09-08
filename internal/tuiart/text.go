package tuiart

import "github.com/ViSiON-3/vision-3-bbs/internal/ansi"

// CenterText centers a string within width using visual (rune) width.
//
// The text is truncated before centring so a box holds its shape: an over-long
// title is cut to the column budget rather than pushing the border out.
func CenterText(s string, width int) string {
	return ansi.Center(ansi.TruncateRunes(s, width, ""), width)
}

// PadRight pads a string to width with spaces, truncating if longer.
func PadRight(s string, width int) string {
	return ansi.PadRight(ansi.TruncateRunes(s, width, ""), width)
}
