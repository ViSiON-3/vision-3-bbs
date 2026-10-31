package ansi

import (
	"strings"
	"testing"
)

func TestOneLine(t *testing.T) {
	got := OneLine("a\x1b[2J\r\n\x07\u009bb\x7f|CL é")
	if want := "a [2J    b |CL é"; got != want {
		t.Errorf("OneLine = %q, want %q", got, want)
	}
}

func TestEscapePipesPrintsAsWritten(t *testing.T) {
	for _, s := range []string{"|CL", "|12red", "||", "a | b", "|", "|PP|P|DE|CR"} {
		if got := string(ReplacePipeCodes([]byte(EscapePipes(s)))); got != s {
			t.Errorf("%q came out as %q", s, got)
		}
	}
}

func TestPlainName(t *testing.T) {
	got := PlainName("Evil|CL\x1b[2J|12")
	if want := "Evil/CL [2J/12"; got != want {
		t.Errorf("PlainName = %q, want %q", got, want)
	}
}

func TestDefangScreenCodes(t *testing.T) {
	cases := map[string]string{
		"|12Red|07":      "|12Red|07",
		"A | B":          "A | B",
		"A || B":         "A || B",
		"Evil|CLx":       "Evil/CLx",
		"|PP|P|DE|CR":    "/PP/P/DE/CR",
		"||CL":           "||CL",
		"a\x1b[2J":       "a [2J",
		"|B4back":        "|B4back",
		"trailing pipe|": "trailing pipe|",
	}
	for in, want := range cases {
		got := DefangScreenCodes(in)
		if got != want {
			t.Errorf("DefangScreenCodes(%q) = %q, want %q", in, got, want)
		}
		if out := string(ReplacePipeCodes([]byte(got))); strings.Contains(out, ClearScreen()) {
			t.Errorf("%q still clears the screen", in)
		}
	}
}
