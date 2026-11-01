package ansi

import "testing"

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
		{"caf\x82 raw CP437", "caf\x82 raw CP437"},
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
		if got := StoredBodyText(tc.in); got != tc.want {
			t.Errorf("StoredBodyText(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
