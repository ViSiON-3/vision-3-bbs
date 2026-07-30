package menu

import "testing"

// adminTruncate trims surrounding whitespace before measuring, then appends an
// ASCII "..." ellipsis when it cuts (hard-cutting instead when max is 3 or
// less). These cases pin that behaviour. The expectations changed once, when
// the ellipsis moved from U+2026 to ASCII because CP437 cannot render U+2026;
// they should not change again without an equally deliberate reason.
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
		{"longer gets ellipsis", "Hello World", 8, "Hello..."},
		// The ellipsis is 3 runes, so anything up to and including 3 has no room
		// for it and hard-cuts instead. This boundary moved when the ellipsis
		// changed from the 1-rune U+2026 to ASCII "...".
		{"maxLen exactly 1 hard-cuts", "Hello", 1, "H"},
		{"maxLen 2 hard-cuts", "Hello", 2, "He"},
		{"maxLen 3 hard-cuts", "Hello", 3, "Hel"},
		{"maxLen 4 has room for the ellipsis", "Hello", 4, "H..."},
		{"maxLen 0 with non-empty trimmed value", "Hello", 0, ""},
		{"leading and trailing whitespace trimmed first", "  Hello  ", 10, "Hello"},
		{"trimmed then truncated", "  Hello World  ", 8, "Hello..."},
		{"multi-byte fits by runes", "héllo", 5, "héllo"},
		{"multi-byte truncation on rune boundary", "héllo wörld", 8, "héllo..."},
		{"cjk truncation on rune boundary", "日本語のメッセージ", 5, "日本..."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := adminTruncate(tt.s, tt.max); got != tt.want {
				t.Errorf("adminTruncate(%q, %d) = %q, want %q", tt.s, tt.max, got, tt.want)
			}
		})
	}
}
