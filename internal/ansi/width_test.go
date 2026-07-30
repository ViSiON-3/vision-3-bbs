package ansi

import (
	"testing"
	"unicode/utf8"
)

func TestTruncateRunes(t *testing.T) {
	tests := []struct {
		name     string
		s        string
		maxRunes int
		ellipsis string
		want     string
	}{
		{"empty input", "", 10, "...", ""},
		{"max 0", "hello", 0, "...", ""},
		{"negative max", "hello", -1, "...", ""},
		{"shorter than max", "abc", 10, "...", "abc"},
		{"exactly max", "Hello", 5, "...", "Hello"},
		{"overflow with 3-rune ellipsis", "Hello World", 8, "...", "Hello..."},
		{"overflow with 2-rune ellipsis", "abcdefgh", 5, "..", "abc.."},
		{"overflow with 1-rune ellipsis", "Hello World", 8, "…", "Hello W…"},
		{"overflow with empty ellipsis hard-cuts", "Hello World", 5, "", "Hello"},
		{"maxRunes equal to ellipsis length hard-cuts", "Hello", 3, "...", "Hel"},
		{"maxRunes below ellipsis length hard-cuts", "Hello", 1, "...", "H"},
		{"maxRunes 0 with empty ellipsis", "Hello", 0, "", ""},
		{"multi-rune ellipsis longer than 3", "Hello World", 10, ">>>>>", "Hello>>>>>"},
		{"multi-byte fits by runes", "héllo", 5, "...", "héllo"},
		{"multi-byte truncation on rune boundary", "héllo wörld", 8, "...", "héllo..."},
		{"cjk truncation on rune boundary", "日本語のメッセージ", 5, "...", "日本..."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := TruncateRunes(tt.s, tt.maxRunes, tt.ellipsis)
			if got != tt.want {
				t.Errorf("TruncateRunes(%q, %d, %q) = %q, want %q", tt.s, tt.maxRunes, tt.ellipsis, got, tt.want)
			}
			if !utf8.ValidString(got) {
				t.Errorf("TruncateRunes(%q, %d, %q) produced invalid UTF-8: %q", tt.s, tt.maxRunes, tt.ellipsis, got)
			}
		})
	}
}

func TestPadRight(t *testing.T) {
	tests := []struct {
		name  string
		s     string
		width int
		want  string
	}{
		{"pads short string", "ab", 5, "ab   "},
		{"exact width no-op", "abcde", 5, "abcde"},
		{"longer than width never truncates", "abcdefgh", 5, "abcdefgh"},
		{"multi-byte counted in runes", "café", 6, "café  "},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := PadRight(tt.s, tt.width); got != tt.want {
				t.Errorf("PadRight(%q, %d) = %q, want %q", tt.s, tt.width, got, tt.want)
			}
		})
	}
}

func TestPadLeft(t *testing.T) {
	tests := []struct {
		name  string
		s     string
		width int
		want  string
	}{
		{"pads short string", "ab", 5, "   ab"},
		{"exact width no-op", "abcde", 5, "abcde"},
		{"longer than width never truncates", "abcdefgh", 5, "abcdefgh"},
		{"multi-byte counted in runes", "café", 6, "  café"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := PadLeft(tt.s, tt.width); got != tt.want {
				t.Errorf("PadLeft(%q, %d) = %q, want %q", tt.s, tt.width, got, tt.want)
			}
		})
	}
}

func TestCenter(t *testing.T) {
	tests := []struct {
		name  string
		s     string
		width int
		want  string
	}{
		{"even padding", "hi", 6, "  hi  "},
		{"odd remainder goes right", "hi", 5, " hi  "},
		{"exact width no-op", "Hello", 5, "Hello"},
		{"longer than width never truncates", "toolong", 3, "toolong"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Center(tt.s, tt.width); got != tt.want {
				t.Errorf("Center(%q, %d) = %q, want %q", tt.s, tt.width, got, tt.want)
			}
		})
	}
}
