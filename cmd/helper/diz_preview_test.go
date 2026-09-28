package main

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// The dry-run DIZ preview cuts the first line to 50 (files import) or 60
// (files reextractdiz) characters with "...", counting characters: "A" then
// "é"s has its 47th/57th byte inside an "é".
func TestDizPreviewCutsByRune(t *testing.T) {
	for _, max := range []int{50, 60} {
		desc := "A" + strings.Repeat("é", max+10) + "\nsecond line"
		got := dizPreview(desc, max)
		want := "A" + strings.Repeat("é", max-4) + "..."
		if !utf8.ValidString(got) {
			t.Errorf("dizPreview(_, %d) = %q is not valid UTF-8", max, got)
		}
		if n := utf8.RuneCountInString(got); n != max {
			t.Errorf("dizPreview(_, %d) has %d characters, want %d", max, n, max)
		}
		if got != want {
			t.Errorf("dizPreview(_, %d) = %q, want %q", max, got, want)
		}
	}
	if got := dizPreview("Short line\nmore", 50); got != "Short line" {
		t.Errorf("dizPreview short = %q, want first line only", got)
	}
}
