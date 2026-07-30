package menu

import "testing"

// truncateRunes (oneliners) trims surrounding whitespace before measuring,
// then appends a 3-rune "..." ellipsis when it cuts. These cases pin its
// current behaviour before it is migrated to delegate to ansi.TruncateRunes.
func TestOnelinerTruncateRunes(t *testing.T) {
	tests := []struct {
		name string
		s    string
		max  int
		want string
	}{
		{"empty input", "", 10, ""},
		{"whitespace only collapses to empty", "   ", 10, ""},
		{"max 0", "hello", 0, ""},
		{"negative max", "hello", -1, ""},
		{"shorter than max", "abc", 10, "abc"},
		{"exactly max", "Hello", 5, "Hello"},
		{"longer gets ellipsis", "Hello World", 8, "Hello..."},
		{"maxLen exactly 3 hard-cuts", "Hello", 3, "Hel"},
		{"maxLen 1 hard-cut", "Hello", 1, "H"},
		{"leading and trailing whitespace trimmed first", "  Hello  ", 10, "Hello"},
		{"trimmed then truncated", "  Hello World  ", 8, "Hello..."},
		{"multi-byte fits by runes", "héllo", 5, "héllo"},
		{"multi-byte truncation on rune boundary", "héllo wörld", 8, "héllo..."},
		{"cjk truncation on rune boundary", "日本語のメッセージ", 5, "日本..."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := truncateRunes(tt.s, tt.max); got != tt.want {
				t.Errorf("truncateRunes(%q, %d) = %q, want %q", tt.s, tt.max, got, tt.want)
			}
		})
	}
}
