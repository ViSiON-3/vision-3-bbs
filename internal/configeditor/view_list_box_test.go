package configeditor

import (
	"fmt"
	"strings"
	"testing"

	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
)

// stripStyles removes ANSI escape sequences so tests can assert on layout.
func stripStyles(s string) string {
	var b strings.Builder
	inEsc := false
	for _, r := range s {
		switch {
		case inEsc:
			if r == 'm' {
				inEsc = false
			}
		case r == '\x1b':
			inEsc = true
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func TestListBox_BasicStructure(t *testing.T) {
	m := Model{width: 40, height: 12}
	boxW := 20
	// header(1)+border(1)+title(1)+sep(1)+list(3)+border(1)+help(1) = 9 fixed rows
	lb := m.newListBox(boxW, 9)

	lb.topBorder()
	lb.title("Title")
	lb.separator()
	items := []string{"alpha", "bravo", "charlie", "delta"}
	lb.list(3, 1, 2, len(items), func(i int) string { return items[i] })
	lb.bottomBorder()
	lb.bgRows(lb.bottomPad)
	out := stripStyles(lb.finish("HELP"))

	lines := strings.Split(out, "\n")
	if len(lines) != 12 {
		t.Fatalf("expected 12 screen lines, got %d", len(lines))
	}
	// extraV = 12-9 = 3 → topPad 1, bottomPad 2.
	if lb.bottomPad != 2 {
		t.Errorf("bottomPad = %d, want 2", lb.bottomPad)
	}
	// Top border after header + 1 top-pad line.
	wantBorder := "┌" + strings.Repeat("─", boxW) + "┐"
	if got := strings.TrimLeft(lines[2], "░"); !strings.HasPrefix(got, wantBorder) {
		t.Errorf("line 2 = %q, want prefix %q", got, wantBorder)
	}
	// Title centered inside │ │.
	if !strings.Contains(lines[3], "│") || !strings.Contains(lines[3], "Title") {
		t.Errorf("title row = %q", lines[3])
	}
	// List window shows items 1..3 (scroll=1), each padded to boxW.
	for i, want := range []string{"bravo", "charlie", "delta"} {
		row := lines[5+i]
		if !strings.Contains(row, want) {
			t.Errorf("list row %d = %q, want it to contain %q", i, row, want)
		}
		inner := strings.TrimRight(strings.Trim(strings.Trim(row, "░"), "│"), " ")
		if inner != want {
			t.Errorf("list row %d inner = %q, want %q", i, inner, want)
		}
	}
	// Help bar is the last line, centered over the full width.
	last := lines[len(lines)-1]
	if strings.TrimSpace(last) != "HELP" || len([]rune(last)) != 40 {
		t.Errorf("help bar = %q", last)
	}
}

func TestListBox_ListPadsAndTruncates(t *testing.T) {
	m := Model{width: 30, height: 10}
	lb := m.newListBox(10, 10)
	long := strings.Repeat("x", 25)
	lb.list(2, 0, 0, 2, func(i int) string {
		return []string{long, "ok"}[i]
	})
	lines := strings.Split(stripStyles(lb.b.String()), "\n")
	rowFor := func(sub string) string {
		for _, l := range lines {
			if strings.Contains(l, sub) {
				return strings.Trim(strings.Trim(l, "░"), "│")
			}
		}
		t.Fatalf("no row containing %q", sub)
		return ""
	}
	if got := rowFor("xxx"); got != strings.Repeat("x", 10) {
		t.Errorf("long row = %q, want truncated to 10", got)
	}
	if got := rowFor("ok"); got != "ok"+strings.Repeat(" ", 8) {
		t.Errorf("short row = %q, want padded to 10", got)
	}
}

func TestListBox_ListUnicodeRowsStayValid(t *testing.T) {
	m := Model{width: 30, height: 10}
	lb := m.newListBox(10, 10)
	// 25 runes of multi-byte content: byte-based truncation would split a
	// UTF-8 sequence and misalign the box.
	long := strings.Repeat("é", 25)
	lb.list(2, 0, 0, 2, func(i int) string {
		return []string{long, "ünïcødé"}[i]
	})
	out := stripStyles(lb.b.String())
	if !utf8.ValidString(out) {
		t.Fatal("list output contains invalid UTF-8 (rune split by truncation)")
	}
	lines := strings.Split(out, "\n")
	rowFor := func(sub string) string {
		for _, l := range lines {
			if strings.Contains(l, sub) {
				return strings.Trim(strings.Trim(l, "░"), "│")
			}
		}
		t.Fatalf("no row containing %q", sub)
		return ""
	}
	if got := rowFor("éé"); got != strings.Repeat("é", 10) {
		t.Errorf("long unicode row = %q, want 10 runes of é", got)
	}
	if got := rowFor("ünïcødé"); got != "ünïcødé"+strings.Repeat(" ", 3) {
		t.Errorf("short unicode row = %q, want padded to 10 runes", got)
	}
}

func TestListBox_ErrorRowTruncates(t *testing.T) {
	m := Model{width: 30, height: 10}
	lb := m.newListBox(10, 10)
	got := stripStyles(lb.errorRow("this error is far too long"))
	if got != " this e..." {
		t.Errorf("errorRow = %q, want %q", got, " this e...")
	}
	if short := stripStyles(lb.errorRow("bad")); short != " bad"+strings.Repeat(" ", 6) {
		t.Errorf("short errorRow = %q", short)
	}
}

func TestListNavKey(t *testing.T) {
	tests := []struct {
		key         tea.KeyType
		cursor      int
		total       int
		wantCursor  int
		wantHandled bool
	}{
		{tea.KeyUp, 2, 5, 1, true},
		{tea.KeyUp, 0, 5, 0, true},
		{tea.KeyDown, 2, 5, 3, true},
		{tea.KeyDown, 4, 5, 4, true},
		{tea.KeyDown, 0, 0, 0, true},
		{tea.KeyHome, 3, 5, 0, true},
		{tea.KeyEnd, 0, 5, 4, true},
		{tea.KeyEnd, 0, 0, 0, true},
		{tea.KeyEnter, 2, 5, 2, false},
		{tea.KeySpace, 2, 5, 2, false},
	}
	for _, tt := range tests {
		got, handled := listNavKey(tea.KeyMsg{Type: tt.key}, tt.cursor, tt.total)
		if got != tt.wantCursor || handled != tt.wantHandled {
			t.Errorf("listNavKey(%v, cursor=%d, total=%d) = (%d, %v), want (%d, %v)",
				tt.key, tt.cursor, tt.total, got, handled, tt.wantCursor, tt.wantHandled)
		}
	}
}

func TestClampListScroll(t *testing.T) {
	tests := []struct {
		cursor, scroll, visible, want int
	}{
		{0, 0, 10, 0},  // in window
		{5, 0, 10, 0},  // in window
		{10, 0, 10, 1}, // just past bottom
		{15, 0, 10, 6}, // far past bottom
		{2, 5, 10, 2},  // above window
		{0, -3, 10, 0}, // negative scroll clamped
	}
	for _, tt := range tests {
		if got := clampListScroll(tt.cursor, tt.scroll, tt.visible); got != tt.want {
			t.Errorf("clampListScroll(%d, %d, %d) = %d, want %d",
				tt.cursor, tt.scroll, tt.visible, got, tt.want)
		}
	}
}

func TestListBox_StatusScreenLineCount(t *testing.T) {
	m := Model{width: 40, height: 24}
	lb := m.newListBox(20, 15)
	lb.topBorder()
	lb.title("T")
	out := stripStyles(lb.statusScreen(fmt.Sprintf("%-20s", "Loading..."), 6, 2, "ESC"))
	lines := strings.Split(out, "\n")
	// header(1)+topPad+border(1)+title(1)+status(1)+blank(7)+border(1)
	// +bottomPad+2+help(1); extraV=24-15=9 → topPad 4, bottomPad 5.
	if len(lines) != 24 {
		t.Errorf("status screen has %d lines, want 24", len(lines))
	}
	if !strings.Contains(out, "Loading...") {
		t.Error("status text missing")
	}
	if strings.TrimSpace(lines[len(lines)-1]) != "ESC" {
		t.Errorf("help bar = %q", lines[len(lines)-1])
	}
}
