package ansi

import "testing"

func TestCP437ArtToUTF8(t *testing.T) {
	for name, tc := range map[string]struct{ in, want string }{
		// ESC[D steps back over the space; the arrow is drawn in its place.
		"arrows":            {"\xc4 \x1b[D\x1e\x1b[0mSysop \x1b[D\x1f", "─ \x1b[D▲\x1b[0mSysop \x1b[D▼"},
		"controls acted on": {"a\r\nb\tc\x07\x08", "a\r\nb\tc\x07\x08"},
		"utf-8 art":         {"─ \x1e", "─ ▲"},
		"plain utf-8":       {"─ ok", "─ ok"},
		"escape kept":       {"\x1b[1;31m\x01", "\x1b[1;31m☺"},
	} {
		if got := string(CP437ArtToUTF8([]byte(tc.in))); got != tc.want {
			t.Errorf("%s: %q, want %q", name, got, tc.want)
		}
	}
}
