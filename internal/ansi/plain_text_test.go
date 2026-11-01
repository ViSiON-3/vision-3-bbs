package ansi

import (
	"strings"
	"testing"
)

func TestPlainText(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"plain", "plain"},
		{"\x1b[1;31mred\x1b[0m", "red"},
		{"a\x1b[2Jb\x1b[5;10Hc\x1b[Kd\x1b7e\x1b8f\x1b[s\x1b[u", "abcdef"},
		{"title\x1b]0;evil\x07 here", "title here"},
		{"query\x1b[6n done", "query done"},
		{"one\r\ntwo\rthree\nfour", "one\ntwo\nthree\nfour"},
		{"bell\x07 tab\there \x01smile", "bell tab\there smile"},
		{"c1 \u009b6n is dropped whole", "c1  is dropped whole"},
		{"caf\x82 raw CP437", "caf\uFFFD raw CP437"}, // decode with DecodeBody first
		{"|04pipes stay|07", "|04pipes stay|07"},
		{"cut short\x1b[1;3", "cut short"},
	} {
		if got := PlainText(tc.in); got != tc.want {
			t.Errorf("PlainText(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// StoredBodyText filters a single-byte body a byte at a time, so CP437 pairs
// that form a UTF-8 C1 control stay, while a UTF-8 body is filtered as
// BodyText filters it.
func TestStoredBodyText(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"CP\xc2\x9b\xdb\x05 \x1b[6nok\x1b[1mbold", "CP\xc2\x9b\xdb ok\x1b[1mbold"},
		{"utf8 \u009b6n café\x1b]0;t\x07", "utf8  café"},
		{"\x1b]0;unterminated\xdb\nnext", "\nnext"},
	} {
		if got := StoredBodyText(tc.in, ""); got != tc.want {
			t.Errorf("StoredBodyText(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// Review findings on #872. A terminal string in a single-byte body ends only
// at a 7-bit terminator: CP437 C2 9C is two characters, not U+009C (ST).
func TestStoredBodyTextSingleByteStringTerminator(t *testing.T) {
	for _, in := range []string{
		"A\x1b]0;\xc2\x9cleaked\xdb\x07B",  // OSC
		"A\x1bPq\xc2\x9cleaked\xdb\x1b\\B", // DCS
	} {
		if got := StoredBodyText(in, ""); got != "AB" {
			t.Errorf("StoredBodyText(%q) = %q, want %q", in, got, "AB")
		}
	}
}

// A body labelled UTF-8 that holds a stray CP437 byte is still UTF-8: its
// encoded C1 controls are controls.
func TestStoredBodyTextMixedUTF8KeepsNoC1(t *testing.T) {
	in := "x\u009b6n café \x82 y"
	got := StoredBodyText(in, "UTF-8")
	if strings.Contains(got, "\u009b") || strings.Contains(got, "6n") {
		t.Errorf("StoredBodyText(%q, UTF-8) = %q, kept the CSI query", in, got)
	}
	if !strings.Contains(got, "café \x82 y") {
		t.Errorf("StoredBodyText(%q, UTF-8) = %q, lost text", in, got)
	}
}

// Taking a byte out from between two stray bytes must not join them into a
// control: C2 and 9B either side of a dropped byte would make U+009B.
func TestFiltersDoNotRecombineIntoC1(t *testing.T) {
	for name, got := range map[string]string{
		"PlainText(DecodeBody)": PlainText(DecodeBody("START\x1b[\xc2\x01\x9b6n END", "")),
		"BodyText":              BodyText("a\xc2\x05\x9b6n b"),
		"StoredBodyText mixed":  StoredBodyText("a\xc2\x05\x9b6n é", "UTF-8"),
	} {
		if strings.Contains(got, "\u009b") {
			t.Errorf("%s = %q, holds U+009B", name, got)
		}
	}
}
