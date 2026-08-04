package wfcui

import (
	"strings"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/admin"
)

// hostileHandle embeds an OSC title-change sequence and a C0 control byte —
// the shape of a terminal-escape injection a caller could attempt via their
// handle. No rendered view may pass these bytes through to the terminal.
const hostileHandle = "Bad\x1b]0;pwned\x07Guy\x012K"

// assertNoControlBytes fails if s contains any C0 control byte other than
// newline, a DEL, or a C1 control (U+0080–U+009F — U+009B is 8-bit CSI).
// Views under NoColor emit no escapes of their own, so any hit is injected
// data leaking through.
func assertNoControlBytes(t *testing.T, view, s string) {
	t.Helper()
	for _, r := range s {
		if (r < 0x20 && r != '\n') || r == 0x7f || (r >= 0x80 && r <= 0x9f) {
			t.Fatalf("%s leaked control byte %q into rendered output:\n%q", view, r, s)
		}
	}
}

// TestNodeTableStripsControlBytes verifies a hostile handle cannot inject
// terminal escapes via the node list.
func TestNodeTableStripsControlBytes(t *testing.T) {
	m := makeModel(Options{NoColor: true, ASCII: true}, 100, 30)
	m.mode = modeList
	m.snapshot = &admin.SystemSnapshot{
		SystemName: "TestBBS",
		Time:       time.Now(),
		Nodes: []admin.NodeState{
			{NodeID: 1, Handle: hostileHandle, Activity: "read\x1b[2Jing", Status: admin.StatusOnline},
		},
		Counters: admin.Counters{ActiveNodes: 1},
	}
	got := m.View()
	assertNoControlBytes(t, "list view", got)
	if !strings.Contains(got, "Bad") {
		t.Errorf("printable part of handle missing; got:\n%s", got)
	}
}

// TestEventFeedStripsControlBytes verifies a hostile handle cannot inject
// terminal escapes via the event feed.
func TestEventFeedStripsControlBytes(t *testing.T) {
	m := makeModel(Options{NoColor: true, ASCII: true}, 100, 30)
	m.mode = modeList
	m.showLogs = true
	m.snapshot = &admin.SystemSnapshot{SystemName: "TestBBS", Time: time.Now()}
	m.events = []admin.Event{
		{Time: time.Now(), Type: admin.EventCallerConnected, NodeID: 2,
			Handle: hostileHandle, Message: "caller\x1b[31m connected"},
	}
	got := m.View()
	assertNoControlBytes(t, "event feed", got)
}

// TestDetailsViewStripsControlBytes verifies a hostile handle cannot inject
// terminal escapes via the node details view.
func TestDetailsViewStripsControlBytes(t *testing.T) {
	m := makeModel(Options{NoColor: true, ASCII: true}, 100, 30)
	m.mode = modeDetails
	m.selected = 0
	m.snapshot = &admin.SystemSnapshot{
		SystemName: "TestBBS",
		Time:       time.Now(),
		Nodes: []admin.NodeState{
			{NodeID: 1, Handle: hostileHandle, Activity: "x\x1bc", CurrentMenu: "MAIN\x08\x08",
				RemoteAddr: "10.0.0.1:9\x1b[9999", Status: admin.StatusOnline},
		},
	}
	got := m.View()
	assertNoControlBytes(t, "details view", got)
}

// TestSanitizeTerminal covers the helper directly: strips C0 controls and DEL,
// keeps printable text (including non-ASCII) intact.
func TestSanitizeTerminal(t *testing.T) {
	cases := []struct{ in, want string }{
		{"plain", "plain"},
		{hostileHandle, "Bad]0;pwnedGuy2K"},
		{"tab\there", "tabhere"},
		{"höla™", "höla™"},
		{"", ""},
		// C1 controls: U+009B is 8-bit CSI, so "2J" is a screen clear on
		// terminals that honour C1. CP437 high bytes decode to printable
		// codepoints (é, ü, ¢) outside this range, so they survive.
		{"clear\u009b2J", "clear2J"},
		{"pad\u0080ding", "padding"},
		{"café ¢ ü", "café ¢ ü"},
	}
	for _, c := range cases {
		if got := sanitizeTerminal(c.in); got != c.want {
			t.Errorf("sanitizeTerminal(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
