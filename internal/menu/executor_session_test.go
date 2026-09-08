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

// TestReadLineFromSessionIH_StrayLeadByteDoesNotEatNextChar reproduces the
// bug found in review: utf8Pending was only cleared by the extended-byte
// branch and by Backspace. A stray valid-looking lead byte (0xC9 here, which
// wants one continuation byte) left pending by itself; the ASCII branch for
// 'A' appended and echoed 'A' but never touched utf8Pending, so the next
// extended byte (0xC3, the lead byte of a legitimate "é") was appended to
// the STALE pending buffer instead of starting fresh. 0xC9+0xC3 is not a
// valid sequence, so it was dropped as malformed -- silently eating the
// leading byte of "é" -- and the trailing continuation byte 0xA9 was then
// dropped too as an orphaned continuation byte with no lead. Net effect:
// "é" vanished with no echo and no error, and the stored line was "A", not
// "Aé". Reachable whenever a UTF-8-mode session's client sends a high byte
// with no valid continuation, e.g. auto-detection misreading a raw-CP437
// client as modern.
func TestReadLineFromSessionIH_StrayLeadByteDoesNotEatNextChar(t *testing.T) {
	ts := newTestSession("\xc9A\xc3\xa9\r")
	SetSessionOutputMode(ts, ansi.OutputModeUTF8)
	terminal := newTestTerminal(ts)

	line, err := readLineFromSessionIH(ts, terminal)
	if err != nil {
		t.Fatalf("readLineFromSessionIH: %v", err)
	}
	if !utf8.ValidString(line) {
		t.Fatalf("line is not valid UTF-8: %q", line)
	}
	if line != "Aé" {
		t.Errorf("line = %q, want %q (stray lead byte must not swallow the next legitimate character)", line, "Aé")
	}
}

// TestReadLineFromSessionIH_BackspaceInterruptsPendingUTF8Sequence verifies
// the case right next to the bug above: Backspace arriving while a partial
// multi-byte sequence is buffered (nothing appended to line or echoed yet)
// must discard only the pending bytes -- not touch line, not echo "\b \b" --
// and normal typing afterward must resume cleanly.
func TestReadLineFromSessionIH_BackspaceInterruptsPendingUTF8Sequence(t *testing.T) {
	// 0xC3 is a valid lead byte awaiting one continuation byte; Backspace
	// arrives before that continuation byte, then 'x', then Enter.
	ts := newTestSession("\xc3\x08x\r")
	SetSessionOutputMode(ts, ansi.OutputModeUTF8)
	terminal := newTestTerminal(ts)

	line, err := readLineFromSessionIH(ts, terminal)
	if err != nil {
		t.Fatalf("readLineFromSessionIH: %v", err)
	}
	if line != "x" {
		t.Errorf("line = %q, want %q (backspace on a pending sequence must not affect line)", line, "x")
	}
	if n := strings.Count(ts.output(), "\b \b"); n != 0 {
		t.Errorf("output contains %d \\b \\b sequences, want 0 (nothing was displayed for the pending byte yet)", n)
	}
}

// A stray lead byte must not take a following valid character down with it.
// Dropping the whole pending buffer on a malformed decode discards the next
// lead byte too; the decoder has to resynchronise by dropping one byte and
// retrying, the way any UTF-8 decoder does.
func TestReadLineFromSessionIH_ResyncsAfterStrayLeadByte(t *testing.T) {
	ts := newTestSession("\xc3" + "日" + "\r") // stray 2-byte lead, then a valid "日"
	terminal := newTestTerminal(ts)
	SetSessionOutputMode(ts, ansi.OutputModeUTF8)
	t.Cleanup(func() { ClearSessionOutputMode(ts) })

	got, err := readLineFromSessionIH(ts, terminal)
	if err != nil {
		t.Fatalf("readLineFromSessionIH: %v", err)
	}
	if got != "日" {
		t.Errorf("line = %q, want %q — the stray lead byte swallowed the next character", got, "日")
	}
}

// --- readLineFromSessionIHMax: rune-counted input cap ---

// TestReadLineFromSessionIHMax_ASCIIStopsAtLimit is the basic contract: once
// the line holds maxLen runes, further printable keystrokes are dropped and
// not echoed, and Enter still terminates the read normally.
func TestReadLineFromSessionIHMax_ASCIIStopsAtLimit(t *testing.T) {
	ts := newTestSession("abcde\r")
	terminal := newTestTerminal(ts)

	line, err := readLineFromSessionIHMax(ts, terminal, 3)
	if err != nil {
		t.Fatalf("readLineFromSessionIHMax: %v", err)
	}
	if line != "abc" {
		t.Errorf("line = %q, want %q", line, "abc")
	}
	if out := ts.output(); strings.ContainsAny(out, "de") {
		t.Errorf("output = %q, dropped keystrokes must not be echoed", out)
	}
}

