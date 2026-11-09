package leaf

import (
	"slices"
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/chat"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

// Members from the hub carry each caller's node and board, so the chat
// screen can tell callers on other boards apart from this board's; a leave
// takes out the member on the leaving node, not the first with the handle;
// NodeID names this leaf.
func TestChatSession_MembersCarryBoards(t *testing.T) {
	ts, _, _ := newTestHub(t, true)
	aliceLeaf := subscribedLeaf(t, ts, "alicebbs")
	bobLeaf := subscribedLeaf(t, ts, "bobbbs")
	aliceNode, bobNode := aliceLeaf.chatSessions.nodeID, bobLeaf.chatSessions.nodeID

	alice := aliceLeaf.NewChatSession("alice")
	defer alice.Close()
	bobSysop := bobLeaf.NewChatSession("sysop")
	defer bobSysop.Close()
	aliceSysop := aliceLeaf.NewChatSession("sysop")
	defer aliceSysop.Close()
	for _, s := range []*ChatSession{alice, bobSysop, aliceSysop} {
		if _, _, err := s.Join("lobby"); err != nil {
			t.Fatalf("join %s: %v", s.handle, err)
		}
	}
	if alice.NodeID() != aliceNode || bobSysop.NodeID() != bobNode {
		t.Errorf("NodeID = %q, %q; want %q, %q", alice.NodeID(), bobSysop.NodeID(), aliceNode, bobNode)
	}

	byNode := func(a, b chat.ChatUser) int {
		return strings.Compare(a.Handle+"\x00"+a.Node, b.Handle+"\x00"+b.Node)
	}
	want := []chat.ChatUser{
		{Handle: "alice", Node: aliceNode, BBS: "alicebbs"},
		{Handle: "sysop", Node: aliceNode, BBS: "alicebbs"},
		{Handle: "sysop", Node: bobNode, BBS: "bobbbs"},
	}
	slices.SortFunc(want, byNode)
	got := aliceSysop.Users()
	slices.SortFunc(got, byNode)
	if !slices.Equal(got, want) {
		t.Fatalf("members = %+v, want %+v", got, want)
	}

	// bob's sysop leaves: this board's sysop stays listed.
	aliceLeaf.chatSessions.dispatch(chatEvent(t, protocol.EventChatLeave,
		protocol.ChatLeavePayload{Room: "lobby", Handle: "sysop", BBS: "bobbbs", Node: bobNode}))
	got = aliceSysop.Users()
	if len(got) != 2 || slices.ContainsFunc(got, func(u chat.ChatUser) bool { return u.Node == bobNode }) {
		t.Errorf("after bob's sysop left: %+v", got)
	}
	if ev := <-aliceSysop.events; ev.Type != chat.TypeLeave || ev.Leave.Node != bobNode {
		t.Errorf("leave event = %+v, want bob's node on it", ev)
	}
}

// A hub that predates members lists handles only; those members have no
// node or board. A hub that lists members is taken at its word.
func TestMembersOfFallsBackToHandles(t *testing.T) {
	old := protocol.ChatJoinResponse{Users: []string{"alice", "bob"}}
	if got := membersOf(old); !slices.Equal(got, []chat.ChatUser{{Handle: "alice"}, {Handle: "bob"}}) {
		t.Errorf("older hub: %+v", got)
	}
	current := protocol.ChatJoinResponse{Users: []string{"alice"}, Members: []protocol.ChatMember{{Handle: "alice", Node: "n", BBS: "B"}}}
	if got := membersOf(current); !slices.Equal(got, []chat.ChatUser{{Handle: "alice", Node: "n", BBS: "B"}}) {
		t.Errorf("current hub: %+v", got)
	}
}

// A leave removes the member on its node; one without a node, from an older
// hub, the first member with the handle; one for a node not in the list
// removes nothing.
func TestRemoveMember(t *testing.T) {
	users := []chat.ChatUser{{Handle: "sysop", Node: "n1", BBS: "A"}, {Handle: "sysop", Node: "n2", BBS: "B"}, {Handle: "bob", Node: "n1", BBS: "A"}}
	for _, tc := range []struct {
		node string
		want []chat.ChatUser
	}{
		{"n2", []chat.ChatUser{users[0], users[2]}},
		{"", []chat.ChatUser{users[1], users[2]}},
		{"n9", users},
	} {
		if got := removeMember(slices.Clone(users), "sysop", tc.node); !slices.Equal(got, tc.want) {
			t.Errorf("remove sysop on %q = %+v, want %+v", tc.node, got, tc.want)
		}
	}
}
