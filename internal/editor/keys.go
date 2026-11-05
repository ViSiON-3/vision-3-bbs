package editor

import (
	"bytes"
	"strconv"
	"time"
)

// DefaultEscTimeout is how long a lone ESC waits for the rest of an escape
// sequence before it counts as the Esc key (see ReadKey for why it is this
// long). The script engine's readkey uses the same window.
const DefaultEscTimeout = 500 * time.Millisecond

// This file is the one table of the keys that arrive as escape sequences.
// ReadKey decodes them through it, and so does the script engine's readkey,
// which names them with KeyName.

// csiFinalKeys maps the final byte of "ESC [ ... final" to its key. Modifier
// parameters, as in ESC [ 1 ; 5 A for Ctrl-Up, don't change the key.
//
// 'K' is deliberately absent: SyncTERM sends ESC [ K for End, but it is also
// "Erase in Line", and clients that echo server output or pass it through a
// broken raw mode would type phantom End keys.
var csiFinalKeys = map[byte]int{
	'A': KeyArrowUp,
	'B': KeyArrowDown,
	'C': KeyArrowRight,
	'D': KeyArrowLeft,
	'H': KeyHome,
	'F': KeyEnd,      // xterm / VT standard End
	'U': KeyPageDown, // SyncTERM / EtherTerm
	'V': KeyPageUp,   // SyncTERM / EtherTerm
	'Z': KeyBackTab,
}

// csiTildeKeys maps n in "ESC [ n ~" (VT220, xterm, SyncTERM) to its key.
// 7 and 8 are rxvt's Home and End.
var csiTildeKeys = map[int]int{
	1: KeyHome, 2: KeyInsert, 3: KeyDeleteKey, 4: KeyEnd,
	5: KeyPageUp, 6: KeyPageDown, 7: KeyHome, 8: KeyEnd,
	11: KeyF1, 12: KeyF2, 13: KeyF3, 14: KeyF4, 15: KeyF5,
	17: KeyF6, 18: KeyF7, 19: KeyF8, 20: KeyF9, 21: KeyF10,
	23: KeyF11, 24: KeyF12,
}

// ss3Keys maps b in "ESC O b" to its key: cursor keys in application mode,
// Home and End, and F1-F4 as VT100 and xterm send them.
var ss3Keys = map[byte]int{
	'A': KeyArrowUp,
	'B': KeyArrowDown,
	'C': KeyArrowRight,
	'D': KeyArrowLeft,
	'H': KeyHome,
	'F': KeyEnd,
	'P': KeyF1,
	'Q': KeyF2,
	'R': KeyF3,
	'S': KeyF4,
}

// keyNames names the special keys for scripts.
var keyNames = map[int]string{
	KeyArrowUp: "UP", KeyArrowDown: "DOWN", KeyArrowLeft: "LEFT", KeyArrowRight: "RIGHT",
	KeyHome: "HOME", KeyEnd: "END", KeyPageUp: "PGUP", KeyPageDown: "PGDN",
	KeyInsert: "INS", KeyDeleteKey: "DEL", KeyBackTab: "BACKTAB", KeyEsc: "ESC",
	KeyF1: "F1", KeyF2: "F2", KeyF3: "F3", KeyF4: "F4", KeyF5: "F5", KeyF6: "F6",
	KeyF7: "F7", KeyF8: "F8", KeyF9: "F9", KeyF10: "F10", KeyF11: "F11", KeyF12: "F12",
}

// CSIKey returns the key for the CSI sequence "ESC [ params final", or 0 if
// it is not a key.
func CSIKey(params []byte, final byte) int {
	if final != '~' {
		return csiFinalKeys[final]
	}
	first, _, _ := bytes.Cut(params, []byte{';'})
	n, err := strconv.Atoi(string(first))
	if err != nil {
		return 0
	}
	return csiTildeKeys[n]
}

// SS3Key returns the key for the SS3 sequence "ESC O b", or 0 if it is not
// a key.
func SS3Key(b byte) int {
	return ss3Keys[b]
}

// KeyName returns the name scripts use for a special key ("UP", "PGDN",
// "F1", "ESC", ...), or "" for any other key code.
func KeyName(key int) string {
	return keyNames[key]
}
