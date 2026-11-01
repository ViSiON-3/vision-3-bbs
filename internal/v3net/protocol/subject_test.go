package protocol

import "testing"

// A hub refuses a new post with a control character in the fields that are shown
// to callers, as it does for the subject.
func TestValidateRefusesOriginFieldControls(t *testing.T) {
	set := map[string]func(*Message, string){
		"origin_board": func(m *Message, s string) { m.OriginBoard = s },
		"tearline":     func(m *Message, s string) { m.Tearline = s },
		"origin":       func(m *Message, s string) { m.Origin = s },
	}
	for name, setField := range set {
		for _, bad := range []string{"a\x1b[2Jb", "a\u009bb", "a\nb", "a\x7fb"} {
			m := validMessage()
			setField(&m, bad)
			if err := m.Validate(); err == nil {
				t.Errorf("%s %q: accepted", name, bad)
			}
		}
		m := validMessage()
		setField(&m, "Caf\xc3\xa9 \x9b BBS")
		if err := m.Validate(); err != nil {
			t.Errorf("%s: plain text refused: %v", name, err)
		}
	}
}

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
