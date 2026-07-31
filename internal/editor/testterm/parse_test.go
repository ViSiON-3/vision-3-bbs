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
