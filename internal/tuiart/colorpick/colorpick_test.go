package colorpick

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// Left and Right wrap along each row, Up/Down/Tab switch rows, and a
// foreground-only picker never leaves the foreground row.
func TestPickerKeys(t *testing.T) {
	p := New(15, false)
	key := func(k tea.KeyType) bool { return p.Key(tea.KeyMsg{Type: k}) }
	key(tea.KeyRight)
	if p.FG != 0 {
		t.Errorf("fg wrapped to %d, want 0", p.FG)
	}
	key(tea.KeyLeft)
	if p.FG != 15 {
		t.Errorf("fg wrapped back to %d, want 15", p.FG)
	}
	key(tea.KeyDown)
	if p.Row != RowBG {
		t.Fatal("down did not reach the background row")
	}
	key(tea.KeyLeft)
	if p.BG != 7 {
		t.Errorf("bg wrapped to %d, want 7", p.BG)
	}
	key(tea.KeyRight)
	if p.BG != NoBG {
		t.Errorf("bg wrapped to %d, want none", p.BG)
	}
	if key(tea.KeyEnter) {
		t.Error("Enter reported as a picker key")
	}

	fgOnly := New(8, true)
	fgOnly.Key(tea.KeyMsg{Type: tea.KeyDown})
	if fgOnly.Row != RowFG {
		t.Error("a foreground-only picker moved to the background row")
	}
}

// An attribute round-trips through the picker, the blink bit kept, and a
// required background never offers "none".
func TestPickerAttr(t *testing.T) {
	for _, attr := range []int{31, 112, 15, 0x9E} {
		p := FromAttr(attr)
		if got := p.Attr(attr); got != attr {
			t.Errorf("attr %d round-tripped to %d", attr, got)
		}
	}
	p := FromAttr(0x1F) // white on blue
	p.Row = RowBG
	for range 8 {
		p.Key(tea.KeyMsg{Type: tea.KeyLeft})
		if p.BG == NoBG {
			t.Fatal("a required background offered none")
		}
	}
	if row := p.BGRow(Styles{}); strings.Contains(row, "--") {
		t.Error("required background row shows the none swatch")
	}
}
