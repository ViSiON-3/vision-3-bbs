package testterm

import "testing"

func TestAbsoluteCursorPositioning(t *testing.T) {
	tt := New(20, 10)
	tt.Write([]byte("\x1b[3;5HX"))

	if got := tt.Cell(3, 5).Rune; got != 'X' {
		t.Errorf("Cell(3,5).Rune = %q, want 'X'", got)
	}
	if row, col := tt.Cursor(); row != 3 || col != 6 {
		t.Errorf("Cursor() = (%d,%d), want (3,6)", row, col)
	}
}

// ESC[H with no parameters homes the cursor.
func TestCursorHomeWithoutParams(t *testing.T) {
	tt := New(20, 10)
	tt.Write([]byte("\x1b[5;5Hxx\x1b[HA"))

	if got := tt.Cell(1, 1).Rune; got != 'A' {
		t.Errorf("Cell(1,1).Rune = %q, want 'A'", got)
	}
}

func TestRelativeCursorMovement(t *testing.T) {
	tt := New(20, 10)
	// Home, down 2, right 3, write, up 1, left 2, write.
	tt.Write([]byte("\x1b[H\x1b[2B\x1b[3CX\x1b[1A\x1b[2DY"))

	if got := tt.Cell(3, 4).Rune; got != 'X' {
		t.Errorf("Cell(3,4).Rune = %q, want 'X'", got)
	}
	if got := tt.Cell(2, 3).Rune; got != 'Y' {
		t.Errorf("Cell(2,3).Rune = %q, want 'Y'", got)
	}
}

// A movement with no parameter moves by one.
func TestRelativeMovementDefaultsToOne(t *testing.T) {
	tt := New(20, 10)
	tt.Write([]byte("\x1b[5;5H\x1b[BX"))

	if got := tt.Cell(6, 5).Rune; got != 'X' {
		t.Errorf("Cell(6,5).Rune = %q, want 'X'", got)
	}
}

// Movement must not walk off the grid.
func TestCursorMovementClampsToScreen(t *testing.T) {
	tt := New(10, 4)
	tt.Write([]byte("\x1b[1;1H\x1b[9A\x1b[9D"))
	if row, col := tt.Cursor(); row != 1 || col != 1 {
		t.Errorf("Cursor() = (%d,%d), want (1,1)", row, col)
	}

	tt.Write([]byte("\x1b[99;99H"))
	if row, col := tt.Cursor(); row != 4 || col != 10 {
		t.Errorf("Cursor() = (%d,%d), want (4,10)", row, col)
	}
}

// This is the exact shape updateDynamicHeaderFields uses: save, jump, write,
// restore.
func TestSaveAndRestoreCursor(t *testing.T) {
	tt := New(20, 10)
	tt.Write([]byte("\x1b[4;2HAB\x1b[s\x1b[1;1HZ\x1b[uC"))

	if got := tt.Row(1); got != "Z" {
		t.Errorf("Row(1) = %q, want %q", got, "Z")
	}
	if got := tt.Row(4); got != " ABC" {
		t.Errorf("Row(4) = %q, want %q", got, " ABC")
	}
}

func TestCursorVisibility(t *testing.T) {
	tt := New(20, 10)
	if !tt.CursorVisible() {
		t.Error("cursor should start visible")
	}
	tt.Write([]byte("\x1b[?25l"))
	if tt.CursorVisible() {
		t.Error("cursor should be hidden after ESC[?25l")
	}
	tt.Write([]byte("\x1b[?25h"))
	if !tt.CursorVisible() {
		t.Error("cursor should be visible after ESC[?25h")
	}
	if got := tt.Unhandled(); len(got) != 0 {
		t.Errorf("Unhandled() = %q, want empty", got)
	}
}

// ESC[K is what Screen.ClearEOL emits.
func TestEraseToEndOfLine(t *testing.T) {
	tt := New(10, 3)
	tt.Write([]byte("abcdef\x1b[1;4H\x1b[K"))

	if got := tt.Row(1); got != "abc" {
		t.Errorf("Row(1) = %q, want %q", got, "abc")
	}
}

