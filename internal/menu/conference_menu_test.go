package menu

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// padRight and truncateStr lay out the area, conference, file and V3Net
// pickers. Both must work in characters: their inputs include area and
// conference names from UTF-8 JSON and network names fetched from the remote
// V3Net registry.

func TestPadRightPadsToVisibleColumns(t *testing.T) {
	tests := []struct {
		name  string
		s     string
		width int
		want  int // expected visible columns
	}{
		{"ascii shorter", "abc", 10, 10},
		{"ascii exact", "abcdefghij", 10, 10},
		{"ascii longer left alone", "abcdefghijkl", 10, 12},
		{"multi-byte padded by runes", "café", 10, 10},
		{"cjk padded by runes", "日本語", 10, 10},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := utf8.RuneCountInString(padRight(tt.s, tt.width))
			if got != tt.want {
				t.Errorf("padRight(%q, %d) = %d columns, want %d", tt.s, tt.width, got, tt.want)
			}
		})
	}
}

func TestTruncateStrCutsOnRuneBoundaries(t *testing.T) {
	tests := []struct {
		name   string
		s      string
		maxLen int
		want   string
	}{
		{"shorter than max", "abc", 10, "abc"},
		{"exactly max", "abcde", 5, "abcde"},
		{"longer gets ellipsis", "abcdefgh", 5, "abc.."},
		{"maxLen 2 hard cut", "abcdef", 2, "ab"},
		{"multi-byte fits by runes", "café", 4, "café"},
		{"multi-byte cut on boundary", "café society", 6, "café.."},
		{"cjk cut on boundary", "日本語のメッセージ", 5, "日本語.."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncateStr(tt.s, tt.maxLen)
			if got != tt.want {
				t.Errorf("truncateStr(%q, %d) = %q, want %q", tt.s, tt.maxLen, got, tt.want)
			}
			if !utf8.ValidString(got) {
				t.Errorf("truncateStr(%q, %d) produced invalid UTF-8: %q", tt.s, tt.maxLen, got)
			}
		})
	}
}

// The two are used together to build fixed-width rows; a multi-byte name must
// not push the row wider than the column budget.
func TestPadRightAfterTruncateStrHoldsColumnBudget(t *testing.T) {
	for _, name := range []string{"ascii name", "café society", "日本語のメッセージ"} {
		row := padRight(truncateStr(name, 12), 12)
		if got := utf8.RuneCountInString(row); got != 12 {
			t.Errorf("row for %q = %d columns, want 12 (%q)", name, got, row)
		}
		if strings.Contains(row, "�") {
			t.Errorf("row for %q contains a replacement char: %q", name, row)
		}
	}
}
