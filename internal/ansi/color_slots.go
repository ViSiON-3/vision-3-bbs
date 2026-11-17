package ansi

import (
	"strconv"
	"sync/atomic"
)

// ColorSlotDefaults are the built-in colours of the caller colour slots |C1
// to |C7: 1 prompt, 2 input, 3 text, 4 stat, 5 text2, 6 stat2, 7 bar. They
// match the stock screens' colours, so a screen written with the codes looks
// as it did with fixed ones.
//
// ReplacePipeCodes expands a |Cn code to its slot's board default: the
// built-in colour, or the board's own (SetColorSlotDefaults). A caller's own
// colour takes its place earlier, where a caller's placeholders are filled in
// (see the menu package), so this is what a screen with no caller behind it
// shows, and what authored ANSI art always shows.
var ColorSlotDefaults = [7]string{"|07", "|15", "|07", "|15", "|08", "|11", "|00|B6"}

// ColorSlotCode returns the code |Cn for slot n (1 to 7).
func ColorSlotCode(n int) string { return "|C" + strconv.Itoa(n) }

// colorSlotExpansions holds what each |Cn expands to: the escape sequences
// of the board's slot defaults.
var colorSlotExpansions atomic.Pointer[[7]string]

func init() { SetColorSlotDefaults([7]string{}) }

// SetColorSlotDefaults sets the board's colour for each slot, as pipe codes;
// a "" keeps that slot's built-in colour. The BBS calls it whenever it loads
// strings.json.
func SetColorSlotDefaults(codes [7]string) {
	var e [7]string
	for i, c := range codes {
		if c == "" {
			c = ColorSlotDefaults[i]
		}
		e[i] = string(ReplacePipeCodes([]byte(c)))
	}
	colorSlotExpansions.Store(&e)
}

// colorSlotExpansion returns what code expands to when it is a slot code,
// |C1 to |C7.
func colorSlotExpansion(code string) (string, bool) {
	if len(code) != 3 || code[0] != '|' || code[1] != 'C' || code[2] < '1' || code[2] > '7' {
		return "", false
	}
	if e := colorSlotExpansions.Load(); e != nil {
		return e[code[2]-'1'], true
	}
	return "", false
}

// pipeCodeReplacement is what the pipe code code expands to, if it is one.
func pipeCodeReplacement(code string) (string, bool) {
	if e, ok := colorSlotExpansion(code); ok {
		return e, true
	}
	r, ok := pipeCodeReplacements[code]
	return r, ok
}
