package ansi

import (
	"strings"
	"testing"
)

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

func TestDrawCP437ControlGlyphs(t *testing.T) {
	if got := string(DrawCP437ControlGlyphs([]byte("─\x1eA\x1b[1;31m\r\n\x07"))); got != "─▲A\x1b[1;31m\r\n\x07" {
		t.Errorf("got %q", got)
	}
	in := []byte("plain text\r\n")
	if got := DrawCP437ControlGlyphs(in); &got[0] != &in[0] {
		t.Error("text with no control glyphs was copied")
	}
}

// HardWrap counts a glyph control byte as the cell it is drawn in, so a
// row using ▲ ▼ breaks where an 80-column terminal wraps it.
func TestHardWrapCountsControlGlyphs(t *testing.T) {
	row := strings.Repeat("-", 78) + "\x1e\x1f" + "X"
	if got := string(HardWrap([]byte(row), 80, false)); got != row[:80]+"\r\nX" {
		t.Errorf("got %q", got)
	}
}

// ArtForOutput draws the glyph control bytes for a UTF-8 terminal and
// leaves art alone for a CP437 one.
func TestArtForOutputDrawsControlGlyphs(t *testing.T) {
	art := []byte("\xc4\x1eSysop\x1e\xc4")
	if got := string(ArtForOutput(art, OutputModeUTF8)); got != "─▲Sysop▲─" {
		t.Errorf("UTF-8: %q", got)
	}
	if got := ArtForOutput(art, OutputModeCP437); string(got) != string(art) {
		t.Errorf("CP437: %q", got)
	}
}
