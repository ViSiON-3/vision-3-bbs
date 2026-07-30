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
		want  string
	}{
		{"shorter is centred", "Title", 20, "       Title        "},
		{"exactly width is untouched", "12345678901234567890", 20, "12345678901234567890"},
		{"longer is truncated", "A Very Long Category Title That Overflows", 20, "A Very Long Category"},
		{"multi-byte truncated on a rune boundary", "Zone Réseau — Configuration Générale Étendue", 20, "Zone Réseau — Config"},
		{"cjk shorter is centred", "日本語のメッセージエリア設定画面", 20, "  日本語のメッセージエリア設定画面  "},
		{"cjk longer is truncated", "日本語のメッセージエリア設定画面をとても長くしたもの", 20, "日本語のメッセージエリア設定画面をとても"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := centerText(tt.s, tt.width)
			if got != tt.want {
				t.Errorf("centerText(%q, %d) = %q, want %q", tt.s, tt.width, got, tt.want)
			}
			if !utf8.ValidString(got) {
				t.Errorf("centerText(%q, %d) produced invalid UTF-8: %q", tt.s, tt.width, got)
			}
			if n := utf8.RuneCountInString(got); n != tt.width {
				t.Errorf("centerText(%q, %d) = %d columns, want exactly %d", tt.s, tt.width, n, tt.width)
			}
		})
	}
}
