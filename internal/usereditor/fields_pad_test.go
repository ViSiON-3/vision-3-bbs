package usereditor

import (
	"testing"
	"unicode/utf8"
)

// padRight and padLeft lay out the user editor's columns. The fields they
// render — handle, real name, group location, private note — are edited in this
// TUI without the BBS prompts' ASCII gate, so they can hold multi-byte text and
// must be measured and cut in characters.
func TestPadHelpersUseColumnsNotBytes(t *testing.T) {
	tests := []struct {
		name  string
		s     string
		width int
	}{
		{"ascii short", "abc", 10},
		{"ascii exact", "abcdefghij", 10},
		{"ascii long", "abcdefghijklmn", 10},
		{"accented short", "café", 10},
		{"accented long", "café société normande", 10},
		{"cjk long", "日本語のメッセージ", 10},
	}
	for _, tt := range tests {
		for _, fn := range []struct {
			name string
			f    func(string, int) string
		}{{"padRight", padRight}, {"padLeft", padLeft}} {
			t.Run(fn.name+"/"+tt.name, func(t *testing.T) {
				got := fn.f(tt.s, tt.width)
				if !utf8.ValidString(got) {
					t.Fatalf("%s(%q, %d) produced invalid UTF-8: %q", fn.name, tt.s, tt.width, got)
				}
				if n := utf8.RuneCountInString(got); n != tt.width {
					t.Errorf("%s(%q, %d) = %d columns, want %d (%q)", fn.name, tt.s, tt.width, n, tt.width, got)
				}
			})
		}
	}
}
