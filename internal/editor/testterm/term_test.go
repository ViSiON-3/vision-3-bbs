package testterm

import "testing"

func TestWritePlacesTextAtCursor(t *testing.T) {
	tt := New(20, 5)

	if _, err := tt.Write([]byte("Hello")); err != nil {
		t.Fatalf("Write: %v", err)
	}

	if got := tt.Row(1); got != "Hello" {
		t.Errorf("Row(1) = %q, want %q", got, "Hello")
	}
	if row, col := tt.Cursor(); row != 1 || col != 6 {
		t.Errorf("Cursor() = (%d,%d), want (1,6)", row, col)
	}
}

func TestRowTrimsTrailingBlanks(t *testing.T) {
	tt := New(20, 5)
	tt.Write([]byte("Hi"))

	if got := tt.Row(1); got != "Hi" {
		t.Errorf("Row(1) = %q, want %q (no padding to width)", got, "Hi")
	}
	if got := tt.Row(2); got != "" {
		t.Errorf("Row(2) = %q, want empty", got)
	}
}

// Out-of-range access must not panic: a wrong assertion should report a bad
// value, not take the test binary down.
func TestCellOutOfRangeIsBlank(t *testing.T) {
	tt := New(20, 5)
	for _, c := range []struct{ row, col int }{{0, 1}, {1, 0}, {6, 1}, {1, 21}, {-3, -3}} {
		if got := tt.Cell(c.row, c.col); got.Rune != ' ' {
			t.Errorf("Cell(%d,%d).Rune = %q, want blank", c.row, c.col, got.Rune)
		}
	}
}

func TestSnapshotJoinsRowsAndDropsTrailingBlankRows(t *testing.T) {
	tt := New(10, 4)
	tt.Write([]byte("ab"))

	want := "ab"
	if got := tt.Snapshot(); got != want {
		t.Errorf("Snapshot() = %q, want %q", got, want)
	}
}

// Unrecognised sequences must be consumed (not printed as text) and recorded,
// so a test can assert the double did not silently ignore output.
func TestUnknownEscapeIsConsumedAndRecorded(t *testing.T) {
	tt := New(20, 5)
	tt.Write([]byte("A\x1b[5nB"))

	if got := tt.Row(1); got != "AB" {
		t.Errorf("Row(1) = %q, want %q — escape bytes leaked into the grid", got, "AB")
	}
	if got := tt.Unhandled(); len(got) != 1 || got[0] != "\x1b[5n" {
		t.Errorf("Unhandled() = %q, want [%q]", got, "\x1b[5n")
	}
}

// A sequence split across two Writes must still be parsed as one sequence.
func TestEscapeSplitAcrossWrites(t *testing.T) {
	tt := New(20, 5)
	tt.Write([]byte("A\x1b[5"))
	tt.Write([]byte("nB"))

	if got := tt.Row(1); got != "AB" {
		t.Errorf("Row(1) = %q, want %q", got, "AB")
	}
	if got := tt.Unhandled(); len(got) != 1 || got[0] != "\x1b[5n" {
		t.Errorf("Unhandled() = %q, want [%q]", got, "\x1b[5n")
	}
}

// Bytes left over when the stream ends mid-sequence must not vanish: without
// a trace, an incomplete escape sequence swallowed by a test double looks
// identical to one that never happened.
func TestIncompletePendingSequenceIsRecorded(t *testing.T) {
	tt := New(20, 5)
	tt.Write([]byte("\x1b[3"))

	got := tt.Unhandled()
	if len(got) != 1 || got[0] != `pending: "\x1b[3"` {
		t.Errorf("Unhandled() = %q, want [%q]", got, `pending: "\x1b[3"`)
	}
}

func TestWriteDecodesMultiByteUTF8(t *testing.T) {
	tt := New(20, 5)
	tt.Write([]byte("░é"))

	if got := tt.Cell(1, 1).Rune; got != '░' {
		t.Errorf("Cell(1,1).Rune = %q, want '░'", got)
	}
	if got := tt.Cell(1, 2).Rune; got != 'é' {
		t.Errorf("Cell(1,2).Rune = %q, want 'é'", got)
	}
	if row, col := tt.Cursor(); row != 1 || col != 3 {
		t.Errorf("Cursor() = (%d,%d), want (1,3) — one column per rune", row, col)
	}
}

