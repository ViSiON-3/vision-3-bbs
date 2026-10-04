package keys

import (
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	cases := map[string]string{
		"hello{enter}":    "hello\r",
		"{esc}{up}{down}": "\x1b\x1b[A\x1b[B",
		"{left}{right}":   "\x1b[D\x1b[C",
		"a{bs}{tab}":      "a\x08\t",
		"{{literal}":      "{literal}",
		"Y":               "Y",
		"{ctrl-z}":        "\x1a",
		"{ctrl-A}x":       "\x01x",
	}
	for _, enc := range []string{"utf8", "cp437"} {
		for in, want := range cases {
			got, err := Parse(in, enc)
			if err != nil || string(got) != want {
				t.Errorf("%s: %q -> %q, %v; want %q", enc, in, got, err, want)
			}
		}
		for _, bad := range []string{"{nope}", "{enter", "x{", "{ctrl-}", "{ctrl-1}", "{ctrl-ab}"} {
			if _, err := Parse(bad, enc); err == nil {
				t.Errorf("%s: %q accepted", enc, bad)
			}
		}
	}
}

func TestParseCP437(t *testing.T) {
	cases := map[string]string{
		"it’s ‘fine’{enter}":   "it's 'fine'\r",
		"“quoted” — and – so…": "\"quoted\" - and - so...",
		"a b":                  "a b",
		"café über{esc}":       "caf\x82 \x81ber\x1b",
		"░▒▓":                  "\xb0\xb1\xb2",
	}
	for in, want := range cases {
		got, err := Parse(in, "cp437")
		if err != nil || string(got) != want {
			t.Errorf("%q -> %q, %v; want %q", in, got, err, want)
		}
	}
	if _, err := Parse("price €5", "cp437"); err == nil || !strings.Contains(err.Error(), "€") || !strings.Contains(err.Error(), "U+20AC") {
		t.Errorf("unencodable rune: %v", err)
	}
	if got, err := Parse("it’s café €5", "utf8"); err != nil || string(got) != "it’s café €5" {
		t.Errorf("utf8 board: %q %v", got, err)
	}
}

func TestStrokes(t *testing.T) {
	got, err := Strokes("hé…{up}{{{enter}", "cp437")
	if err != nil {
		t.Fatal(err)
	}
	want := []Stroke{{[]byte("h"), ""}, {[]byte{0x82}, ""}, {[]byte("."), ""}, {[]byte("."), ""}, {[]byte("."), ""},
		{[]byte("\x1b[A"), "up"}, {[]byte("{"), ""}, {[]byte("\r"), "enter"}}
	if len(got) != len(want) {
		t.Fatalf("got %q", got)
	}
	for i := range want {
		if string(got[i].Bytes) != string(want[i].Bytes) || got[i].Key != want[i].Key {
			t.Errorf("stroke %d: %q, want %q", i, got[i], want[i])
		}
	}
	u, err := Strokes("é{ctrl-z}", "utf8")
	if err != nil || len(u) != 2 || string(u[0].Bytes) != "é" || u[1].Key != "ctrl-z" || string(u[1].Bytes) != "\x1a" {
		t.Fatalf("utf8 strokes %q %v", u, err)
	}
}
