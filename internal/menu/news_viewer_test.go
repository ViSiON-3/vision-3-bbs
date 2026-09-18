package menu

import (
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
)

// NEWSHDR.ANS ends with its rule, a line break, a colour code and one more
// line break, so the header was a row taller than it looked and the body
// started with a blank line under it (#372).
func TestNewsHeaderDropsTrailingBlankRow(t *testing.T) {
	// The shipped header's shape: content, then a colour-only final row.
	hdr := string(ansi.ReplacePipeCodes([]byte("|15Title\r\n|08" + strings.Repeat("\xc4", 10) + "\r\n|07\r\n")))

	trimmed := trimTrailingBlankRows([]byte(hdr))

	// Two rows of real content: the title and the rule. The trailing colour
	// row and the empty remainder after the last line break both go, which is
	// the blank line that used to sit under the header.
	if got := findHeaderEndRow(trimmed); got != 2 {
		t.Errorf("header is %d rows, want 2 (untrimmed it was %d)", got, findHeaderEndRow([]byte(hdr)))
	}
	// The colour the header chose for the body must survive the trim.
	if !strings.Contains(string(trimmed), "\x1b[0;37m") {
		t.Errorf("the trailing colour code was dropped with its row: %q", string(trimmed))
	}
	// And nothing visible may be lost.
	if got := stripEscapes(string(trimmed)); !strings.Contains(got, "Title") || !strings.Contains(got, "\xc4") {
		t.Errorf("visible header content was lost: %q", got)
	}
}

// Several blank rows collapse, not just one, and every colour they set is kept.
func TestNewsHeaderDropsSeveralTrailingBlankRows(t *testing.T) {
	hdr := "Title\r\n\x1b[0;37m\r\n   \r\n\x1b[1;30m\r\n"
	trimmed := string(trimTrailingBlankRows([]byte(hdr)))

	if rows := strings.Count(trimmed, "\r\n"); rows != 0 {
		t.Errorf("expected every trailing blank row to go, %d line break(s) left: %q", rows, trimmed)
	}
	for _, code := range []string{"\x1b[0;37m", "\x1b[1;30m"} {
		if !strings.Contains(trimmed, code) {
			t.Errorf("colour %q was dropped: %q", code, trimmed)
		}
	}
}

// A header that ends in real content must be left alone.
func TestNewsHeaderKeepsContentRows(t *testing.T) {
	hdr := "Title\r\nSubtitle"
	if got := string(trimTrailingBlankRows([]byte(hdr))); got != hdr {
		t.Errorf("header was altered:\n got  %q\n want %q", got, hdr)
	}
}

// graphicEscapesOnly keeps the colour and discards everything else, which is
// what lets a dropped row hand its colour upwards without also moving the
// cursor or erasing part of the row above.
func TestNewsGraphicEscapesOnly(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"\x1b[0;37m", "\x1b[0;37m"},
		{"   \x1b[1;30m  ", "\x1b[1;30m"},
		{"text\x1b[31mmore", "\x1b[31m"},
		{"nothing", ""},
		{"", ""},
		// Only colour is carried: cursor and erase depend on where they ran.
		{"\x1b[5;1H\x1b[0;37m", "\x1b[0;37m"},
		{"\x1b[K", ""},
		{"\x1b[?7h", ""},
		{"\x1b[38;5;208m", "\x1b[38;5;208m"},
	} {
		if got := graphicEscapesOnly(tc.in); got != tc.want {
			t.Errorf("graphicEscapesOnly(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// The viewer scrolls a wrapped body, so every line it will draw must carry the
// colour state it inherits - a window starting part-way down is not preceded on
// screen by the lines above it (#362).
func TestNewsBodyLinesCarryEntryState(t *testing.T) {
	item := &NewsItem{Body: "|12red line\nstill red\nstill red"}
	lines := renderNewsBody(item, shippedHeaderWidth, 80, ansi.OutputModeUTF8)
	if len(lines) < 3 {
		t.Fatalf("expected at least 3 body lines, got %d: %q", len(lines), lines)
	}

	states := buildBodyEntryStates(lines)
	if states[0] != bodyDefaultColour {
		t.Errorf("first line should start from the default, got %q", states[0])
	}
	if states[1] == bodyDefaultColour {
		t.Errorf("second line lost the colour set on the first: %q", states[1])
	}
	if states[1] != states[2] {
		t.Errorf("colour changed without a code: %q then %q", states[1], states[2])
	}
}

// An item with no body must not produce a phantom line to scroll.
func TestNewsEmptyBodyRendersNoLines(t *testing.T) {
	if got := renderNewsBody(&NewsItem{}, shippedHeaderWidth, 80, ansi.OutputModeUTF8); len(got) != 0 {
		t.Errorf("empty body produced %d line(s): %q", len(got), got)
	}
}

// trimTrailingBlankRows keeps the colour the header ends on so the body
// inherits it, so the colour fold has to start from that state rather than
// from grey. Otherwise a header ending in anything but grey coloured the body
// one way when written in sequence and another when paged.
func TestNewsBodyFoldStartsFromTheHeaderColour(t *testing.T) {
	// A header whose final row sets bright cyan and nothing else.
	hdr := trimTrailingBlankRows(ansi.ReplacePipeCodes([]byte("|15Title\r\n|11\r\n")))

	state := ansi.NewSGRState()
	state.Write(bodyDefaultColour)
	state.Write(string(hdr))
	seeded := state.Escape()

	if seeded == bodyDefaultColour {
		t.Fatalf("fixture header must leave a colour other than the default, got %q", seeded)
	}

	body := []string{"plain line", "another plain line"}
	states := buildBodyEntryStatesFrom(body, seeded)
	for i, got := range states {
		if got != seeded {
			t.Errorf("body line %d starts in %q, want the header's %q", i, got, seeded)
		}
	}

	// And the unseeded form still starts from grey, for the message reader.
	if got := buildBodyEntryStates(body)[0]; got != bodyDefaultColour {
		t.Errorf("unseeded fold starts in %q, want the default %q", got, bodyDefaultColour)
	}
}
