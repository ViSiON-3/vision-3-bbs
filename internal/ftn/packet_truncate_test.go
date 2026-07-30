package ftn

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// FTS-0001 packed-message fields are null-terminated byte strings with byte
// limits (36 for To/From, 72 for Subject), so truncation must respect the byte
// budget — but it must not cut a multi-byte character in half, which would put
// an invalid UTF-8 sequence on the wire for every other system to parse.
func TestTruncateFieldCutsOnRuneBoundaryWithinByteBudget(t *testing.T) {
	tests := []struct {
		name string
		s    string
		max  int
	}{
		{"ascii under budget", "Hello", 36},
		{"ascii over budget", "abcdefghijklmnopqrstuvwxyz0123456789extra", 36},
		{"accented over budget", "Réponse à propos des accents dans les sujets FTN", 36},
		{"cjk over budget", "日本語のメッセージの件名がとても長い場合", 36},
		// Constructed so the byte budget lands INSIDE a multi-byte rune: 35
		// ASCII bytes then a 2-byte "é" occupying bytes 35-36, so a byte cut at
		// 36 keeps only the lead byte.
		{"budget splits a 2-byte rune", strings.Repeat("a", 35) + "é" + "tail", 36},
		// Same for a 3-byte rune straddling the limit.
		{"budget splits a 3-byte rune", strings.Repeat("a", 34) + "日" + "tail", 36},
		{"subject budget", "Re: " + "société normande " + "encore plus long que soixante-douze octets", 72},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncateField(tt.s, tt.max)

			if len(got) > tt.max {
				t.Errorf("len = %d bytes, want <= %d (FTS-0001 field limit)", len(got), tt.max)
			}
			if !utf8.ValidString(got) {
				t.Errorf("truncateField produced invalid UTF-8: %q", got)
			}
			// Must keep as much as the byte budget allows: adding the next rune
			// back would have to overflow.
			if len(got) < len(tt.s) {
				next, size := utf8.DecodeRuneInString(tt.s[len(got):])
				if next != utf8.RuneError && len(got)+size <= tt.max {
					t.Errorf("truncated to %d bytes but another %d-byte rune would fit in %d", len(got), size, tt.max)
				}
			}
		})
	}
}
