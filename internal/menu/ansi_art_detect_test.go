package menu

import "testing"

// Art that positions with nothing but these sequences is still art.
func TestArtDetectionSeesGEFd(t *testing.T) {
	for _, seq := range []string{"\x1b[10G", "\x1b[2E", "\x1b[2F", "\x1b[3d", "\x1b[G", "\x1b[E", "\x1b[F", "\x1b[d"} {
		body := "row one" + seq + "more"
		if !detectAnsiArtInMessage(body) {
			t.Errorf("detectAnsiArtInMessage(%q) = false", body)
		}
		if !containsAnsiArt(body) {
			t.Errorf("containsAnsiArt(%q) = false", body)
		}
	}
	if detectAnsiArtInMessage("plain \x1b[1;33mcolour\x1b[0m only") {
		t.Error("colour alone is not art")
	}
}
