package tea

import (
	"bytes"
	"context"
	"fmt"
	"testing"
)

// readAllMsgs runs the real ANSI input reader over input and returns every
// message it produced before the reader hit EOF.
func readAllMsgs(t *testing.T, input []byte) []Msg {
	t.Helper()
	msgs := make(chan Msg, 64)
	_ = readAnsiInputs(context.Background(), msgs, bytes.NewReader(input))
	close(msgs)
	var out []Msg
	for m := range msgs {
		out = append(out, m)
	}
	return out
}

// TestShiftedFunctionKeySequences covers the local modification that decodes
// xterm-style shifted function keys as KeyShiftF1..KeyShiftF12 while leaving
// the ambiguous vt220/rxvt/Linux-console F13..F20 codes alone.
func TestShiftedFunctionKeySequences(t *testing.T) {
	tests := []struct {
		seq  string
		typ  KeyType
		alt  bool
		name string
	}{
		// xterm CSI form, Shift ("2").
		{"\x1b[1;2P", KeyShiftF1, false, "shift+f1"},
		{"\x1b[1;2Q", KeyShiftF2, false, "shift+f2"},
		{"\x1b[1;2R", KeyShiftF3, false, "shift+f3"},
		{"\x1b[1;2S", KeyShiftF4, false, "shift+f4"},
		{"\x1b[15;2~", KeyShiftF5, false, "shift+f5"},
		{"\x1b[17;2~", KeyShiftF6, false, "shift+f6"},
		{"\x1b[18;2~", KeyShiftF7, false, "shift+f7"},
		{"\x1b[19;2~", KeyShiftF8, false, "shift+f8"},
		{"\x1b[20;2~", KeyShiftF9, false, "shift+f9"},
		{"\x1b[21;2~", KeyShiftF10, false, "shift+f10"},
		{"\x1b[23;2~", KeyShiftF11, false, "shift+f11"},
		{"\x1b[24;2~", KeyShiftF12, false, "shift+f12"},

		// Older xterm SS3 form with a modifier.
		{"\x1bO2P", KeyShiftF1, false, "shift+f1"},
		{"\x1bO2Q", KeyShiftF2, false, "shift+f2"},
		{"\x1bO2R", KeyShiftF3, false, "shift+f3"},
		{"\x1bO2S", KeyShiftF4, false, "shift+f4"},

		// Alt+Shift ("4").
		{"\x1b[1;4P", KeyShiftF1, true, "alt+shift+f1"},
		{"\x1b[1;4S", KeyShiftF4, true, "alt+shift+f4"},
		{"\x1b[15;4~", KeyShiftF5, true, "alt+shift+f5"},
		{"\x1b[21;4~", KeyShiftF10, true, "alt+shift+f10"},
		{"\x1b[24;4~", KeyShiftF12, true, "alt+shift+f12"},

		// ESC-prefixed (meta) form of a shifted key.
		{"\x1b\x1b[1;2Q", KeyShiftF2, true, "alt+shift+f2"},

		// Unshifted keys are unchanged.
		{"\x1bOP", KeyF1, false, "f1"},
		{"\x1b[21~", KeyF10, false, "f10"},
		{"\x1b[21;3~", KeyF10, true, "alt+f10"},

		// vt220/rxvt/Linux-console codes stay F13..F20: their meaning
		// (real F13+ or Shift+F(n-10)) depends on the terminal.
		{"\x1b[25~", KeyF13, false, "f13"},
		{"\x1b[26~", KeyF14, false, "f14"},
		{"\x1b[28~", KeyF15, false, "f15"},
		{"\x1b[29~", KeyF16, false, "f16"},
		{"\x1b[31~", KeyF17, false, "f17"},
		{"\x1b[32~", KeyF18, false, "f18"},
		{"\x1b[33~", KeyF19, false, "f19"},
		{"\x1b[34~", KeyF20, false, "f20"},
	}
	for _, tc := range tests {
		t.Run(fmt.Sprintf("%s %q", tc.name, tc.seq), func(t *testing.T) {
			msgs := readAllMsgs(t, []byte(tc.seq))
			if len(msgs) != 1 {
				t.Fatalf("got %d messages %#v, want 1", len(msgs), msgs)
			}
			km, ok := msgs[0].(KeyMsg)
			if !ok {
				t.Fatalf("got %T %#v, want KeyMsg", msgs[0], msgs[0])
			}
			if km.Type != tc.typ || km.Alt != tc.alt {
				t.Errorf("got Type=%v Alt=%v, want Type=%v Alt=%v", km.Type, km.Alt, tc.typ, tc.alt)
			}
			if got := km.String(); got != tc.name {
				t.Errorf("String() = %q, want %q", got, tc.name)
			}
		})
	}
}

// TestShiftedFunctionKeyNames checks every new key type has its own name and
// that the new constants follow KeyF20 (so upstream key values are unchanged).
func TestShiftedFunctionKeyNames(t *testing.T) {
	want := []string{
		"shift+f1", "shift+f2", "shift+f3", "shift+f4", "shift+f5", "shift+f6",
		"shift+f7", "shift+f8", "shift+f9", "shift+f10", "shift+f11", "shift+f12",
	}
	// "Other keys" count down from -1, so later constants are smaller.
	for i, name := range want {
		k := KeyShiftF1 - KeyType(i)
		if got := k.String(); got != name {
			t.Errorf("KeyType %d String() = %q, want %q", k, got, name)
		}
	}
	if KeyShiftF1 != KeyF20-1 {
		t.Errorf("KeyShiftF1=%d, want KeyF20-1=%d", KeyShiftF1, KeyF20-1)
	}
}
