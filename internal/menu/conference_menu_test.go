package menu

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// padRight and truncateStr lay out the area, conference, file and V3Net
// pickers. Both must work in characters: their inputs include area and
// conference names from UTF-8 JSON and network names fetched from the remote
// V3Net registry.

func TestPadRightPadsToVisibleColumns(t *testing.T) {
	tests := []struct {
		name  string
		s     string
		width int
		want  int // expected visible columns
	}{
		{"ascii shorter", "abc", 10, 10},
		{"ascii exact", "abcdefghij", 10, 10},
		{"ascii longer left alone", "abcdefghijkl", 10, 12},
		{"multi-byte padded by runes", "café", 10, 10},
		{"cjk padded by runes", "日本語", 10, 10},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := utf8.RuneCountInString(padRight(tt.s, tt.width))
			if got != tt.want {
				t.Errorf("padRight(%q, %d) = %d columns, want %d", tt.s, tt.width, got, tt.want)
			}
		})
	}
}

func TestTruncateStrCutsOnRuneBoundaries(t *testing.T) {
	tests := []struct {
		name   string
		s      string
		maxLen int
		want   string
	}{
		{"empty input", "", 10, ""},
		{"maxLen 0", "abcdef", 0, ""},
		{"shorter than max", "abc", 10, "abc"},
		{"exactly max", "abcde", 5, "abcde"},
		{"longer gets ellipsis", "abcdefgh", 5, "abc.."},
		{"maxLen 2 hard cut", "abcdef", 2, "ab"},
		{"maxLen 1 hard cut", "abcdef", 1, "a"},
		{"multi-byte fits by runes", "café", 4, "café"},
		{"multi-byte cut on boundary", "café society", 6, "café.."},
		{"cjk cut on boundary", "日本語のメッセージ", 5, "日本語.."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncateStr(tt.s, tt.maxLen)
			if got != tt.want {
				t.Errorf("truncateStr(%q, %d) = %q, want %q", tt.s, tt.maxLen, got, tt.want)
			}
			if !utf8.ValidString(got) {
				t.Errorf("truncateStr(%q, %d) produced invalid UTF-8: %q", tt.s, tt.maxLen, got)
			}
		})
	}
}

// The two are used together to build fixed-width rows; a multi-byte name must
// not push the row wider than the column budget.
func TestPadRightAfterTruncateStrHoldsColumnBudget(t *testing.T) {
	for _, name := range []string{"ascii name", "café society", "日本語のメッセージ"} {
		row := padRight(truncateStr(name, 12), 12)
		if got := utf8.RuneCountInString(row); got != 12 {
			t.Errorf("row for %q = %d columns, want 12 (%q)", name, got, row)
		}
		if strings.Contains(row, "�") {
			t.Errorf("row for %q contains a replacement char: %q", name, row)
		}
	}
}

// TestChangeMsgConferenceJoinsAndPersists picks a conference from the text
// list by number and by tag: the caller joins it (for messages and files),
// lands in its first readable area, and the choice is saved.
func TestChangeMsgConferenceJoinsAndPersists(t *testing.T) {
	env := newMsgEnv(t)
	farID := addScanArea(env, "FAR", 2, 0)

	for _, tc := range []struct{ name, input string }{
		{"by number", "2\r"},
		{"by tag", "felonynet\r"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := env.sub(t)
			u := *env.caller
			r := env.run(runChangeMsgConference, &u, "", tc.input)
			if r.err != nil {
				t.Fatalf("runChangeMsgConference: %v", r.err)
			}
			if !r.has("Local Areas", "FelonyNet", "Conference Joined!") {
				t.Errorf("list or confirmation missing; output:\n%s", r.text())
			}
			saved := env.mustDiskUser(2)
			if saved.CurrentMsgConferenceID != 2 || saved.CurrentMessageAreaID != farID {
				t.Errorf("saved conference/area = %d/%d, want 2/%d", saved.CurrentMsgConferenceID, saved.CurrentMessageAreaID, farID)
			}
			if saved.CurrentFileConferenceID != 2 {
				t.Errorf("saved file conference = %d, want 2 (joined for both)", saved.CurrentFileConferenceID)
			}
		})
	}
}

// TestChangeMsgConferencePromptLoop checks Enter re-prompts, ? redraws the
// list, an unknown entry is reported, and Q leaves the conference unchanged.
func TestChangeMsgConferencePromptLoop(t *testing.T) {
	env := newMsgEnv(t)
	u := *env.caller

	r := env.run(runChangeMsgConference, &u, "", "\r?\r9\rnope\rq\r")
	if n := strings.Count(r.text(), "FelonyNet message network areas"); n != 2 {
		t.Errorf("list drawn %d times, want 2 (initial and ?)", n)
	}
	for _, want := range []string{"Conference '9' not found", "Conference 'nope' not found"} {
		if !r.has(want) {
			t.Errorf("output lacks %q:\n%s", want, r.text())
		}
	}
	if saved := env.mustDiskUser(2); saved.CurrentMsgConferenceID != 1 {
		t.Errorf("saved conference = %d after Q, want 1", saved.CurrentMsgConferenceID)
	}

	if r := env.run(runChangeMsgConference, nil, "", "2\r"); r.has("FelonyNet") {
		t.Errorf("anonymous caller shown the list:\n%s", r.text())
	}
	env.e.ConferenceMgr = nil
	if r := env.run(runChangeMsgConference, &u, "", "2\r"); r.has("FelonyNet") || r.err != nil {
		t.Errorf("no conference manager: err=%v output:\n%s", r.err, r.text())
	}
}

// TestNextPrevMsgConfWraps steps through the conferences with NEXTMSGCONF
// and PREVMSGCONF: each wraps at the ends and the choice is saved.
func TestNextPrevMsgConfWraps(t *testing.T) {
	env := newMsgEnv(t)
	u := env.caller

	for _, step := range []struct {
		cmd  string
		want int
	}{{"NEXTMSGCONF", 2}, {"NEXTMSGCONF", 1}, {"PREVMSGCONF", 2}, {"PREVMSGCONF", 1}} {
		r := env.runCmd(step.cmd, u, "", "")
		if r.err != nil {
			t.Fatalf("%s: %v", step.cmd, r.err)
		}
		if got := env.mustDiskUser(2).CurrentMsgConferenceID; got != step.want {
			t.Fatalf("after %s saved conference = %d, want %d", step.cmd, got, step.want)
		}
	}

	if r := env.runCmd("NEXTMSGCONF", nil, "", ""); r.err != nil || r.user != nil {
		t.Errorf("anonymous NEXTMSGCONF: user=%v err=%v", r.user, r.err)
	}
}
