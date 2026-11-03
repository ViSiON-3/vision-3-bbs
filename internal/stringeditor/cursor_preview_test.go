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
