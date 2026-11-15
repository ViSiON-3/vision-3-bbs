package menu

import (
	"bytes"
	"regexp"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
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
		{"|XY01,01|CL|P|PP|DE|CR |16", "|XY01,01|CL|P|PP|DE|CR |16"},
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
	got := renderCustomPrompt(user.CleanCustomPrompt("a\x1b[2Jb\x07c☺é€"), nil)
	if want := []byte("a[2Jbc?\x82?"); !bytes.Equal(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

// The caller's text takes the same colour codes as the rest of the board,
// longest first: |B10 is a bright green background, |23 resets, and |16,
// which the board has no code for, prints.
func TestRenderCustomPromptFullColourSet(t *testing.T) {
	got := renderCustomPrompt("|B10|15Ready>|B8x|23y|16", nil)
	want := string(ansi.ReplacePipeCodes([]byte("|B10|15"))) + "Ready>" +
		string(ansi.ReplacePipeCodes([]byte("|B8"))) + "x" + string(ansi.ReplacePipeCodes([]byte("|23"))) + "y|16"
	if string(got) != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

// A colour code and the placeholder after it are expanded apart: |B1 then a
// handle starting "0" is a red background and "0Neo", not |B10 and "Neo".
func TestRenderCustomPromptKeepsTokensApart(t *testing.T) {
	got := renderCustomPrompt("|B1|UH", map[string]string{"|UH": "0Neo"})
	want := append(ansi.ReplacePipeCodes([]byte("|B1")), "0Neo"...)
	if !bytes.Equal(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

// A placeholder's value keeps its colour codes, but its screen codes print.
func TestRenderCustomPromptEscapesPlaceholderValues(t *testing.T) {
	got := renderCustomPrompt("|CAN>", map[string]string{"|CAN": "|14Gen|CLeral|XY01,01"})
	if vis := visibleText(got); vis != "Gen|CLeral|XY01,01>" {
		t.Errorf("shown as %q", vis)
	}
	if !bytes.Contains(got, ansi.ReplacePipeCodes([]byte("|14"))) {
		t.Errorf("the value's colour was lost: %q", got)
	}
	if m := screenEscapeRe.Find(got); m != nil {
		t.Errorf("a screen code in the value acted: %q", m)
	}
}

// A placeholder's value goes to CP437 like the caller's own text, so the
// whole prompt is in one encoding.
func TestRenderCustomPromptValuesAreCP437(t *testing.T) {
	got := renderCustomPrompt("|GL>", map[string]string{"|GL": "Montréal"})
	if want := []byte("Montr\x82al>"); !bytes.Equal(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}
