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
