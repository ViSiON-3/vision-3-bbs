package leaf

import (
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/chat"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

// The sysop's block list keeps a caller on another board out of every
// session here: their room lines, private messages, join and leave
// notices and history are dropped; the handle is matched ignoring case
// and the node exactly. Topic changes are the room's and are kept.
func TestChatBlockListDropsCaller(t *testing.T) {
	ts, _, _ := newTestHub(t, true)
	bobLeaf := subscribedLeaf(t, ts, "bobbbs")
	bob := bobLeaf.NewChatSession("bob")
	defer bob.Close()
	if _, _, err := bob.Join("lobby"); err != nil {
		t.Fatal(err)
	}
	if err := bob.Post("lobby", "before alice"); err != nil {
		t.Fatal(err)
	}

	aliceLeaf := setupLeafWithBlock(t, ts.URL, "Bob@"+bobLeaf.chatSessions.nodeID)
	alice := aliceLeaf.NewChatSession("alice")
	defer alice.Close()
	_, history, err := alice.Join("lobby")
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 0 {
		t.Errorf("history from a blocked caller shown: %+v", history)
	}

	bobNode := bobLeaf.chatSessions.nodeID
	reg := aliceLeaf.chatSessions
	stamp := "2026-01-01T00:00:00Z"
	for name, ev := range map[string]protocol.Event{
		"message": chatEvent(t, protocol.EventChatMessage, protocol.ChatMsgPayload{Room: "lobby", FromHandle: "bob", FromNode: bobNode, Text: "x", Timestamp: stamp}),
		"private": chatEvent(t, protocol.EventChatPrivate, protocol.ChatMsgPayload{FromHandle: "bob", FromNode: bobNode, ToHandle: "alice", ToNode: reg.nodeID, Text: "psst", Timestamp: stamp}),
		"join":    chatEvent(t, protocol.EventChatJoin, protocol.ChatJoinPayload{Room: "lobby", Handle: "bob", BBS: "bobbbs", Node: bobNode}),
		"leave":   chatEvent(t, protocol.EventChatLeave, protocol.ChatLeavePayload{Room: "lobby", Handle: "bob", BBS: "bobbbs", Node: bobNode}),
	} {
		reg.dispatch(ev)
		noEvent(t, alice, name+" from a blocked caller")
	}
	// Someone else on bob's board is not blocked, and bob's topic is kept.
	reg.dispatch(chatEvent(t, protocol.EventChatMessage, protocol.ChatMsgPayload{Room: "lobby", FromHandle: "carol", FromNode: bobNode, Text: "hi", Timestamp: stamp}))
	if ev := <-alice.events; ev.Type != chat.TypeMessage || ev.Message.Handle != "carol" {
		t.Errorf("carol's line = %+v", ev)
	}
	reg.dispatch(chatEvent(t, protocol.EventChatTopic, protocol.ChatTopicPayload{Room: "lobby", Topic: "T", SetBy: "bob"}))
	if ev := <-alice.events; ev.Type != chat.TypeTopic {
		t.Errorf("topic by a blocked caller = %+v, want it kept", ev)
	}
}

// setupLeafWithBlock is subscribedLeaf with a chat block list.
func setupLeafWithBlock(t *testing.T, hubURL string, blocked ...string) *Leaf {
	t.Helper()
	l, _ := setupLeaf(t, hubURL, &mockJAMWriter{})
	l.cfg.BBSName = "alicebbs"
	l.cfg.ChatBlocked = blocked
	l.chatSessions.blocked = chatBlockSet(blocked)
	offerChat(l)
	if err := l.subscribe(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(l.Close)
	return l
}

// chatBlockSet reads handle@node entries and skips anything else.
func TestChatBlockSet(t *testing.T) {
	set := chatBlockSet([]string{"Bob@n1", " carol @ n2 ", "nobody", "@n3", "dave@"})
	if len(set) != 2 || !set[chatBlockKey("bob", "n1")] || !set[chatBlockKey("Carol", "n2")] {
		t.Errorf("set = %v", set)
	}
	if chatBlockSet(nil) != nil {
		t.Error("empty list should be nil")
	}
}
