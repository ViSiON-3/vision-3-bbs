package syncjs

import (
	"fmt"
	"strings"
)

// Synchronet attribute byte layout:
//
//	Bits 0-2: foreground color (0-7)
//	Bit 3:    high intensity
//	Bits 4-6: background color (0-7)
//	Bit 7:    blink
const (
	AttrBlink = 0x80
	AttrHigh  = 0x08
	AttrFGMask = 0x07
	AttrBGMask = 0x70
)

// ANSI color index for the 8 base Synchronet colors.
var syncColorToANSI = [8]int{0, 4, 2, 6, 1, 5, 3, 7} // K,B,G,C,R,M,Y,W

// AttrToANSI converts a Synchronet attribute byte to an ANSI SGR escape sequence.
func AttrToANSI(attr uint8) string {
	fg := int(attr & AttrFGMask)
	bg := int((attr & AttrBGMask) >> 4)
	hi := attr&AttrHigh != 0
	blink := attr&AttrBlink != 0

	parts := []string{"0"} // reset first
	if hi {
		parts = append(parts, "1")
	}
	if blink {
		parts = append(parts, "5")
	}
	parts = append(parts, fmt.Sprintf("%d", 30+syncColorToANSI[fg]))
	parts = append(parts, fmt.Sprintf("%d", 40+syncColorToANSI[bg]))

	return fmt.Sprintf("\x1b[%sm", strings.Join(parts, ";"))
}

// ParseCtrlA converts Synchronet Ctrl-A attribute codes in a string to ANSI escape sequences.
// Ctrl-A codes are \x01 followed by an attribute character.
func ParseCtrlA(input string) string {
	var b strings.Builder
	b.Grow(len(input))

	i := 0
	for i < len(input) {
		if input[i] == 0x01 && i+1 < len(input) {
			code := input[i+1]
			ansi := ctrlAToANSI(code)
			if ansi != "" {
				b.WriteString(ansi)
			}
			i += 2
			continue
		}
		b.WriteByte(input[i])
		i++
	}
	return b.String()
}

// StripCtrlA removes all Ctrl-A codes from a string, returning plain text.
func StripCtrlA(input string) string {
	var b strings.Builder
	b.Grow(len(input))

	i := 0
	for i < len(input) {
		if input[i] == 0x01 && i+1 < len(input) {
			i += 2 // skip ctrl-A + code byte
			continue
		}
		b.WriteByte(input[i])
		i++
	}
	return b.String()
}

// ctrlAToANSI maps a single Ctrl-A code character to its ANSI escape sequence.
func ctrlAToANSI(code byte) string {
	switch code {
	// Foreground colors
	case 'K', 'k': return "\x1b[0;30m"   // Black
	case 'R', 'r': return "\x1b[0;31m"   // Red
	case 'G', 'g': return "\x1b[0;32m"   // Green
	case 'Y', 'y': return "\x1b[0;33m"   // Brown/Yellow
	case 'B', 'b': return "\x1b[0;34m"   // Blue
	case 'M', 'm': return "\x1b[0;35m"   // Magenta
	case 'C', 'c': return "\x1b[0;36m"   // Cyan
	case 'W', 'w': return "\x1b[0;37m"   // White

	// Attributes
	case 'H', 'h': return "\x1b[1m"      // High intensity
	case 'I', 'i': return "\x1b[5m"      // Blink
	case 'N', 'n': return "\x1b[0m"      // Normal (reset)
	case '-':      return "\x1b[0m"      // Normal (reset)

	// Background colors (0-7)
	case '0': return "\x1b[40m" // Black background
	case '1': return "\x1b[44m" // Blue background
	case '2': return "\x1b[42m" // Green background
	case '3': return "\x1b[46m" // Cyan background
	case '4': return "\x1b[41m" // Red background
	case '5': return "\x1b[45m" // Magenta background
	case '6': return "\x1b[43m" // Yellow/Brown background
	case '7': return "\x1b[47m" // White background

	// Cursor control
	case '[': return "\x1b[s"   // Save cursor position
	case ']': return "\x1b[u"   // Restore cursor position
	case 'L', 'l': return "\x1b[K" // Clear to end of line

	default:
		return ""
	}
}
