package tuiart

import (
	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/uitext"
)

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

// FitInput bounds a rendered bubbles/textinput view to width cells and reports
// the width it occupies, so a caller can pad the rest of its row.
//
// The widget renders a cursor cell after the text, so a field whose value fills
// its configured Width returns Width+1 cells. Measuring that for padding is not
// enough on its own: when the view is wider than the space available, the
// padding goes to zero but the oversized view is still emitted and the row
// overruns its box. Bounding the view is what actually holds the row width.
func FitInput(view string, width int) (string, int) {
	if width < 0 {
		width = 0
	}
	if w := uitext.ApproximateVisibleLen(view); w <= width {
		return view, w
	}
	return uitext.TruncateToVisual(view, width), width
}
