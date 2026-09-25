package ansi

import (
	"strings"
	"testing"
)

func TestSetVGAPalette(t *testing.T) {
	seq := SetVGAPalette()
	for _, want := range []string{
		"\x1b]4;0;rgb:00/00/00\a",  // palette black is true black
		"\x1b]4;8;rgb:55/55/55\a",  // bright black is VGA dark grey
		"\x1b]4;15;rgb:ff/ff/ff\a", // bright white
		"\x1b]10;rgb:aa/aa/aa\a",   // default fg: VGA light grey
		"\x1b]11;rgb:00/00/00\a",   // default bg: black
	} {
		if !strings.Contains(seq, want) {
			t.Errorf("SetVGAPalette() missing %q", want)
		}
	}
	if n := strings.Count(seq, "\x1b]4;"); n != 16 {
		t.Errorf("SetVGAPalette() sets %d palette entries, want 16", n)
	}
	// Telnet escapes 0xFF; the sequences must be plain ASCII.
	for i := 0; i < len(seq); i++ {
		if seq[i] >= 0x80 {
			t.Fatalf("non-ASCII byte %#x in palette sequence", seq[i])
		}
	}
}
