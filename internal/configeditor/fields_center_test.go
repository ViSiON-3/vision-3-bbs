package configeditor

import (
	"testing"
	"unicode/utf8"
)

// centerText lays out box headers and help bars whose content is dynamic —
// category titles, record names, hub-area counts. The box must hold its shape:
// an over-long value is truncated to the column budget rather than pushing the
// border out. This matches menueditor and usereditor.
func TestCenterTextHoldsTheBoxWidth(t *testing.T) {
	tests := []struct {
		name  string
		s     string
		width int
	}{
		{"shorter than width", "Title", 20},
		{"exactly width", "12345678901234567890", 20},
		{"longer than width", "A Very Long Category Title That Overflows", 20},
		{"multi-byte longer than width", "Zone Réseau — Configuration Générale Étendue", 20},
		{"cjk longer than width", "日本語のメッセージエリア設定画面", 20},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := centerText(tt.s, tt.width)
			if !utf8.ValidString(got) {
				t.Fatalf("centerText(%q, %d) produced invalid UTF-8: %q", tt.s, tt.width, got)
			}
			if n := utf8.RuneCountInString(got); n != tt.width {
				t.Errorf("centerText(%q, %d) = %d columns, want exactly %d (%q)", tt.s, tt.width, n, tt.width, got)
			}
		})
	}
}
