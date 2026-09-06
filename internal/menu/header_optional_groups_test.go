package menu

import (
	"strings"
	"testing"
)

func expandHdr(t *testing.T, tmpl string, subs map[byte]string) string {
	t.Helper()
	return string(expandHeaderOptionalGroups([]byte(tmpl), subs))
}

// The case this exists for: nine shipped templates print a user-note row that
// is empty for essentially every caller.
func TestHeaderGroupBlanksWhenValueEmpty(t *testing.T) {
	subs := map[byte]string{'U': "", 'F': "Felonius"}

	got := expandHdr(t, "|{ Note: @U@|}", subs)
	if strings.TrimSpace(got) != "" {
		t.Errorf("group should have blanked, got %q", got)
	}
	// Width is preserved rather than removed, so nothing after it shifts.
	if len(got) != len(" Note: ") {
		t.Errorf("blanked width = %d, want %d (literal text, empty value)", len(got), len(" Note: "))
	}
}

func TestHeaderGroupKeptWhenValuePresent(t *testing.T) {
	subs := map[byte]string{'U': "sysop"}
	if got, want := expandHdr(t, "|{ Note: @U@|}", subs), " Note: @U@"; got != want {
		t.Errorf("got %q, want %q — a populated group keeps its content", got, want)
	}
}

// A padded placeholder reserves its column budget, so blanking must emit that
// many spaces or a box border after it moves.
//
// The "#" form reserves the width of the whole token, not the number of
// hashes: "@U#########@" is twelve characters and occupies twelve columns, so
// the padding lines up in an ANSI editor with what the source looks like.
func TestHeaderGroupBlankPreservesPaddedWidth(t *testing.T) {
	subs := map[byte]string{'U': ""}
	const token = "@U#########@"

	got := expandHdr(t, "|{"+token+"|}", subs)
	if strings.TrimSpace(got) != "" {
		t.Errorf("expected blanks, got %q", got)
	}
	if len(got) != len(token) {
		t.Errorf("blanked width = %d, want %d (the token's reserved columns)", len(got), len(token))
	}
}

// Column positions must survive, which is the whole reason headers blank
// rather than remove.
func TestHeaderGroupBlankingKeepsFollowingContentInPlace(t *testing.T) {
	subs := map[byte]string{'U': "", 'M': "LOCAL"}
	tmpl := "|{User Note: @U#####@|} Status: @M@"

	full := expandHdr(t, tmpl, map[byte]string{'U': "x", 'M': "LOCAL"})
	blank := expandHdr(t, tmpl, subs)

	if strings.Index(full, " Status:") != strings.Index(blank, " Status:") {
		t.Errorf("Status moved: populated=%d blank=%d\n full=%q\nblank=%q",
			strings.Index(full, " Status:"), strings.Index(blank, " Status:"), full, blank)
	}
}

// ANSI escapes take no columns, so they must not inflate the blank width.
func TestHeaderGroupWidthIgnoresAnsiEscapes(t *testing.T) {
	subs := map[byte]string{'U': ""}
	got := expandHdr(t, "|{\x1b[1mNote:\x1b[0m@U@|}", subs)
	if strings.TrimSpace(got) != "" {
		t.Errorf("expected blanks, got %q", got)
	}
	if len(got) != len("Note:") {
		t.Errorf("blanked width = %d, want %d — escapes must not count", len(got), len("Note:"))
	}
}

func TestHeaderGroupEdgeCases(t *testing.T) {
	subs := map[byte]string{'U': "", 'F': "Felonius"}

	// No placeholder inside: nothing conditional, keep the literal text.
	if got, want := expandHdr(t, "|{---|}", subs), "---"; got != want {
		t.Errorf("literal-only group: got %q, want %q", got, want)
	}
	// Mixed: one populated code keeps the whole group.
	if got, want := expandHdr(t, "|{@F@/@U@|}", subs), "@F@/@U@"; got != want {
		t.Errorf("mixed group: got %q, want %q", got, want)
	}
	// Unmatched opener degrades to visible text.
	if got, want := expandHdr(t, "a|{ Note: @U@", subs), "a|{ Note: @U@"; got != want {
		t.Errorf("unmatched opener: got %q, want %q", got, want)
	}
	// Templates without groups are untouched.
	if got, want := expandHdr(t, "From: @F@", subs), "From: @F@"; got != want {
		t.Errorf("no groups: got %q, want %q", got, want)
	}
	// Unknown code is not a basis to blank.
	if got, want := expandHdr(t, "|{[@Q@]|}", subs), "[@Q@]"; got != want {
		t.Errorf("unknown code: got %q, want %q", got, want)
	}
}

// Whitespace-only values count as empty; a padded value is still "no value".
func TestHeaderGroupTreatsWhitespaceAsEmpty(t *testing.T) {
	got := expandHdr(t, "|{[@U@]|}", map[byte]string{'U': "   "})
	if strings.TrimSpace(got) != "" {
		t.Errorf("whitespace-only value should blank the group, got %q", got)
	}
}

// Review finding: not every recognised placeholder draws from the substitution
// map. @G@ is gap fill, expanded later against the terminal width, so a group
// holding it must not be blanked just because a neighbouring value is empty —
// that would silently destroy the gap fill.
func TestHeaderGroupKeepsContentWithUnevaluatablePlaceholder(t *testing.T) {
	subs := map[byte]string{'U': ""} // 'G' deliberately absent, as in the real map

	if got, want := expandHdr(t, "|{@U@@G@|}", subs), "@U@@G@"; got != want {
		t.Errorf("group holding a gap fill was blanked: got %q, want %q", got, want)
	}
	// A group with only the empty value still blanks.
	if got := expandHdr(t, "|{[@U@]|}", subs); strings.TrimSpace(got) != "" {
		t.Errorf("group with only an empty value should blank, got %q", got)
	}
}
