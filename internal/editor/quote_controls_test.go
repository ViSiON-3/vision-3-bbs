package editor

import (
	"strings"
	"testing"
)

// stripControl keeps raw CP437, where 0x9B is a cent sign, and drops the same
// character only in its UTF-8 form, where it is CSI.
func TestStripControlDropsC1(t *testing.T) {
	for in, want := range map[string]string{
		"a\u009bb":       "ab",
		"a\u0085\u009fb": "ab",
		"a\x07\x7fb":     "ab",
		"5\x9b \x80\xdb": "5\x9b \x80\xdb",
		"café":           "café",
	} {
		if got := stripControl(in); got != want {
			t.Errorf("stripControl(%q) = %q, want %q", in, got, want)
		}
	}
}

// A quoted line carries nothing a terminal would act on, into the picker or the
// reply, and none of a string's payload.
func TestCleanQuoteLineDropsControls(t *testing.T) {
	ch := &CommandHandler{}
	for in, want := range map[string]string{
		"a\u009b2Jb":                "ab",
		"a\u009d0;PAYLOAD\u009cb":   "ab",
		"a\x1b]0;PAYLOAD\x07b":      "ab",
		"a\x07\x05b":                "ab",
		"a\tb\t\r":                  "a b",
		"a\x1b|01[2Jb":              "ab",
		"cent 5\x9b \xdb\xb0":       "cent 5\x9b \xdb\xb0",
		"\x1b[1;31mred\x1b[0m text": "red text",
	} {
		if got := ch.cleanQuoteLine(in); got != want {
			t.Errorf("cleanQuoteLine(%q) = %q, want %q", in, got, want)
		}
	}
}

// Through the real picker: what it draws and what lands in the reply.
func TestQuotePickerDropsBodyControls(t *testing.T) {
	tt, ch, ih, cleanup := newQuoteHarness(t, " \x1b", []string{"OPEN\u009b6n\x1b]0;PAYLOAD\x07\x07CLOSE"})
	defer cleanup()

	ch.HandleQuote(ih, 1, 1)

	if got := ch.buffer.GetContent(); !strings.Contains(got, "OPENCLOSE") {
		t.Errorf("reply = %q", got)
	}
	if screen := tt.Snapshot(); strings.Contains(screen, "PAYLOAD") || strings.Contains(screen, "6n") {
		t.Errorf("the picker drew:\n%s", screen)
	}
	if got := tt.Unhandled(); len(got) != 0 {
		t.Errorf("the picker sent sequences the terminal does not handle: %q", got)
	}
}
