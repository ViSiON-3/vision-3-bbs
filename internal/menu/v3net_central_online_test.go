package menu

import (
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

// @UN@ counts callers on other V3Net boards, once per caller per board
// across networks and nodes, leaving out this board's own; a fresh count
// is served without asking the hub again.
func TestNetOnlineCount(t *testing.T) {
	presence := func(node string, handles ...string) protocol.CentralActivityBoard {
		var es []protocol.CentralActivityEntry
		for i, h := range handles {
			es = append(es, protocol.CentralActivityEntry{Handle: h, Session: node + string(rune('a'+i))})
		}
		return activityBoardOf(node, node, protocol.CentralKindPresence, es...)
	}
	fake := &centralFake{
		node: "self",
		nets: []CentralNetworkInfo{activityNet("one"), activityNet("two")},
		acts: map[string]map[string][]protocol.CentralActivityBoard{
			// Alice on two nodes of beta, and beta on both networks.
			"one": {protocol.CentralKindPresence: {presence("self", "Me"), presence("beta", "Alice", "alice", "Bob")}},
			"two": {protocol.CentralKindPresence: {presence("beta", "Bob"), presence("gamma", "Alice")}},
		},
	}
	e := &MenuExecutor{V3NetCentral: fake}
	if n := e.v3netOnlineCount(); n != 3 {
		t.Errorf("count %d, want 3 (Alice and Bob on beta, Alice on gamma)", n)
	}
	before := fake.fetches
	if n := e.v3netOnlineCount(); n != 3 || fake.fetches != before {
		t.Errorf("fresh count refetched: %d, %d fetches", n, fake.fetches-before)
	}
	out := string(e.renderPromptText("Users: @UC#@ Online: @U@ Net: @UN@", nil, 7, 2, 1))
	if !strings.Contains(out, "Users: 7     Online: 2 Net: 3") {
		t.Errorf("prompt %q", out)
	}

	// V3Net off, or Who's Online offered nowhere: 0, and nothing fetched.
	off := &centralFake{nets: []CentralNetworkInfo{{Network: "one", Enabled: true, SupportKnown: true, Supported: true}}}
	if n := (&MenuExecutor{V3NetCentral: off}).v3netOnlineCount(); n != 0 || off.fetches != 0 {
		t.Errorf("feature off: %d, %d fetches", n, off.fetches)
	}
	if n := (&MenuExecutor{}).v3netOnlineCount(); n != 0 {
		t.Errorf("no V3Net: %d", n)
	}
}
