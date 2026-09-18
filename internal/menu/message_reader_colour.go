package menu

import "github.com/ViSiON-3/vision-3-bbs/internal/ansi"

// bodyDefaultColour is the state the body starts in. Grey rather than a bare
// reset, so the cyan of the |11 reading suffix cannot bleed into plain body
// text on the first line.
const bodyDefaultColour = "\x1b[0;37m"

// buildBodyEntryStates returns, for each body line, the escape sequence that
// puts the terminal into the colour state that line begins in.
//
// The reader paints only the visible window, positioning each row absolutely,
// so a line is not preceded on screen by the lines above it. A message that
// sets a colour once and then writes several plain lines therefore rendered
// correctly only until the line carrying the colour scrolled off the top.
// Folding the state forward over the whole body lets any scroll position be
// drawn exactly as a scroll from the top would have drawn it (#362).
func buildBodyEntryStates(lines []string) []string {
	states := make([]string, len(lines))
	sgr := ansi.NewSGRState()
	sgr.Write(bodyDefaultColour)
	for i, line := range lines {
		states[i] = sgr.Escape()
		sgr.Write(line)
	}
	return states
}
