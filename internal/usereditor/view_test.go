package usereditor

import "testing"

// centerText (view.go) is currently byte-based (len(s) >= width, s[:width]).
// Every caller today passes ASCII literals, so byte and rune counting agree —
// these cases pin that observable behaviour before the helper is migrated to
// delegate to ansi.Center/ansi.TruncateRunes as a genuine (if today
// unreachable) correctness fix.
func TestViewCenterText(t *testing.T) {
	tests := []struct {
		name  string
		s     string
		width int
		want  string
	}{
		{"empty input", "", 10, "          "},
		{"shorter than width, even padding", "hi", 6, "  hi  "},
		{"shorter than width, odd remainder goes right", "hi", 5, " hi  "},
		{"exactly width", "Hello", 5, "Hello"},
		{"longer than width hard-cuts", "toolong", 3, "too"},
		{"width 0 with empty input", "", 0, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := centerText(tt.s, tt.width); got != tt.want {
				t.Errorf("centerText(%q, %d) = %q, want %q", tt.s, tt.width, got, tt.want)
			}
		})
	}
}
