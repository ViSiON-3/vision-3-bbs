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
