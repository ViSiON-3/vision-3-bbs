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
