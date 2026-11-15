package menu

import (
	"bytes"
	"regexp"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
)

// screenEscapeRe matches an escape sequence that does anything but set colour.
var screenEscapeRe = regexp.MustCompile(`\x1b\[[0-9;]*[^0-9;m]`)

// Colour codes and placeholders expand; every other code a sysop's prompt can
// hold prints as written.
func TestRenderCustomPromptKeepsOnlyColoursAndPlaceholders(t *testing.T) {
	ph := map[string]string{"|UH": "Felonius", "|MN": "MAIN", "|CA": "GEN"}
	var got []byte
	for _, tc := range []struct{ in, want string }{
		{"|09|UH|B1@|MN |CA ||x %%inc.ans%% @RR@ @UC@ |{|UH|}", "Felonius@MAIN GEN |x %%inc.ans%% @RR@ @UC@ |{Felonius|}"},
		{"|XY01,01|CL|P|PP|DE|CR |16|23", "|XY01,01|CL|P|PP|DE|CR |16|23"},
	} {
		out := renderCustomPrompt(tc.in, ph)
		if vis := visibleText(out); vis != tc.want {
			t.Errorf("%q shown as %q, want %q", tc.in, vis, tc.want)
		}
		if m := screenEscapeRe.Find(out); m != nil {
			t.Errorf("a screen code expanded: %q in %q", m, out)
		}
		got = append(got, out...)
	}
	for _, code := range []string{"|09", "|B1"} {
		if seq := ansi.ReplacePipeCodes([]byte(code)); !bytes.Contains(got, seq) {
			t.Errorf("%s did not expand: %q", code, got)
		}
	}
}

// Control characters never reach the screen, and a character with no CP437
// form is a "?" rather than a control code.
func TestRenderCustomPromptDropsControls(t *testing.T) {
	got := renderCustomPrompt("a\x1b[2Jb\x07c☺é€", nil)
	if want := []byte("a[2Jbc?\x82?"); !bytes.Equal(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestCustomPromptColourCodes(t *testing.T) {
	for code, want := range map[string]bool{
		"|00": true, "|07": true, "|15": true, "|B0": true, "|B7": true,
		"|16": false, "|23": false, "|B8": false, "|CL": false, "|1": false,
	} {
		if got := customPromptColourAt([]byte(code), 0); got != want {
			t.Errorf("%s: %v, want %v", code, got, want)
		}
	}
}
