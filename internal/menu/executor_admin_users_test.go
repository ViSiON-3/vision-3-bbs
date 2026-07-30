package menu

import "testing"

// adminTruncate trims surrounding whitespace before measuring, then appends a
// single-rune "…" ellipsis when it cuts. These cases pin its current
// behaviour before it is migrated to delegate to ansi.TruncateRunes.
func TestAdminTruncate(t *testing.T) {
	tests := []struct {
		name string
		s    string
		max  int
		want string
	}{
		{"empty input", "", 10, ""},
		{"whitespace only collapses to empty", "   ", 10, ""},
		{"max 0", "hello", 0, ""},
		{"shorter than max", "abc", 10, "abc"},
		{"exactly max", "Hello", 5, "Hello"},
		{"longer gets ellipsis", "Hello World", 8, "Hello W…"},
		{"maxLen exactly 1 hard-cuts", "Hello", 1, "H"},
		{"maxLen 0 with non-empty trimmed value", "Hello", 0, ""},
		{"leading and trailing whitespace trimmed first", "  Hello  ", 10, "Hello"},
		{"trimmed then truncated", "  Hello World  ", 8, "Hello W…"},
		{"multi-byte fits by runes", "héllo", 5, "héllo"},
		{"multi-byte truncation on rune boundary", "héllo wörld", 8, "héllo w…"},
		{"cjk truncation on rune boundary", "日本語のメッセージ", 5, "日本語の…"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := adminTruncate(tt.s, tt.max); got != tt.want {
				t.Errorf("adminTruncate(%q, %d) = %q, want %q", tt.s, tt.max, got, tt.want)
			}
		})
	}
}