func TestEraseToStartOfLine(t *testing.T) {
	tt := New(10, 3)
	tt.Write([]byte("abcdef\x1b[1;3H\x1b[1K"))

	// Columns 1..3 cleared, the rest kept.
	if got := tt.Row(1); got != "   def" {
		t.Errorf("Row(1) = %q, want %q", got, "   def")
	}
}

func TestEraseWholeLine(t *testing.T) {
	tt := New(10, 3)
	tt.Write([]byte("abcdef\x1b[2;1Hxyz\x1b[1;1H\x1b[2K"))

	if got := tt.Row(1); got != "" {
		t.Errorf("Row(1) = %q, want empty", got)
	}
	if got := tt.Row(2); got != "xyz" {
		t.Errorf("Row(2) = %q, want %q — other rows must be untouched", got, "xyz")
	}
}

// ESC[2J ESC[H is what ansi.ClearScreen() emits and Screen.ClearScreen uses.
func TestEraseWholeDisplayAndHome(t *testing.T) {
	tt := New(10, 3)
	tt.Write([]byte("aaa\x1b[2;1Hbbb\x1b[2J\x1b[H"))

	if got := tt.Snapshot(); got != "" {
		t.Errorf("Snapshot() = %q, want empty", got)
	}
	if row, col := tt.Cursor(); row != 1 || col != 1 {
		t.Errorf("Cursor() = (%d,%d), want (1,1)", row, col)
	}
}

func TestEraseDisplayToEndAndToStart(t *testing.T) {
	tt := New(5, 3)
	tt.Write([]byte("aaaaa\x1b[2;1Hbbbbb\x1b[3;1Hccccc"))
	tt.Write([]byte("\x1b[2;3H\x1b[J")) // erase from cursor to end of screen

	if got := tt.Row(1); got != "aaaaa" {
		t.Errorf("Row(1) = %q, want unchanged %q", got, "aaaaa")
	}
	if got := tt.Row(2); got != "bb" {
		t.Errorf("Row(2) = %q, want %q", got, "bb")
	}
	if got := tt.Row(3); got != "" {
		t.Errorf("Row(3) = %q, want empty", got)
	}

	tt2 := New(5, 3)
	tt2.Write([]byte("aaaaa\x1b[2;1Hbbbbb\x1b[3;1Hccccc"))
	tt2.Write([]byte("\x1b[2;3H\x1b[1J")) // erase from start of screen to cursor

	if got := tt2.Row(1); got != "" {
		t.Errorf("Row(1) = %q, want empty", got)
	}
	if got := tt2.Row(2); got != "   bb" {
		t.Errorf("Row(2) = %q, want %q", got, "   bb")
	}
	if got := tt2.Row(3); got != "ccccc" {
		t.Errorf("Row(3) = %q, want unchanged %q", got, "ccccc")
	}
}

// An unrecognised erase mode must not be a silent no-op.
func TestUnrecognisedEraseModesAreRecorded(t *testing.T) {
	tt := New(10, 3)
	tt.Write([]byte("\x1b[9K"))
	if got := tt.Unhandled(); len(got) != 1 || got[0] != "\x1b[9K" {
		t.Errorf("Unhandled() after ESC[9K = %q, want [%q]", got, "\x1b[9K")
	}

	tt2 := New(10, 3)
	tt2.Write([]byte("\x1b[5J"))
	if got := tt2.Unhandled(); len(got) != 1 || got[0] != "\x1b[5J" {
		t.Errorf("Unhandled() after ESC[5J = %q, want [%q]", got, "\x1b[5J")
	}
}

// An explicit 0 count on a relative move must still move by one, matching
// real terminals — only an omitted parameter is the "default to 1" case.
func TestRelativeMovementExplicitZeroStillMovesOne(t *testing.T) {
	tt := New(20, 10)
	tt.Write([]byte("\x1b[5;5H\x1b[0BX"))

	if got := tt.Cell(6, 5).Rune; got != 'X' {
		t.Errorf("Cell(6,5).Rune = %q, want 'X' — ESC[0B must move down by one", got)
	}
}

