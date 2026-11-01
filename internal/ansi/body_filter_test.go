package ansi

import (
	"strings"
	"testing"
)

func TestBodyTextKeeps(t *testing.T) {
	for _, s := range []string{
		"plain text\r\n\ttabbed",
		"\x1b[0m\x1b[1;31m\x1b[38;5;12m\x1b[38:5:12m\x1b[m",
		"\x1b[A\x1b[5B\x1b[C\x1b[12D\x1b[2E\x1b[F\x1b[10G\x1b[H\x1b[1;1H\x1b[5d\x1b[1;1f",
		"\x1b[J\x1b[0J\x1b[1J\x1b[2J\x1b[K\x1b[0K\x1b[1K\x1b[2K",
		"\x1b[s\x1b[u\x1b7\x1b8",
		"café █▓▒░ ¢   日本語",
		"\x01\x02\x03\x04\x06\x10\x11\x12\x13\x14\x15\x16\x17\x19\x1c\x1d\x1e\x1f",
		// Raw CP437: every byte of the C1 range is a character there.
		"\x80\x81\x82\x83\x84\x85\x8d\x90\x98\x9b\x9c\x9d\x9e\x9f\xc4\xb0\xdb",
		"\x9b2J", // a cent sign and text, not CSI
	} {
		if got := BodyText(s); got != s {
			t.Errorf("BodyText(%q) = %q", s, got)
		}
	}
}

func TestBodyTextDrops(t *testing.T) {
	for _, s := range []string{
		"\x1b]0;P\x07", "\x1b]0;P\x1b\\", "\x1bPqP\x1b\\", "\x1b_P\x1b\\", "\x1b^P\x1b\\", "\x1bXP\x1b\\",
		"\x1b]0;P\u009c", "\x1bPqP\u009c", "\u009d0;P\u009c", "\u009d0;P\x07", "\u0090P\u009c", "\u0090P\x1b\\",
		"\u0098P\u009c", "\u009eP\u009c", "\u009fP\u009c", "\x1b]0;P\x18", "\x1b]0;P\x1a", "\u009b2J", "\u009b6n", "\u009b?25l",
		"\u0080", "\u0084", "\u0085", "\u008d", "\u0099", "\u009c",
		"\x1b[6n", "\x1b[5n", "\x1b[?6n", "\x1b[c", "\x1b[0c", "\x1b[>c", "\x1b[=c", "\x1b[?25$p", "\x1b[18t", "\x1b[21t", "\x1b[x",
		"\x1bc", "\x1b[!p", "\x1b[?1049h", "\x1b[?25l", "\x1b[?1000h", "\x1b[?2004h", "\x1b[?7l", "\x1b[?7h", "\x1b[4h", "\x1b[>4;2m",
		"\x1b[?1m", "\x1b[1 m", "\x1b[3J", "\x1b[1s", "\x1b[1u", "\x1b[?u", "\x1b[1:2H",
		"\x1b[1;5r", "\x1b[5S", "\x1b[5T", "\x1b[5L", "\x1b[5M", "\x1b[5P", "\x1b[5@", "\x1b[5X", "\x1b[5i",
		"\x1b(0", "\x1b)B", "\x1b#8", "\x1b%G", "\x1b F", "\x1b=", "\x1b>", "\x1bD", "\x1bE", "\x1bH", "\x1bM", "\x1bN", "\x1b\\",
		"\x05", "\x07", "\x08", "\x0b", "\x0c", "\x0e", "\x0f", "\x18", "\x1a", "\x7f",
	} {
		if got := BodyText("a" + s + "b"); got != "ab" {
			t.Errorf("BodyText(%q) = %q", "a"+s+"b", got)
		}
		if got := BodyText(s); got != "" {
			t.Errorf("BodyText(%q) = %q, at the end of the body", s, got)
		}
	}
}

// Taking a sequence out cannot join what is left into a new one.
func TestBodyTextDoesNotRecombine(t *testing.T) {
	for in, want := range map[string]string{
		"\x1b\x1b[6n":          "",
		"\x1b\u0085[6n":        "[6n",
		"\x1b\x07[6n":          "[6n",
		"\x1b]0;\x07\x1b[6n":   "",
		"\u009b\u009b6n":       "",
		"\x1b":                 "",
		"a\x1b\nb":             "a\nb",
		"\x1b[3\x07m":          "m",
		"\x1b[1\nline":         "\nline",
		"\x1b[":                "",
		"\x1b[12;":             "",
		"\x1b(":                "",
		"\x1b]0;a\x1b[1mb\x07": "\x1b[1mb",
	} {
		if got := BodyText(in); got != want {
			t.Errorf("BodyText(%q) = %q, want %q", in, got, want)
		}
		if again := BodyText(BodyText(in)); again != BodyText(in) {
			t.Errorf("BodyText(%q) changed when run twice: %q", in, again)
		}
	}
}

