package menu

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/message"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// runNewscanConfig's message-area picker lays out area names with an inline
// padRight closure and an areaName[:37]-style truncation, both measuring
// len() in bytes — the same class of bug fixed in runFileNewscanConfig
// (internal/menu/file_newscan.go). Area names are sysop-editable UTF-8 JSON
// and can be auto-created from hub-supplied names by V3Net area sync, so
// both are reachable with real data.
func TestRunNewscanConfigAreaNameRuneCorrect(t *testing.T) {
	// 20 CJK runes = 60 bytes: rune count is well under the 40-column limit,
	// but the byte length is over it, so a byte-length truncation check wrongly
	// truncates a name that should render in full, splitting mid-rune.
	longName := strings.Repeat("日", 20)
	// 20 "é" runes = 40 bytes exactly: byte length reaches the 40-column pad
	// width even though the rune (visible) length is only 20, so a byte-length
	// pad check skips padding a name that is visibly 20 columns short.
	padName := strings.Repeat("é", 20)

	dataDir, cfgDir := t.TempDir(), t.TempDir()
	mm, err := message.NewMessageManager(dataDir, cfgDir, "TestBBS", nil)
	if err != nil {
		t.Fatalf("NewMessageManager: %v", err)
	}
	if _, err := mm.AddArea(message.MessageArea{Tag: "AREA1", Name: longName, AreaType: "local"}); err != nil {
		t.Fatalf("AddArea longName: %v", err)
	}
	if _, err := mm.AddArea(message.MessageArea{Tag: "AREA2", Name: padName, AreaType: "local"}); err != nil {
		t.Fatalf("AddArea padName: %v", err)
	}

	um, err := user.NewUserManager(t.TempDir())
	if err != nil {
		t.Fatalf("NewUserManager: %v", err)
	}
	u, err := um.AddUser("password", "Tester", "Real Name", "Loc")
	if err != nil {
		t.Fatalf("AddUser: %v", err)
	}
	u.AccessLevel = 255

	e := &MenuExecutor{MessageMgr: mm}
	ts := newTestSession("q")
	terminal := newTestTerminal(ts)

	c := &cmdCtx{
		e: e, s: ts, terminal: terminal, userManager: um, currentUser: u,
		nodeNumber: 1, sessionStartTime: time.Now(),
		outputMode: ansi.OutputModeUTF8, termWidth: 80, termHeight: 24,
	}

	if _, _, err := runNewscanConfig(c, ""); err != nil {
		t.Fatalf("runNewscanConfig: %v", err)
	}

	stripped := testAnsiEscape.ReplaceAllString(ts.output(), "")

	if got, want := strings.Count(stripped, "日"), 20; got != want {
		t.Errorf("rendered long area name has %d intact '日' runes, want %d (all 20 fit under the 40-column limit): %q",
			got, want, stripped)
	}

	wantPadded := padName + strings.Repeat(" ", 20) + " ["
	if !strings.Contains(stripped, wantPadded) {
		t.Errorf("rendered short area name row missing %d columns of padding before the bracket; want substring %q in %q",
			20, wantPadded, stripped)
	}
}

// TestNewscanConfigTogglesAndPersists drives NEWSCANCONFIG over the shipped
// areas plus one in another conference: SPACE/Enter toggle the highlighted
// area, arrows and paging move past conference headers, A tags all, N clears
// all, and ESC or Q saves the result to the user record.
func TestNewscanConfigTogglesAndPersists(t *testing.T) {
	env := newMsgEnv(t)
	addScanArea(env, "FAR", 2, 0)

	cases := []struct {
		name  string
		start []string
		keys  string
		want  []string
	}{
		{"space tags the first area", nil, " \x1b", []string{"GENERAL"}},
		{"space again untags it", []string{"GENERAL"}, " q", nil},
		{"down then enter tags the second", nil, "\x1b[B\rQ", []string{"PRIVMAIL"}},
		{"down crosses the conference header", nil, "\x1b[B\x1b[B\x1b[B \x1b", []string{"FAR"}},
		{"up comes back", nil, "\x1b[B\x1b[B\x1b[A \x1b", []string{"PRIVMAIL"}},
		{"page down reaches the last", nil, "\x1b[6~ \x1b", []string{"FAR"}},
		{"page up returns to the first", nil, "\x1b[6~\x1b[5~ \x1b", []string{"GENERAL"}},
		{"ctrl-x and ctrl-e move too", nil, "\x18\x18\x05 \x1b", []string{"PRIVMAIL"}},
		{"A tags all", nil, "a\x1b", []string{"FAR", "GENERAL", "PRIVMAIL"}},
		{"N clears all", []string{"GENERAL", "FAR"}, "n\x1b", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := env.sub(t)
			env.caller.TaggedMessageAreaTags = tc.start
			if err := env.um.UpdateUser(env.caller); err != nil {
				t.Fatal(err)
			}
			r := env.runCmd("NEWSCANCONFIG", env.caller, "", tc.keys)
			if r.err != nil {
				t.Fatalf("NEWSCANCONFIG: %v", r.err)
			}
			got := slices.Clone(env.mustDiskUser(2).TaggedMessageAreaTags)
			slices.Sort(got)
			if !slices.Equal(got, tc.want) {
				t.Errorf("saved tags = %v, want %v", got, tc.want)
			}
			if !r.has(fmt.Sprintf("Newscan configuration saved (%d areas tagged).", len(tc.want))) {
				t.Errorf("save notice missing; output:\n%s", r.text())
			}
		})
	}
}

// TestNewscanConfigListsOnlyReadableAreas checks areas the caller cannot
// read are not offered, and conference names head their areas.
func TestNewscanConfigListsOnlyReadableAreas(t *testing.T) {
	env := newMsgEnv(t)
	lowly := *env.caller
	lowly.AccessLevel = 10 // PRIVMAIL needs 25

	r := env.runCmd("NEWSCANCONFIG", &lowly, "", "a\x1b")
	if r.has("Private Mail") {
		t.Errorf("unreadable PRIVMAIL offered; output:\n%s", r.text())
	}
	if !r.has("Local Areas", "General Discussion") {
		t.Errorf("conference header or area missing; output:\n%s", r.text())
	}
	if got := lowly.TaggedMessageAreaTags; !slices.Equal(got, []string{"GENERAL"}) {
		t.Errorf("A tagged %v, want only GENERAL", got)
	}

	if r := env.runCmd("NEWSCANCONFIG", nil, "", "q"); r.err != nil || r.has("Newscan configuration saved") {
		t.Errorf("anonymous NEWSCANCONFIG saved: %v\n%s", r.err, r.text())
	}
}
