package ansi

import (
	"bytes"
	"fmt"
	"testing"
)

// |C1 to |C7 expand to their built-in colours, next to the codes they share a
// first letter with: |CL and |CR still act, and |CA and |CC, which only menus
// fill in, are left as written.
func TestColorSlotCodesExpand(t *testing.T) {
	for i, def := range ColorSlotDefaults {
		code := fmt.Sprintf("|C%d", i+1)
		got := ReplacePipeCodes([]byte(code + "x"))
		want := ReplacePipeCodes([]byte(def + "x"))
		if !bytes.Equal(got, want) {
			t.Errorf("%s = %q, want %s's %q", code, got, def, want)
		}
	}
	got := string(ReplacePipeCodes([]byte("|CL|C1a|CRb|CAc|CCd|C8e|C0f")))
	want := "\x1b[2J\x1b[H" + string(ReplacePipeCodes([]byte("|07"))) + "a\r\nb|CAc|CCd|C8e|C0f"
	if got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

// A caller's text can't use a slot code: it is escaped like a screen code.
func TestEscapeScreenPipeCodesEscapesColorSlots(t *testing.T) {
	if got := string(ReplacePipeCodes([]byte(EscapeScreenPipeCodes("|C1x")))); got != "|C1x" {
		t.Errorf("got %q", got)
	}
}
