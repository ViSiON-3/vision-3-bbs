package ansi

import "strconv"

// ColorSlotDefaults are the built-in colours of the caller colour slots |C1
// to |C7: 1 prompt, 2 input, 3 text, 4 stat, 5 text2, 6 stat2, 7 bar. They
// match the stock screens' colours, so a screen written with the codes looks
// as it did with fixed ones.
//
// ReplacePipeCodes expands a |Cn code to its slot's built-in colour. A
// caller's own colour, or the board's default, takes its place earlier,
// where the caller's placeholders are filled in (see the menu package), so
// this is what a screen with no caller behind it shows. Authored ANSI art
// gets the built-in colour too, never a caller's.
var ColorSlotDefaults = [7]string{"|07", "|15", "|07", "|15", "|08", "|11", "|00|B3"}

// ColorSlotCode returns the code |Cn for slot n (1 to 7).
func ColorSlotCode(n int) string { return "|C" + strconv.Itoa(n) }

func init() {
	for i, c := range ColorSlotDefaults {
		pipeCodeReplacements[ColorSlotCode(i+1)] = string(ReplacePipeCodes([]byte(c)))
	}
}