func TestWriteWrapsAtRightMargin(t *testing.T) {
	tt := New(4, 3)
	tt.Write([]byte("abcdef"))

	if got := tt.Row(1); got != "abcd" {
		t.Errorf("Row(1) = %q, want %q", got, "abcd")
	}
	if got := tt.Row(2); got != "ef" {
		t.Errorf("Row(2) = %q, want %q", got, "ef")
	}
	if row, col := tt.Cursor(); row != 2 || col != 3 {
		t.Errorf("Cursor() = (%d,%d), want (2,3)", row, col)
	}
}

func TestLineFeedOnLastRowScrolls(t *testing.T) {
	tt := New(6, 3)
	tt.Write([]byte("one\r\ntwo\r\nthree"))

	if got := tt.Snapshot(); got != "one\ntwo\nthree" {
		t.Errorf("Snapshot() = %q, want %q", got, "one\ntwo\nthree")
	}

	tt.Write([]byte("\r\nfour"))

	if got := tt.Snapshot(); got != "two\nthree\nfour" {
		t.Errorf("Snapshot() = %q, want %q — top row should have scrolled off", got, "two\nthree\nfour")
	}
	if row := tt.row; row != 3 {
		t.Errorf("cursor row = %d, want 3 (stays on the last row)", row)
	}
}

func TestWrapOnLastRowScrolls(t *testing.T) {
	tt := New(3, 2)
	tt.Write([]byte("abc\r\ndef"))
	if got := tt.Snapshot(); got != "abc\ndef" {
		t.Fatalf("setup: Snapshot() = %q, want %q", got, "abc\ndef")
	}

	tt.Write([]byte("g")) // wraps past the last column on the last row

	if got := tt.Snapshot(); got != "def\ng" {
		t.Errorf("Snapshot() = %q, want %q", got, "def\ng")
	}
}

func TestCarriageReturnAndBackspaceAndTab(t *testing.T) {
	tt := New(20, 3)
	tt.Write([]byte("abc\rX"))
	if got := tt.Row(1); got != "Xbc" {
		t.Errorf("Row(1) = %q, want %q", got, "Xbc")
	}

	tt2 := New(20, 3)
	tt2.Write([]byte("abc\b\bX"))
	if got := tt2.Row(1); got != "aXc" {
		t.Errorf("Row(1) = %q, want %q", got, "aXc")
	}

	tt3 := New(20, 3)
	tt3.Write([]byte("ab\tX"))
	if got := tt3.Cell(1, 9).Rune; got != 'X' {
		t.Errorf("Cell(1,9).Rune = %q, want 'X' (tab stop at column 9)", got)
	}
}

// 0xB0 is ░ and 0xDB is █ in CP437. Both are single bytes on the wire.
func TestCP437ModeDecodesHighBytes(t *testing.T) {
	tt := New(10, 3, CP437())
	tt.Write([]byte{0xB0, 0xDB, 'A'})

	if got := tt.Cell(1, 1).Rune; got != '░' {
		t.Errorf("Cell(1,1).Rune = %q, want '░'", got)
	}
	if got := tt.Cell(1, 2).Rune; got != '█' {
		t.Errorf("Cell(1,2).Rune = %q, want '█'", got)
	}
	if got := tt.Cell(1, 3).Rune; got != 'A' {
		t.Errorf("Cell(1,3).Rune = %q, want 'A'", got)
	}
	if row, col := tt.Cursor(); row != 1 || col != 4 {
		t.Errorf("Cursor() = (%d,%d), want (1,4) — one column per byte", row, col)
	}
}

// A CP437 byte pair that happens to be valid UTF-8 must still decode as two
// separate glyphs. 0xDB 0xB2 is █▓ in CP437 but U+06F2 read as UTF-8.
func TestCP437ModeDoesNotMisreadBytePairsAsUTF8(t *testing.T) {
	tt := New(10, 3, CP437())
	tt.Write([]byte{0xDB, 0xB2})

	if got := tt.Cell(1, 1).Rune; got != '█' {
		t.Errorf("Cell(1,1).Rune = %q, want '█'", got)
	}
	if got := tt.Cell(1, 2).Rune; got != '▓' {
		t.Errorf("Cell(1,2).Rune = %q, want '▓'", got)
	}
}

// Escape sequences are ASCII and must be interpreted the same in CP437 mode.
func TestCP437ModeStillParsesEscapes(t *testing.T) {
	tt := New(10, 3, CP437())
	tt.Write([]byte("\x1b[2;3HX"))

	if got := tt.Cell(2, 3).Rune; got != 'X' {
		t.Errorf("Cell(2,3).Rune = %q, want 'X'", got)
	}
	if got := tt.Unhandled(); len(got) != 0 {
		t.Errorf("Unhandled() = %q, want empty", got)
	}
}
