package usereditor

import (
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/user"
	tea "github.com/charmbracelet/bubbletea"
)

// editing returns a model sitting on the edit screen for a seeded user.
func editing(t *testing.T) Model {
	t.Helper()
	m, _ := editorOver(t, &user.User{ID: 1, Handle: "Alice", AccessLevel: 10})
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	if m.mode != modeEdit {
		t.Fatalf("expected modeEdit, got %v", m.mode)
	}
	return m
}

// at returns the index of the editable field at the given column and row.
func (m Model) at(t *testing.T, col, row int) int {
	t.Helper()
	for i, f := range m.fields {
		if f.Col == col && f.Row == row {
			return i
		}
	}
	t.Fatalf("no field at col %d row %d", col, row)
	return -1
}

func (m Model) label() string { return m.fields[m.editField].Label }

// The defect #242 reports: reaching the right column meant pressing Down
// through the whole left column. One press of Right now does it.
func TestRightMovesToTheAdjacentColumn(t *testing.T) {
	m := editing(t)
	m.editField = m.at(t, leftCol, 4) // Handle
	m.editField = m.horizontalField(rightCol)

	if got := m.label(); got != "Validated" {
		t.Errorf("Right from Handle selected %q, want Validated", got)
	}
	if f := m.fields[m.editField]; f.Col != rightCol || f.Row != 4 {
		t.Errorf("landed at col %d row %d, want col %d row 4", f.Col, f.Row, rightCol)
	}
}

// Right from a left-column row with no right-column counterpart lands on the
// nearest editable one rather than failing or jumping to the top.
func TestRightFromBelowTheRightColumnClampsToNearest(t *testing.T) {
	m := editing(t)
	m.editField = m.at(t, leftCol, 15) // WFC Keys, below the last editable right field
	m.editField = m.horizontalField(rightCol)

	f := m.fields[m.editField]
	if f.Col != rightCol {
		t.Fatalf("did not move to the right column, got col %d", f.Col)
	}
	if f.Type == ftDisplay {
		t.Errorf("landed on read-only field %q", f.Label)
	}
	if f.Row != 11 { // Output Mode: the last editable right-column row
		t.Errorf("landed on row %d, want the nearest editable row 11", f.Row)
	}
}

// Left and Right at the outer edges hold position. Arrow keys read as spatial
// movement, so an edge press must not teleport across the screen.
func TestHorizontalAtTheEdgesIsANoOp(t *testing.T) {
	m := editing(t)

	m.editField = m.at(t, leftCol, 4)
	if got := m.horizontalField(leftCol); got != m.editField {
		t.Errorf("Left in the left column moved to %q", m.fields[got].Label)
	}

	m.editField = m.at(t, rightCol, 4)
	if got := m.horizontalField(rightCol); got != m.editField {
		t.Errorf("Right in the right column moved to %q", m.fields[got].Label)
	}
}

// Up and Down stay inside the current column instead of walking the slice.
func TestVerticalStaysInItsColumn(t *testing.T) {
	for _, col := range []int{leftCol, rightCol} {
		m := editing(t)
		m.editField = m.at(t, col, 4)
		for i := 0; i < 40; i++ {
			m.editField = m.verticalField(1)
			f := m.fields[m.editField]
			if f.Col != col {
				t.Fatalf("Down from column %d reached column %d (%q) after %d presses",
					col, f.Col, f.Label, i+1)
			}
			if f.Type == ftDisplay {
				t.Fatalf("Down selected read-only field %q", f.Label)
			}
		}
	}
}

// Down off the bottom of a column wraps to its top, matching what the linear
// walk did before, only scoped to the column.
func TestVerticalWrapsWithinTheColumn(t *testing.T) {
	m := editing(t)
	m.editField = m.at(t, rightCol, 11) // Output Mode, last editable right field
	m.editField = m.verticalField(1)

	if f := m.fields[m.editField]; f.Col != rightCol || f.Row != 4 {
		t.Errorf("Down wrapped to col %d row %d, want col %d row 4", f.Col, f.Row, rightCol)
	}

	m.editField = m.at(t, rightCol, 4)
	m.editField = m.verticalField(-1)
	if f := m.fields[m.editField]; f.Col != rightCol || f.Row != 11 {
		t.Errorf("Up wrapped to col %d row %d, want col %d row 11", f.Col, f.Row, rightCol)
	}
}

// The arrow keys are additive: Tab and Enter keep advancing in field order.
func TestArrowKeysReachEveryEditableField(t *testing.T) {
	m := editing(t)
	seen := map[int]bool{}
	for _, col := range []int{leftCol, rightCol} {
		m.editField = m.at(t, col, 4)
		for i := 0; i < 40; i++ {
			seen[m.editField] = true
			m.editField = m.verticalField(1)
		}
	}
	for i, f := range m.fields {
		if f.Type != ftDisplay && !seen[i] {
			t.Errorf("field %q (col %d row %d) is unreachable by arrow keys", f.Label, f.Col, f.Row)
		}
	}
}

// An arrow key must mean the same thing whether or not a field is open for
// editing. Down used to confirm and walk the field slice linearly from inside
// the input, while outside it moved within the column — so pressing Down twice
// landed you in different places depending on a mode you could not see.
func TestArrowsMeanTheSameThingWhileEditing(t *testing.T) {
	for _, tc := range []struct {
		name string
		key  tea.KeyType
		dir  int
	}{{"down", tea.KeyDown, 1}, {"up", tea.KeyUp, -1}} {
		t.Run(tc.name, func(t *testing.T) {
			m := editing(t)
			m.editField = m.at(t, leftCol, 6) // Access Level, mid-column
			want := m.verticalField(tc.dir)

			// Open the field, then press the arrow to confirm and move.
			updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
			m = updated.(Model)
			if m.mode != modeEditField {
				t.Fatalf("Enter did not open the field, mode = %v", m.mode)
			}
			updated, _ = m.Update(tea.KeyMsg{Type: tc.key})
			m = updated.(Model)

			if m.mode != modeEdit {
				t.Fatalf("%s left mode %v, want modeEdit", tc.name, m.mode)
			}
			if m.editField != want {
				t.Errorf("%s from inside the input selected %q, but from outside it selects %q",
					tc.name, m.fields[m.editField].Label, m.fields[want].Label)
			}
			if f := m.fields[m.editField]; f.Col != leftCol {
				t.Errorf("%s escaped its column, landed on %q in column %d", tc.name, f.Label, f.Col)
			}
		})
	}
}

// Tab and Enter keep advancing in field order, which is the older muscle
// memory and is deliberately not column-scoped.
func TestTabAdvancesInFieldOrderFromInsideTheInput(t *testing.T) {
	m := editing(t)
	m.editField = m.at(t, leftCol, 6)
	want := m.nextEditableField(1)

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = updated.(Model)

	if m.editField != want {
		t.Errorf("Tab selected %q, want %q", m.fields[m.editField].Label, m.fields[want].Label)
	}
}
