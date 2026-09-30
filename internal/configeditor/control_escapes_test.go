package configeditor

import "testing"

func TestControlEscapesRoundTrip(t *testing.T) {
	for _, tc := range []struct{ raw, shown string }{
		{"", ""},
		{"neo\rsecret\r", `neo\rsecret\r`},
		{"\r\n", `\r\n`},
		{"a\tb", `a\tb`},
		{`c:\doors`, `c:\\doors`},
		{"\x1b[2J", `\x1B[2J`},
		{"\x00\x7f", `\x00\x7F`},
		{"{USERHANDLE}\r", `{USERHANDLE}\r`},
	} {
		if got := showControlEscapes(tc.raw); got != tc.shown {
			t.Errorf("showControlEscapes(%q) = %q, want %q", tc.raw, got, tc.shown)
		}
		got, err := parseControlEscapes(tc.shown)
		if err != nil {
			t.Errorf("parseControlEscapes(%q): %v", tc.shown, err)
			continue
		}
		if got != tc.raw {
			t.Errorf("parseControlEscapes(%q) = %q, want %q", tc.shown, got, tc.raw)
		}
	}
}

func TestParseControlEscapesAcceptsLowercaseHex(t *testing.T) {
	got, err := parseControlEscapes(`\x1b`)
	if err != nil || got != "\x1b" {
		t.Errorf(`parseControlEscapes(\x1b) = %q, %v`, got, err)
	}
}

// A stray backslash is far more likely a mistyped escape than a character
// meant for the door server.
func TestParseControlEscapesRejectsWhatItCannotRead(t *testing.T) {
	for _, in := range []string{`\`, `abc\`, `\q`, `\x`, `\x1`, `\xZZ`} {
		if got, err := parseControlEscapes(in); err == nil {
			t.Errorf("parseControlEscapes(%q) = %q, want an error", in, got)
		}
	}
}
