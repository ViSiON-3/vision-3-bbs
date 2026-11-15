package user

import (
	"strings"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
)

// ColorSlots is how many colour slots a caller has, |C1 to |C7: 1 prompt,
// 2 input, 3 text, 4 stat, 5 text2, 6 stat2, 7 bar.
const ColorSlots = 7

// ValidColorCode reports whether code is one a colour slot can hold: a
// foreground |00 to |15, optionally followed by a background |B0 to |B15. The
// codes are read with ansi.ColorPipeCodeLen, as the rest of the board reads
// them, so a code this takes is one the screens draw.
func ValidColorCode(code string) bool {
	fg := ansi.ColorPipeCodeLen(code)
	if fg != 3 || code[1] == 'B' || code == "|23" {
		return false
	}
	bg := code[fg:]
	return bg == "" || len(bg) > 2 && bg[1] == 'B' && ansi.ColorPipeCodeLen(bg) == len(bg)
}

// ColorCode returns the caller's code for slot n (1 to ColorSlots), or ""
// when it is not set and the board's default applies. A stored code that
// isn't valid counts as not set.
func (u *User) ColorCode(n int) string {
	if u == nil || n < 1 || n > len(u.ColorCodes) {
		return ""
	}
	if c := u.ColorCodes[n-1]; ValidColorCode(c) {
		return c
	}
	return ""
}

// SetColorCode sets slot n to code, or back to the default for "". It
// reports false, changing nothing, for a slot or code it can't hold.
func (u *User) SetColorCode(n int, code string) bool {
	code = strings.TrimSpace(code)
	if n < 1 || n > ColorSlots || code != "" && !ValidColorCode(code) {
		return false
	}
	codes := make([]string, ColorSlots)
	copy(codes, u.ColorCodes)
	codes[n-1] = code
	for len(codes) > 0 && codes[len(codes)-1] == "" {
		codes = codes[:len(codes)-1]
	}
	if len(codes) == 0 {
		codes = nil
	}
	u.ColorCodes = codes
	return true
}
