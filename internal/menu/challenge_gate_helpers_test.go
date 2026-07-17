package menu

import (
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/editor"
)

func TestParseChallengeKey(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"ESC", editor.KeyEsc},
		{"esc", editor.KeyEsc},
		{"", editor.KeyEsc},
		{"*", int('*')},
		{"A", int('A')},
		{"12", int('1')}, // first rune wins
	}
	for _, c := range cases {
		if got := parseChallengeKey(c.in); got != c.want {
			t.Errorf("parseChallengeKey(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestFindCountdownField(t *testing.T) {
	// Matches the real BOTCHECK.ASC layout: ESC[0m, then two text lines.
	prompt := []byte("\x1b[0m\r\n Press ESC twice if you're not a bot.\r\n You have ## seconds.\r\n")
	row, col, width, found := findCountdownField(prompt)
	if !found {
		t.Fatal("found = false, want true")
	}
	if row != 3 {
		t.Errorf("row = %d, want 3", row)
	}
	if col != 11 { // " You have " = 10 chars, '#' starts at column 11
		t.Errorf("col = %d, want 11", col)
	}
	if width != 2 {
		t.Errorf("width = %d, want 2", width)
	}
}

func TestFindCountdownFieldSkipsColorCodes(t *testing.T) {
	// A color code before the field must not shift the computed column.
	prompt := []byte(" You have \x1b[1;32m### seconds.")
	row, col, width, found := findCountdownField(prompt)
	if !found || row != 1 || col != 11 || width != 3 {
		t.Errorf("got row=%d col=%d width=%d found=%v; want 1/11/3/true", row, col, width, found)
	}
}

func TestFindCountdownFieldNone(t *testing.T) {
	if _, _, _, found := findCountdownField([]byte("no placeholder here")); found {
		t.Error("found = true, want false")
	}
}

func TestFormatCountdownValue(t *testing.T) {
	cases := []struct {
		secs, width int
		want        string
	}{
		{20, 2, "20"},
		{5, 2, " 5"}, // right-aligned within width
		{5, 3, "  5"},
		{100, 2, "100"}, // exceeds width -> printed as-is
	}
	for _, c := range cases {
		if got := formatCountdownValue(c.secs, c.width); got != c.want {
			t.Errorf("formatCountdownValue(%d,%d) = %q, want %q", c.secs, c.width, got, c.want)
		}
	}
}

func TestSubstituteCountdown(t *testing.T) {
	prompt := []byte(" You have ## seconds.")
	got := string(substituteCountdown(prompt, 9, 2))
	if got != " You have  9 seconds." {
		t.Errorf("substituteCountdown = %q", got)
	}
}
