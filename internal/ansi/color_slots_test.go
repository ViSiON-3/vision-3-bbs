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

// PipeCodeAt, which the string editor's preview reads codes with, gives a slot
// code the board's current default, the same as ReplacePipeCodes draws.
func TestPipeCodeAtFollowsBoardSlotDefaults(t *testing.T) {
	t.Cleanup(func() { SetColorSlotDefaults([7]string{}) })
	SetColorSlotDefaults([7]string{"|12|B4"})
	code, expansion, n := PipeCodeAt([]byte("|C1x"), 0)
	want := string(ReplacePipeCodes([]byte("|12|B4")))
	if code != "|C1" || n != 3 || expansion != want {
		t.Errorf("PipeCodeAt = %q, %q, %d; want |C1, %q, 3", code, expansion, n, want)
	}
	if got := string(ReplacePipeCodes([]byte("|C1x"))); got != want+"x" {
		t.Errorf("ReplacePipeCodes = %q, want %q", got, want+"x")
	}
}

// The Bar slot's built-in colour is the stock lightbar highlight, white on
// blue. |B codes follow ANSI order, so blue is |B4.
func TestBarSlotIsTheLightbarHighlight(t *testing.T) {
	if got, want := string(ReplacePipeCodes([]byte("|C7"))), "\x1b[1;37m\x1b[44m"; got != want {
		t.Errorf("|C7 = %q, want white on blue %q", got, want)
	}
}
