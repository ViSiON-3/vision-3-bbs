package menu

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
)

// --- session output mode plumbing ---

// TestSessionOutputModeDefaultsToCP437 verifies the safe default for a
// session that never called SetSessionOutputMode: CP437, because most users
// are on CP437 terminals and a CP437 byte is never mistaken for part of a
// UTF-8 continuation sequence.
func TestSessionOutputModeDefaultsToCP437(t *testing.T) {
	ts := newTestSession("")
	if got := sessionOutputMode(ts); got != ansi.OutputModeCP437 {
		t.Errorf("sessionOutputMode() default = %v, want OutputModeCP437", got)
	}
}

func TestSetSessionOutputModeOverridesDefault(t *testing.T) {
	ts := newTestSession("")
	SetSessionOutputMode(ts, ansi.OutputModeUTF8)
	if got := sessionOutputMode(ts); got != ansi.OutputModeUTF8 {
		t.Errorf("sessionOutputMode() = %v, want OutputModeUTF8", got)
	}
}

// --- readLineFromSessionIH: CP437 round trip (the acceptance test) ---

// TestReadLineFromSessionIH_CP437RoundTrips is the acceptance test for the
// whole feature: a CP437 user types the raw byte 0x82 (code page 437's
// 'é'). It must be STORED as the UTF-8 encoding of 'é' (so users.json and
// friends only ever hold valid UTF-8) and ECHOED back as the same raw byte
// 0x82 -- not re-encoded -- because a CP437 terminal only understands its
// own single-byte glyph table.
func TestReadLineFromSessionIH_CP437RoundTrips(t *testing.T) {
	ts := newTestSession("caf\x82\r")
	SetSessionOutputMode(ts, ansi.OutputModeCP437)
	terminal := newTestTerminal(ts)

	line, err := readLineFromSessionIH(ts, terminal)
	if err != nil {
		t.Fatalf("readLineFromSessionIH: %v", err)
	}
	if line != "café" {
		t.Errorf("line = %q, want %q", line, "café")
	}
	if !strings.Contains(ts.output(), "caf\x82") {
		t.Errorf("output = %q, want raw byte 0x82 echoed back unchanged", ts.output())
	}
}

// TestReadLineFromSessionIHAllowAbort_CP437RoundTrips guards the sibling
// reader against the same bug; the two functions share almost identical
// loops and must not drift.
func TestReadLineFromSessionIHAllowAbort_CP437RoundTrips(t *testing.T) {
	ts := newTestSession("caf\x82\r")
	SetSessionOutputMode(ts, ansi.OutputModeCP437)
	terminal := newTestTerminal(ts)

	line, err := readLineFromSessionIHAllowAbort(ts, terminal)
	if err != nil {
		t.Fatalf("readLineFromSessionIHAllowAbort: %v", err)
	}
	if line != "café" {
		t.Errorf("line = %q, want %q", line, "café")
	}
}

// TestDecodeExtendedKey_CP437UnmappedByteDropped exercises the drop path for
// a CP437 byte whose Cp437ToUnicode entry is 0 directly against the decode
// helper: it must not be stored (which would put invalid/unintended data
// into users.json) and must not be echoed, matching how the reader silently
// drops anything else it won't accept.
func TestDecodeExtendedKey_CP437UnmappedByteDropped(t *testing.T) {
	line, echo, pending := decodeExtendedKey(nil, ansi.OutputModeCP437, 0, nil)
	if len(line) != 0 {
		t.Errorf("line = %q, want empty (unmapped byte must not be stored)", line)
	}
	if len(echo) != 0 {
		t.Errorf("echo = %v, want nil (unmapped byte must not be echoed)", echo)
	}
	if len(pending) != 0 {
		t.Errorf("pending = %v, want nil", pending)
	}
}

// TestReadLineFromSessionIH_UTF8ModeAccumulatesMultiByteRune drives the three
// bytes of "日" (U+65E5, encoded E6 97 A5) through one at a time -- exactly
// how they arrive over a real connection, one byte per ReadKey call -- and
// expects them assembled into a single stored rune.
func TestReadLineFromSessionIH_UTF8ModeAccumulatesMultiByteRune(t *testing.T) {
	ts := newTestSession("\xe6\x97\xa5\r")
	SetSessionOutputMode(ts, ansi.OutputModeUTF8)
	terminal := newTestTerminal(ts)

	line, err := readLineFromSessionIH(ts, terminal)
	if err != nil {
		t.Fatalf("readLineFromSessionIH: %v", err)
	}
	if line != "日" {
		t.Errorf("line = %q (%d bytes), want \"日\"", line, len(line))
	}
	if n := utf8.RuneCountInString(line); n != 1 {
		t.Errorf("line has %d runes, want 1", n)
	}
	if !strings.Contains(ts.output(), "\xe6\x97\xa5") {
		t.Errorf("output = %q, want the complete UTF-8 sequence echoed back", ts.output())
	}
}

// TestReadLineFromSessionIH_BackspaceRemovesWholeMultiByteChar is the
// backspace hazard test called out in the design brief: before this fix,
// backspace deleted exactly one BYTE (line = line[:len(line)-1]), which would
// cut a multi-byte character in half instead of removing it whole.
func TestReadLineFromSessionIH_BackspaceRemovesWholeMultiByteChar(t *testing.T) {
	// Type 'é' (CP437 0x82), backspace, then 'x', then Enter.
	ts := newTestSession("\x82\x08x\r")
	SetSessionOutputMode(ts, ansi.OutputModeCP437)
	terminal := newTestTerminal(ts)

	line, err := readLineFromSessionIH(ts, terminal)
	if err != nil {
		t.Fatalf("readLineFromSessionIH: %v", err)
	}
	if line != "x" {
		t.Errorf("line = %q, want %q (backspace must remove the whole rune, not one byte)", line, "x")
	}
	if !utf8.ValidString(line) {
		t.Fatalf("line is not valid UTF-8: %q", line)
	}
	if n := strings.Count(ts.output(), "\b \b"); n != 1 {
		t.Errorf("output contains %d \\b \\b sequences, want exactly 1 (one column erased)", n)
	}
}

// TestReadLineFromSessionIH_ASCIIUnchanged is a regression guard: ASCII
// input, the overwhelmingly common case, must behave exactly as before.
func TestReadLineFromSessionIH_ASCIIUnchanged(t *testing.T) {
	ts := newTestSession("hello\x08\x08\r")
	terminal := newTestTerminal(ts)

	line, err := readLineFromSessionIH(ts, terminal)
	if err != nil {
		t.Fatalf("readLineFromSessionIH: %v", err)
	}
	if line != "hel" {
		t.Errorf("line = %q, want %q", line, "hel")
	}
}
