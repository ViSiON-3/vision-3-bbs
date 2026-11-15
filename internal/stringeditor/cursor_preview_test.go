package stringeditor

import (
	"strings"
	"testing"
)

// A cursor code moves the caller's cursor, so the preview draws nothing for
// it and does not count it towards the value's width.
func TestPreviewSkipsCursorCode(t *testing.T) {
	v := "|XY15,12|15Hi"
	if got := PlainTextLength(v); got != 2 {
		t.Errorf("PlainTextLength(%q) = %d, want 2", v, got)
	}
	out := RenderColorString(v, 40)
	if strings.Contains(out, "XY") || strings.Contains(out, "\x1b[12;15H") || !strings.Contains(out, "Hi") {
		t.Errorf("RenderColorString(%q) = %q", v, out)
	}
	// A malformed one is text, as the BBS prints it.
	if got := PlainTextLength("|XY5,12"); got != len("|XY5,12") {
		t.Errorf("malformed code counted as %d cells", got)
	}
}

// A doubled pipe is the BBS's literal-pipe escape: it previews as one pipe
// and never starts a code, so "||XY15,12" is the eight cells of text the
// caller sees, and "||CL" does not vanish.
func TestPreviewDoubledPipeIsLiteral(t *testing.T) {
	for in, want := range map[string]int{"||XY15,12": 8, "||CL": 3, "a||b": 3} {
		if got := PlainTextLength(in); got != want {
			t.Errorf("PlainTextLength(%q) = %d, want %d", in, got, want)
		}
	}
	out := RenderColorString("||XY15,12", 40)
	if !strings.Contains(out, "|XY15,12") || strings.Contains(out, "||") {
		t.Errorf("RenderColorString = %q", out)
	}
}

// |P and |PP save and restore the caller's cursor, so the preview draws
// nothing for them and does not count them; |PP is one code, not |P and "P".
func TestPreviewSkipsSaveRestoreCursor(t *testing.T) {
	for in, want := range map[string]int{"A|PB": 2, "A|PPB": 2, "A|P": 1, "A|PP": 1, "|P|XY01,24 Press |PP": 7} {
		if got := PlainTextLength(in); got != want {
			t.Errorf("PlainTextLength(%q) = %d, want %d", in, got, want)
		}
	}
	// |PV and |POSTS are placeholders, shown as written like the others.
	for _, in := range []string{"|PV", "|POSTS", "Posts: |POSTS"} {
		if got := PlainTextLength(in); got != len(in) {
			t.Errorf("PlainTextLength(%q) = %d, want %d", in, got, len(in))
		}
	}
	out := RenderColorString("A|PPB", 40)
	if strings.Contains(out, "|") || strings.Contains(out, "PB") || !strings.Contains(out, "A") || !strings.Contains(out, "B") {
		t.Errorf("RenderColorString = %q", out)
	}
}

// |23 resets the colours: it draws nothing, and what follows is light gray
// on the panel's own background, whatever came before.
func TestPreviewResetsOn23(t *testing.T) {
	if got := PlainTextLength("A|23B"); got != 2 {
		t.Errorf("PlainTextLength = %d, want 2", got)
	}
	spans := parseColorCodes("|12|B4A|23B")
	last := spans[len(spans)-1]
	if last.text != "B" || last.fg != dosColors[dosLightGray] || last.bg != "" {
		t.Errorf("after |23: %+v", last)
	}
	if first := spans[0]; first.text != "A" || first.fg != dosColors[12] || first.bg == "" {
		t.Errorf("before |23: %+v", first)
	}
}
