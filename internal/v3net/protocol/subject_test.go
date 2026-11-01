package protocol

import "testing"

func TestCleanSubject(t *testing.T) {
	for in, want := range map[string]string{
		"Café <tonight> |15": "Café <tonight> |15",
		"a\x1b[2Jb":          "a [2Jb",
		"\x1b]0;t\x07x":      " ]0;t x",
		"a\u009b2J\r\n\x7f":  "a 2J   ",
	} {
		got := CleanSubject(in)
		if got != want {
			t.Errorf("CleanSubject(%q) = %q, want %q", in, got, want)
		}
		m := validMessage()
		m.Subject = got
		if err := m.Validate(); err != nil {
			t.Errorf("cleaned %q: %v", in, err)
		}
	}
}
