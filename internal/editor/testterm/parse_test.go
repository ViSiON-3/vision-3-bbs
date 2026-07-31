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