// A parameter body byte that is neither a digit nor ';' must not be silently
// dropped: skipping it concatenates the digits on either side into a
// different, wrong parameter. This is the colon sub-parameter form used by
// 24-bit/indexed colour (e.g. ESC[38:2:R:G:Bm); nothing in this codebase
// emits it, but a parser that mis-happens to produce a valid-looking
// parameter from it would silently apply the wrong thing while Unhandled()
// stayed empty.
func TestCsiColonSubParametersAreUnhandledNotMisapplied(t *testing.T) {
	// Without the fix this reads as the single parameter "22" (bold off),
	// which SGR does recognise — so it would be silently applied.
	tt := New(20, 10)
	tt.Write([]byte("\x1b[1m\x1b[2:2m"))

	if !tt.pen.Bold {
		t.Error("ESC[2:2m must not be applied as SGR 22 (bold off) via concatenated digits")
	}
	if got := tt.Unhandled(); len(got) != 1 || got[0] != "\x1b[2:2m" {
		t.Errorf("Unhandled() = %q, want [%q]", got, "\x1b[2:2m")
	}

	// Without the fix this reads as the single parameter "12", moving the
	// cursor to row 12 (clamped to the last row) — a positioning sequence
	// silently misapplied while Unhandled() stayed empty.
	tt2 := New(20, 10)
	tt2.Write([]byte("\x1b[1:2H"))

	if row, col := tt2.Cursor(); row != 1 || col != 1 {
		t.Errorf("Cursor() = (%d,%d), want (1,1) — ESC[1:2H must not move the cursor", row, col)
	}
	if got := tt2.Unhandled(); len(got) != 1 || got[0] != "\x1b[1:2H" {
		t.Errorf("Unhandled() = %q, want [%q]", got, "\x1b[1:2H")
	}

	// The '?' private-mode prefix must still parse correctly.
	tt3 := New(20, 10)
	tt3.Write([]byte("\x1b[?25l"))
	if tt3.CursorVisible() {
		t.Error("cursor should be hidden after ESC[?25l")
	}
	if got := tt3.Unhandled(); len(got) != 0 {
		t.Errorf("Unhandled() = %q, want empty", got)
	}
}

// Erasing must carry the pen's Bold along with Fg/Bg: a real terminal fills
// erased cells with the full current SGR state, and Cell models Bold
// explicitly, so dropping it would leave an erased cell with an attribute
// that never existed.
func TestEraseCarriesPenBold(t *testing.T) {
	tt := New(10, 3)
	tt.Write([]byte("\x1b[1mabc\x1b[1;1H\x1b[K"))

	if got := tt.Cell(1, 1); !got.Bold {
		t.Errorf("Cell(1,1) after erase = %+v, want Bold=true (carried from pen)", got)
	}

	tt2 := New(10, 3)
	tt2.Write([]byte("\x1b[1mabc\x1b[2J"))

	if got := tt2.Cell(1, 1); !got.Bold {
		t.Errorf("Cell(1,1) after ESC[2J = %+v, want Bold=true (carried from pen)", got)
	}
}

// ESC( and ESC) designate a character set and take a third byte naming it;
// that byte must be consumed with the escape, not printed as text.
func TestCharsetDesignationConsumesDesignatorByte(t *testing.T) {
	tt := New(20, 3)
	tt.Write([]byte("A\x1b(BC"))

	if got := tt.Row(1); got != "AC" {
		t.Errorf("Row(1) = %q, want %q — designator byte leaked into the grid", got, "AC")
	}
	if got := tt.Unhandled(); len(got) != 1 || got[0] != "\x1b(B" {
		t.Errorf("Unhandled() = %q, want [%q]", got, "\x1b(B")
	}
}