// TestReadLineFromSessionIHMax_BackspaceAfterLimitFreesRoom verifies the
// limit is a live check on the current line, not a latch: after a dropped
// keystroke, Backspace still removes a rune and typing resumes into the
// freed slot.
func TestReadLineFromSessionIHMax_BackspaceAfterLimitFreesRoom(t *testing.T) {
	// 'a','b','c' fill the line; 'd' is dropped; Backspace removes 'c';
	// 'e' is accepted into the freed slot.
	ts := newTestSession("abcd\x08e\r")
	terminal := newTestTerminal(ts)

	line, err := readLineFromSessionIHMax(ts, terminal, 3)
	if err != nil {
		t.Fatalf("readLineFromSessionIHMax: %v", err)
	}
	if line != "abe" {
		t.Errorf("line = %q, want %q", line, "abe")
	}
	if n := strings.Count(ts.output(), "\b \b"); n != 1 {
		t.Errorf("output contains %d \\b \\b sequences, want exactly 1", n)
	}
}

// TestReadLineFromSessionIHMax_CP437CountsRunesNotBytes types three CP437
// 'é' (0x82) with a cap of 2. Each is stored as a 2-byte UTF-8 rune, so a
// byte-counted limit would stop after one; a rune-counted one keeps two.
func TestReadLineFromSessionIHMax_CP437CountsRunesNotBytes(t *testing.T) {
	ts := newTestSession("\x82\x82\x82\r")
	SetSessionOutputMode(ts, ansi.OutputModeCP437)
	terminal := newTestTerminal(ts)

	line, err := readLineFromSessionIHMax(ts, terminal, 2)
	if err != nil {
		t.Fatalf("readLineFromSessionIHMax: %v", err)
	}
	if line != "éé" {
		t.Errorf("line = %q, want %q", line, "éé")
	}
	if n := utf8.RuneCountInString(line); n != 2 {
		t.Errorf("line has %d runes, want 2", n)
	}
	if n := strings.Count(ts.output(), "\x82"); n != 2 {
		t.Errorf("output echoed 0x82 %d times, want 2 (third must be dropped)", n)
	}
}

// TestReadLineFromSessionIHMax_UTF8RuneCompletesAtLimit drives 'a' then the
// three bytes of "日" with a cap of 2. The rune starts while there is still
// room and must be assembled whole, taking the line to exactly maxLen; the
// following 'b' must then be dropped.
func TestReadLineFromSessionIHMax_UTF8RuneCompletesAtLimit(t *testing.T) {
	ts := newTestSession("a\xe6\x97\xa5b\r")
	SetSessionOutputMode(ts, ansi.OutputModeUTF8)
	terminal := newTestTerminal(ts)

	line, err := readLineFromSessionIHMax(ts, terminal, 2)
	if err != nil {
		t.Fatalf("readLineFromSessionIHMax: %v", err)
	}
	if !utf8.ValidString(line) {
		t.Fatalf("line is not valid UTF-8: %q", line)
	}
	if line != "a日" {
		t.Errorf("line = %q, want %q", line, "a日")
	}
	if strings.Contains(ts.output(), "b") {
		t.Errorf("output = %q, 'b' past the limit must not be echoed", ts.output())
	}
}

// TestReadLineFromSessionIHMax_UTF8SequenceAfterLimitFullyDropped sends a
// complete 2-byte "é" (C3 A9) once the line is already full. Both bytes must
// be swallowed: the lead byte must not be buffered as pending, and the
// continuation byte must not survive to corrupt or extend the line.
func TestReadLineFromSessionIHMax_UTF8SequenceAfterLimitFullyDropped(t *testing.T) {
	ts := newTestSession("a\xc3\xa9\r")
	SetSessionOutputMode(ts, ansi.OutputModeUTF8)
	terminal := newTestTerminal(ts)

	line, err := readLineFromSessionIHMax(ts, terminal, 1)
	if err != nil {
		t.Fatalf("readLineFromSessionIHMax: %v", err)
	}
	if line != "a" {
		t.Errorf("line = %q, want %q", line, "a")
	}
	if !utf8.ValidString(line) {
		t.Fatalf("line is not valid UTF-8: %q", line)
	}
	if strings.Contains(ts.output(), "\xc3") || strings.Contains(ts.output(), "\xa9") {
		t.Errorf("output = %q, dropped multi-byte sequence must not be echoed", ts.output())
	}
}

// TestReadLineFromSessionIHMax_ZeroMeansUnlimited guards the default used by
// readLineFromSessionIH and readLineFromSessionIHAllowAbort: maxLen 0 must
// impose no cap at all.
func TestReadLineFromSessionIHMax_ZeroMeansUnlimited(t *testing.T) {
	long := strings.Repeat("x", 200)
	ts := newTestSession(long + "\r")
	terminal := newTestTerminal(ts)

	line, err := readLineFromSessionIHMax(ts, terminal, 0)
	if err != nil {
		t.Fatalf("readLineFromSessionIHMax: %v", err)
	}
	if line != long {
		t.Errorf("line has %d runes, want 200 (maxLen 0 must be unlimited)", utf8.RuneCountInString(line))
	}
}
