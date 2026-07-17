package menu

import (
	"strings"

	"github.com/ViSiON-3/vision-3-bbs/internal/editor"
)

// parseChallengeKey maps a config key string to the key code the challenge
// loop compares against. "ESC" (any case) or empty -> editor.KeyEsc; otherwise
// the first rune of the string.
func parseChallengeKey(s string) int {
	if s == "" || strings.EqualFold(s, "ESC") {
		return editor.KeyEsc
	}
	for _, r := range s { // first rune
		return int(r)
	}
	return editor.KeyEsc
}
