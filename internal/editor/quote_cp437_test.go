package editor

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// cp437Word is raw CP437 text, as a quoted message body or author can carry
// it: block █ (0xDB) and box-drawing ─ (0xC4). None of it is valid UTF-8.
var cp437Word = strings.Repeat("\xdb\xc4", 50)

// #859: the quote picker's character handling must keep raw CP437 bytes as
// they are. Converting them to []rune turns each into U+FFFD.
func TestQuoteHelpersKeepRawCP437Bytes(t *testing.T) {
	if got := truncRunes(cp437Word, 7); got != cp437Word[:7] {
		t.Errorf("truncRunes = %q, want %q", got, cp437Word[:7])
	}
	if got := padRunes("\xc4\xc4", 4); got != "\xc4\xc4  " {
		t.Errorf("padRunes = %q, want %q", got, "\xc4\xc4  ")
	}
	wrapped := wrapQuoted("> ", cp437Word, MaxLineLength)
	var body strings.Builder
	for _, line := range wrapped {
		if strings.Contains(line, "�") {
			t.Errorf("wrapped line %q holds U+FFFD", line)
		}
		body.WriteString(strings.TrimPrefix(line, "> "))
	}
	if body.String() != cp437Word {
		t.Errorf("wrapped text = %q, want the source bytes back", body.String())
	}
	if got := quoteInitials("\x8eric Smith"); got != "\x8eS" {
		t.Errorf("quoteInitials = %q, want %q", got, "\x8eS")
	}
	if got := quoteInitials("\x8e\x82"); got != "\x8e\x82" {
		t.Errorf("quoteInitials of one-word name = %q, want %q", got, "\x8e\x82")
	}
	// Valid UTF-8 still counts and cuts by character.
	if got := truncRunes("héllo", 2); got != "hé" || !utf8.ValidString(got) {
		t.Errorf("truncRunes(UTF-8) = %q, want %q", got, "hé")
	}
}
