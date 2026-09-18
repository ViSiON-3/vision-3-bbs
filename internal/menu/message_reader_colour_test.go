package menu

import (
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
)

func entryStatesFor(t *testing.T, body string) ([]string, []string) {
	t.Helper()
	formatted := formatMessageBody(body, "", false)
	lines := wrapAnsiString(string(ansi.ReplacePipeCodes([]byte(formatted))), 79, ansi.OutputModeUTF8)
	return lines, buildBodyEntryStates(lines)
}

// The body starts grey, so the cyan of the |11 reading suffix cannot bleed in.
func TestBodyEntryStatesStartGrey(t *testing.T) {
	_, states := entryStatesFor(t, "just plain text\rand more plain text\r")
	for i, st := range states {
		if st != bodyDefaultColour {
			t.Errorf("line %d entry state is %q, want the default %q", i, st, bodyDefaultColour)
		}
	}
}

// The reported bug (#362): colour set on one line and relied on by the lines
// beneath it. Each of those lines must carry the state it inherits, so a
// window that starts below the colour still draws it.
func TestBodyEntryStatesCarryColourForward(t *testing.T) {
	lines, states := entryStatesFor(t, "|12red one\rstill red\rstill red\r")
	if len(lines) < 3 {
		t.Fatalf("expected at least 3 lines, got %d: %q", len(lines), lines)
	}

	if states[0] != bodyDefaultColour {
		t.Errorf("first line should start from the default, got %q", states[0])
	}
	if states[1] == bodyDefaultColour {
		t.Fatalf("second line lost the colour set on the first; got the default %q", states[1])
	}
	if states[1] != states[2] {
		t.Errorf("colour changed without a code: %q then %q", states[1], states[2])
	}

	// It must be the colour |12 actually produced, not merely "something".
	want := ansi.NewSGRState()
	want.Write(bodyDefaultColour)
	want.Write(string(ansi.ReplacePipeCodes([]byte("|12"))))
	if states[1] != want.Escape() {
		t.Errorf("carried state is %q, want %q", states[1], want.Escape())
	}
}

// A reset in the body returns later lines to the default rather than pinning
// them to whatever came before it.
func TestBodyEntryStatesHonourAReset(t *testing.T) {
	_, states := entryStatesFor(t, "|12red\r|07grey again\rstill grey\r")
	if len(states) < 3 {
		t.Fatalf("expected at least 3 lines, got %d", len(states))
	}
	if states[2] == states[1] {
		t.Errorf("the |07 on line 2 did not take effect: %q", states[2])
	}
	if strings.Contains(states[2], "31") {
		t.Errorf("red survived a colour change: %q", states[2])
	}
}

// Non-SGR escapes must not disturb the tracker.
func TestBodyEntryStatesIgnoreNonColourEscapes(t *testing.T) {
	sgr := ansi.NewSGRState()
	sgr.Write(bodyDefaultColour)
	before := sgr.Escape()
	sgr.Write("\x1b[K\x1b[5;10H\x1b[?25l plain text")
	if after := sgr.Escape(); after != before {
		t.Errorf("non-SGR escapes changed the state: %q -> %q", before, after)
	}
}
