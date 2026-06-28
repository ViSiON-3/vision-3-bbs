package configeditor

import "testing"

func TestPadToCol(t *testing.T) {
	if got := padToCol("abc", 5); got != "abc  " {
		t.Errorf("padToCol pad = %q, want %q", got, "abc  ")
	}
	if got := padToCol("abcdef", 3); got != "abc" {
		t.Errorf("padToCol truncate = %q, want abc", got)
	}
}