// A string with no terminator takes its own line and leaves the rest.
func TestBodyTextUnterminatedString(t *testing.T) {
	for _, intro := range []string{"\x1b]0;", "\x1bP", "\x1b_", "\x1b^", "\x1bX", "\u009d", "\u0090", "\u0098", "\u009e", "\u009f"} {
		for _, eol := range []string{"\n", "\r\n", "\r"} {
			in := "one" + intro + "PAYLOAD" + eol + "two" + eol
			if got, want := BodyText(in), "one"+eol+"two"+eol; got != want {
				t.Errorf("BodyText(%q) = %q, want %q", in, got, want)
			}
		}
		if got := BodyText("one" + intro + "PAYLOAD"); got != "one" {
			t.Errorf("%q to the end of the body = %q", intro, got)
		}

		// A terminator across lines ends it, so the payload goes.
		if got := BodyText("one" + intro + "P\nP\x1b\\two"); got != "onetwo" {
			t.Errorf("%q over two lines = %q", intro, got)
		}

		// One too far off doesn't: only the first line goes.
		in := "one" + intro + "P\n" + strings.Repeat("x", maxBodyStringLen) + "\x1b\\two"
		if got, want := BodyText(in), "one\n"+strings.Repeat("x", maxBodyStringLen)+"two"; got != want {
			t.Errorf("%q with a far terminator lost the rest: got %d bytes", intro, len(got))
		}

		// With no line break either, the limit is what stops it, without
		// splitting a character.
		long := "x" + strings.Repeat("é", maxBodyStringLen)
		got := BodyText("one" + intro + long)
		dropped := len("one") + len(intro) + len(long) - len(got)
		if !strings.HasPrefix(got, "oneé") || dropped < maxBodyStringLen-1 || dropped > maxBodyStringLen+len(intro) {
			t.Errorf("%q in one long line dropped %d bytes", intro, dropped)
		}
		if strings.ToValidUTF8(got, "") != got {
			t.Errorf("%q split a character at the limit", intro)
		}
	}
}

// Only OSC ends at BEL. On a DCS, a BEL is more of the payload.
func TestBodyTextBELEndsOnlyOSC(t *testing.T) {
	if got := BodyText("a\x1b]0;P\x07b"); got != "ab" {
		t.Errorf("OSC: %q", got)
	}
	if got := BodyText("a\x1bPq\x07P\x1b\\b"); got != "ab" {
		t.Errorf("DCS: %q", got)
	}
	if got := BodyText("a\x1bPq\x07P\nb"); got != "a\nb" {
		t.Errorf("unterminated DCS: %q", got)
	}
}

// The renderer gets auto-wrap; everything else is as BodyText.
func TestArtBodyText(t *testing.T) {
	for in, want := range map[string]string{
		"\x1b[?7h":       "\x1b[?7h",
		"\x1b[?7l":       "\x1b[?7l",
		"\x1b[?7;25h":    "\x1b[?7h",
		"\x1b[?25;7l":    "\x1b[?7l",
		"\x1b[?25l":      "",
		"\x1b[?1049h":    "",
		"\x1b[?70h":      "",
		"\x1b[7h":        "",
		"\x1b[6n":        "",
		"\x1b]0;P\x07":   "",
		"\x1b[1;1H\xdb":  "\x1b[1;1H\xdb",
		"\x1b[s\r\x1b[u": "\x1b[s\r\x1b[u",
	} {
		if got := ArtBodyText(in); got != want {
			t.Errorf("ArtBodyText(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDropControls(t *testing.T) {
	for in, want := range map[string]string{
		"a\u009b2Jb":           "a2Jb",
		"a\u0085\u009d\u009fb": "ab",
		"a\x1b\x07\x7f\tb\r\n": "ab",
		"5\x9b \x80\xdb":       "5\x9b \x80\xdb",
		"café ¢":               "café ¢",
	} {
		if got := DropControls(in); got != want {
			t.Errorf("DropControls(%q) = %q, want %q", in, got, want)
		}
	}
}
