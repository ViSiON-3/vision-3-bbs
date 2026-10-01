package leaf

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/chat"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

func TestChatSession_JoinPostAndHistory(t *testing.T) {
	ts, _, _ := newTestHub(t, true)
	aliceLeaf := subscribedLeaf(t, ts, "alicebbs")
	bobLeaf := subscribedLeaf(t, ts, "bobbbs")
	events := watchEvents(t, subscribedLeaf(t, ts, "observer"))

	alice := aliceLeaf.NewChatSession("alice")
	defer alice.Close()
	bob := bobLeaf.NewChatSession("bob")
	defer bob.Close()

	if _, _, err := alice.Join(""); err == nil {
		t.Error("joining an empty room name should fail before reaching the hub")
	}

	// The room name is normalised before it is sent.
	rooms, history, err := alice.Join("Dev Talk")
	if err != nil {
		t.Fatalf("alice join: %v", err)
	}
	if len(rooms) != 1 || rooms[0].Name != "dev-talk" || rooms[0].UserCount != 1 {
		t.Errorf("rooms after first join = %+v, want dev-talk with 1 user", rooms)
	}
	if len(history) != 0 {
		t.Errorf("new room should have no history, got %+v", history)
	}
	if users := alice.Users(); len(users) != 1 || users[0] != "alice" {
		t.Errorf("alice.Users() = %v, want [alice]", users)
	}
	joined := awaitEvent[protocol.ChatJoinPayload](t, events, protocol.EventChatJoin, nil)
	if joined.Room != "dev-talk" || joined.Handle != "alice" || joined.BBS != "alicebbs" {
		t.Errorf("hub announced join %+v", joined)
	}

	if err := alice.Post("dev-talk", "hello network"); err != nil {
		t.Fatalf("alice post: %v", err)
	}
	posted := awaitEvent[protocol.ChatMsgPayload](t, events, protocol.EventChatMessage, nil)
	if posted.Room != "dev-talk" || posted.FromHandle != "alice" || posted.Text != "hello network" ||
		posted.FromNode != aliceLeaf.cfg.Keystore.NodeID() || posted.FromBBS != "alicebbs" {
		t.Errorf("hub broadcast message %+v", posted)
	}

	// A later joiner gets the message as history, stamped with the room.
	rooms, history, err = bob.Join("dev-talk")
	if err != nil {
		t.Fatalf("bob join: %v", err)
	}
	if len(rooms) != 1 || rooms[0].UserCount != 2 {
		t.Errorf("rooms after second join = %+v, want 2 users", rooms)
	}
	if len(history) != 1 || history[0].Text != "hello network" || history[0].Handle != "alice" ||
		history[0].Room != "dev-talk" || history[0].BBS != "alicebbs" || history[0].Timestamp.IsZero() {
		t.Errorf("join history = %+v", history)
	}
	if users := bob.Users(); len(users) != 2 {
		t.Errorf("bob.Users() = %v, want both handles", users)
	}

	msgs, err := bob.History("dev-talk", 10)
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	if len(msgs) != 1 || msgs[0].Text != "hello network" || msgs[0].Node != aliceLeaf.cfg.Keystore.NodeID() {
		t.Errorf("History = %+v", msgs)
	}
}

func TestChatSession_TopicPrivateAndLeave(t *testing.T) {
	ts, _, _ := newTestHub(t, true)
	aliceLeaf := subscribedLeaf(t, ts, "alicebbs")
	bobLeaf := subscribedLeaf(t, ts, "bobbbs")
	events := watchEvents(t, subscribedLeaf(t, ts, "observer"))

	alice := aliceLeaf.NewChatSession("alice")
	defer alice.Close()
	bob := bobLeaf.NewChatSession("bob")
	defer bob.Close()

	// Topic changes and posts require membership, which the hub enforces.
	if err := bob.SetTopic("lobby", "nope"); err == nil || !strings.Contains(err.Error(), "403") {
		t.Errorf("SetTopic before joining = %v, want a 403 error", err)
	}
	if err := bob.Post("lobby", "nope"); err == nil || !strings.Contains(err.Error(), "403") {
		t.Errorf("Post before joining = %v, want a 403 error", err)
	}

	if _, _, err := alice.Join("lobby"); err != nil {
		t.Fatalf("alice join: %v", err)
	}
	if err := alice.SetTopic("lobby", "Door games tonight"); err != nil {
		t.Fatalf("SetTopic: %v", err)
	}
	topic := awaitEvent[protocol.ChatTopicPayload](t, events, protocol.EventChatTopic, nil)
	if topic != (protocol.ChatTopicPayload{Room: "lobby", Topic: "Door games tonight", SetBy: "alice"}) {
		t.Errorf("hub announced topic %+v", topic)
	}
	rooms, err := bob.Rooms()
	if err != nil {
		t.Fatalf("Rooms: %v", err)
	}
	if len(rooms) != 1 || rooms[0] != (chat.RoomInfo{Name: "lobby", Topic: "Door games tonight", UserCount: 1}) {
		t.Errorf("Rooms = %+v", rooms)
	}

	bobNode := bobLeaf.cfg.Keystore.NodeID()
	if err := alice.Private("bob", bobNode, "psst"); err != nil {
		t.Fatalf("Private: %v", err)
	}
	private := awaitEvent[protocol.ChatMsgPayload](t, events, protocol.EventChatPrivate, nil)
	if private.FromHandle != "alice" || private.ToHandle != "bob" || private.ToNode != bobNode || private.Text != "psst" {
		t.Errorf("hub relayed private message %+v", private)
	}
	// Private messages to a node the hub does not know are refused.
	if err := bob.Private("nobody", "0000000000000000", "x"); err == nil || !strings.Contains(err.Error(), "404") {
		t.Errorf("Private to unknown node = %v, want a 404 error", err)
	}

	if err := alice.Leave("lobby"); err != nil {
		t.Fatalf("Leave: %v", err)
	}
	left := awaitEvent[protocol.ChatLeavePayload](t, events, protocol.EventChatLeave, nil)
	if left != (protocol.ChatLeavePayload{Room: "lobby", Handle: "alice", BBS: "alicebbs"}) {
		t.Errorf("hub announced leave %+v", left)
	}
	if alice.currentRoom != "" {
		t.Errorf("current room after Leave = %q, want empty", alice.currentRoom)
	}
	if rooms, err := bob.Rooms(); err != nil || len(rooms) != 0 {
		t.Errorf("Rooms after last member left = %+v, %v; want none", rooms, err)
	}
}

// Users on the same BBS each have their own chat allowance at the hub, and
// each message is credited to the user who sent it.
func TestChatSession_UsersOnOneBBS(t *testing.T) {
	ts, _, _ := newTestHub(t, true)
	board := subscribedLeaf(t, ts, "boardbbs")
	events := watchEvents(t, subscribedLeaf(t, ts, "observer"))

	alice := board.NewChatSession("alice")
	defer alice.Close()
	bob := board.NewChatSession("bob")
	defer bob.Close()
	for _, s := range []*ChatSession{alice, bob} {
		if _, _, err := s.Join("lobby"); err != nil {
			t.Fatalf("%s join: %v", s.handle, err)
		}
	}

	if err := alice.Post("lobby", "hi from alice"); err != nil {
		t.Fatalf("alice post: %v", err)
	}
	if err := bob.Post("lobby", "hi from bob"); err != nil {
		t.Fatalf("bob post straight after alice: %v", err)
	}
	if err := alice.Private("bob", board.cfg.Keystore.NodeID(), "psst"); err != nil {
		t.Fatalf("alice private straight after her post: %v", err)
	}
	if err := alice.Post("lobby", "too fast"); err == nil || !strings.Contains(err.Error(), "429") {
		t.Errorf("alice back-to-back post = %v, want a 429 error", err)
	}

	for _, want := range []string{"alice", "bob"} {
		msg := awaitEvent[protocol.ChatMsgPayload](t, events, protocol.EventChatMessage, nil)
		if msg.FromHandle != want || msg.Text != "hi from "+want {
			t.Errorf("hub broadcast %+v, want a post from %s", msg, want)
		}
	}
	private := awaitEvent[protocol.ChatMsgPayload](t, events, protocol.EventChatPrivate, nil)
	if private.FromHandle != "alice" || private.ToHandle != "bob" {
		t.Errorf("hub relayed private message %+v, want alice to bob", private)
	}

	if err := bob.SetTopic("lobby", "bob's room now"); err != nil {
		t.Fatalf("SetTopic: %v", err)
	}
	if topic := awaitEvent[protocol.ChatTopicPayload](t, events, protocol.EventChatTopic, nil); topic.SetBy != "bob" {
		t.Errorf("topic set by %q, want bob", topic.SetBy)
	}
}

func TestChatSession_ReceivesHubEventsOverSSE(t *testing.T) {
	ts, _, _ := newTestHub(t, true)
	aliceLeaf := subscribedLeaf(t, ts, "alicebbs")
	bobLeaf := subscribedLeaf(t, ts, "bobbbs")

	alice := aliceLeaf.NewChatSession("alice")
	if _, _, err := alice.Join("lobby"); err != nil {
		t.Fatalf("alice join: %v", err)
	}
	// Start the stream only after alice has joined, so her room is settled
	// before events are routed to her session.
	watchEvents(t, aliceLeaf)

	bob := bobLeaf.NewChatSession("bob")
	defer bob.Close()
	if _, _, err := bob.Join("lobby"); err != nil {
		t.Fatalf("bob join: %v", err)
	}
	join := awaitChat(t, alice, chat.TypeJoin)
	if join.Join == nil || *join.Join != (chat.ChatJoin{Room: "lobby", Handle: "bob", BBS: "bobbbs"}) {
		t.Errorf("join event = %+v", join.Join)
	}
	if users := alice.Users(); len(users) != 2 || users[1] != "bob" {
		t.Errorf("alice.Users() after bob joined = %v, want [alice bob]", users)
	}

	if err := bob.Post("lobby", "hi alice"); err != nil {
		t.Fatalf("bob post: %v", err)
	}
	msg := awaitChat(t, alice, chat.TypeMessage)
	if msg.Message == nil || msg.Message.Text != "hi alice" || msg.Message.Handle != "bob" ||
		msg.Message.Room != "lobby" || msg.Message.BBS != "bobbbs" || msg.Message.Timestamp.IsZero() {
		t.Errorf("message event = %+v", msg.Message)
	}

	if err := bob.Leave("lobby"); err != nil {
		t.Fatalf("bob leave: %v", err)
	}
	leave := awaitChat(t, alice, chat.TypeLeave)
	if leave.Leave == nil || leave.Leave.Handle != "bob" {
		t.Errorf("leave event = %+v", leave.Leave)
	}
	if users := alice.Users(); len(users) != 1 || users[0] != "alice" {
		t.Errorf("alice.Users() after bob left = %v, want [alice]", users)
	}

	// Close ends the event stream and unregisters the session; it is idempotent.
	if err := alice.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := alice.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}
	for range alice.Events() {
		// drain until closed
	}
	aliceLeaf.chatSessions.mu.RLock()
	_, stillRegistered := aliceLeaf.chatSessions.sessions["alice"]
	aliceLeaf.chatSessions.mu.RUnlock()
	if stillRegistered {
		t.Error("closed session is still registered on the leaf")
	}
}

// TestChatSession_JoinLeaveWhileStreaming changes rooms while the leaf's SSE
// loop is dispatching the resulting chat events. Under -race it catches
// dispatch reading currentRoom without the session lock (#517).
func TestChatSession_JoinLeaveWhileStreaming(t *testing.T) {
	ts, _, _ := newTestHub(t, true)
	aliceLeaf := subscribedLeaf(t, ts, "alicebbs")
	bobLeaf := subscribedLeaf(t, ts, "bobbbs")
	watchEvents(t, aliceLeaf)

	alice := aliceLeaf.NewChatSession("alice")
	defer alice.Close()
	bob := bobLeaf.NewChatSession("bob")
	defer bob.Close()

	// Bob's joins and leaves keep chat events flowing to alice's stream
	// while she changes room herself.
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 20; i++ {
			if _, _, err := bob.Join("lobby"); err != nil {
				t.Errorf("bob join: %v", err)
				return
			}
			if err := bob.Leave("lobby"); err != nil {
				t.Errorf("bob leave: %v", err)
				return
			}
		}
	}()
	for i := 0; i < 20; i++ {
		if _, _, err := alice.Join("lobby"); err != nil {
			t.Fatalf("alice join: %v", err)
		}
		if err := alice.Leave("lobby"); err != nil {
			t.Fatalf("alice leave: %v", err)
		}
	}
	<-done
}

func TestChatSession_HubErrors(t *testing.T) {
	t.Run("join rejected by hub", func(t *testing.T) {
		l, _ := setupLeaf(t, cannedHub(t, 500, `{"error":"boom"}`).URL, &mockJAMWriter{})
		s := l.NewChatSession("alice")
		if _, _, err := s.Join("lobby"); err == nil || !strings.Contains(err.Error(), "500") {
			t.Errorf("Join = %v, want a status 500 error", err)
		}
		if s.currentRoom != "" {
			t.Errorf("failed join set current room to %q", s.currentRoom)
		}
		if _, err := s.Rooms(); err == nil {
			t.Error("Rooms should fail on a 500")
		}
		if _, err := s.History("lobby", 5); err == nil {
			t.Error("History should fail on a 500")
		}
	})

	t.Run("unparseable replies", func(t *testing.T) {
		l, _ := setupLeaf(t, cannedHub(t, 200, `<html>not json</html>`).URL, &mockJAMWriter{})
		s := l.NewChatSession("alice")
		if _, _, err := s.Join("lobby"); err == nil {
			t.Error("Join should fail on a non-JSON reply")
		}
		if s.currentRoom != "" {
			t.Errorf("failed join set current room to %q", s.currentRoom)
		}
		if _, err := s.Rooms(); err == nil {
			t.Error("Rooms should fail on a non-JSON reply")
		}
		if _, err := s.History("lobby", 5); err == nil {
			t.Error("History should fail on a non-JSON reply")
		}
	})

	t.Run("hub unreachable", func(t *testing.T) {
		l, _ := setupLeaf(t, deadHubURL(t), &mockJAMWriter{})
		s := l.NewChatSession("alice")
		s.currentRoom = "lobby"
		if _, _, err := s.Join("other"); err == nil {
			t.Error("Join should fail when the hub is down")
		}
		if err := s.Leave("lobby"); err == nil {
			t.Error("Leave should fail when the hub is down")
		}
		// A leave the hub never accepted must not clear the current room.
		if s.currentRoom != "lobby" {
			t.Errorf("current room after failed Leave = %q, want lobby", s.currentRoom)
		}
		if _, err := s.Rooms(); err == nil {
			t.Error("Rooms should fail when the hub is down")
		}
	})
}

// newRoomSession registers a session for handle that is already in room.
func newRoomSession(l *Leaf, handle, room string, users ...string) *ChatSession {
	s := l.NewChatSession(handle)
	s.currentRoom = room
	s.currentUsers = users
	return s
}

func chatEvent(t *testing.T, eventType string, payload any) protocol.Event {
	t.Helper()
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal %s payload: %v", eventType, err)
	}
	return protocol.Event{Type: eventType, Data: data}
}

// noEvent fails if the session has an event waiting. dispatch delivers
// synchronously, so an empty channel right after it means nothing was sent.
func noEvent(t *testing.T, s *ChatSession, why string) {
	t.Helper()
	select {
	case ev := <-s.events:
		t.Errorf("%s: unexpected event %+v", why, ev)
	default:
	}
}

func TestDispatch_JoinLeaveTopicUpdateSession(t *testing.T) {
	l, _ := setupLeaf(t, "http://hub.invalid", &mockJAMWriter{})
	alice := newRoomSession(l, "alice", "lobby", "alice")
	other := newRoomSession(l, "dave", "offtopic", "dave")
	reg := l.chatSessions

	reg.dispatch(chatEvent(t, protocol.EventChatJoin, protocol.ChatJoinPayload{Room: "lobby", Handle: "bob", BBS: "B"}))
	ev := <-alice.events
	if ev.Type != chat.TypeJoin || *ev.Join != (chat.ChatJoin{Room: "lobby", Handle: "bob", BBS: "B"}) {
		t.Errorf("join event = %+v", ev)
	}
	if users := alice.Users(); len(users) != 2 || users[1] != "bob" {
		t.Errorf("users after join = %v", users)
	}

	reg.dispatch(chatEvent(t, protocol.EventChatTopic, protocol.ChatTopicPayload{Room: "lobby", Topic: "T", SetBy: "bob"}))
	ev = <-alice.events
	if ev.Type != chat.TypeTopic || *ev.Topic != (chat.ChatTopic{Room: "lobby", Topic: "T", SetBy: "bob"}) {
		t.Errorf("topic event = %+v", ev)
	}

	reg.dispatch(chatEvent(t, protocol.EventChatLeave, protocol.ChatLeavePayload{Room: "lobby", Handle: "bob", BBS: "B"}))
	ev = <-alice.events
	if ev.Type != chat.TypeLeave || *ev.Leave != (chat.ChatLeave{Room: "lobby", Handle: "bob", BBS: "B"}) {
		t.Errorf("leave event = %+v", ev)
	}
	if users := alice.Users(); len(users) != 1 || users[0] != "alice" {
		t.Errorf("users after leave = %v", users)
	}

	// Users returns a copy: changing it must not alter the session's list.
	alice.Users()[0] = "mallory"
	if users := alice.Users(); users[0] != "alice" {
		t.Errorf("Users() exposed internal state: %v", users)
	}

	noEvent(t, other, "session in another room")
	if users := other.Users(); len(users) != 1 {
		t.Errorf("other room's users changed: %v", users)
	}
}

// TestDispatch_JoinAndLeaveCountMemberships covers the hub's broadcast of a
// join arriving after Join has stored a user list that already counts the
// joiner (#519), without hiding other BBSes' users who share a handle: the
// hub lists one entry per membership.
func TestDispatch_JoinAndLeaveCountMemberships(t *testing.T) {
	const self = "<this leaf>"
	for _, tc := range []struct {
		name      string
		users     []string // the session's list before the event
		leave     bool     // a chat_leave rather than a chat_join
		node      string   // the join's node field; self means this leaf
		wantUsers []string
	}{
		{"echo of our own join is not added again", []string{"alice", "sysop"}, false, self, []string{"alice", "sysop"}},
		{"same handle from another BBS is added", []string{"alice", "sysop"}, false, "node-b", []string{"alice", "sysop", "sysop"}},
		{"join from a hub without the node field is added", []string{"alice", "sysop"}, false, "", []string{"alice", "sysop", "sysop"}},
		{"leave of a shared handle removes one entry", []string{"alice", "sysop", "sysop"}, true, "", []string{"alice", "sysop"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			l, _ := setupLeaf(t, "http://hub.invalid", &mockJAMWriter{})
			sess := newRoomSession(l, "sysop", "lobby", tc.users...)
			node := tc.node
			if node == self {
				node = l.chatSessions.nodeID
			}
			ev := chatEvent(t, protocol.EventChatJoin, protocol.ChatJoinPayload{Room: "lobby", Handle: "sysop", BBS: "X", Node: node})
			wantType := chat.TypeJoin
			if tc.leave {
				ev = chatEvent(t, protocol.EventChatLeave, protocol.ChatLeavePayload{Room: "lobby", Handle: "sysop", BBS: "X"})
				wantType = chat.TypeLeave
			}

			l.chatSessions.dispatch(ev)

			// The event is still delivered, so the UI can announce it.
			if got := <-sess.events; got.Type != wantType {
				t.Errorf("event = %+v, want type %v", got, wantType)
			}
			if got := sess.Users(); strings.Join(got, ",") != strings.Join(tc.wantUsers, ",") {
				t.Errorf("users = %v, want %v", got, tc.wantUsers)
			}
		})
	}
}

func TestDispatch_DropsMalformedAndUnknownEvents(t *testing.T) {
	l, _ := setupLeaf(t, "http://hub.invalid", &mockJAMWriter{})
	alice := newRoomSession(l, "alice", "lobby", "alice")
	reg := l.chatSessions

	for _, eventType := range []string{
		protocol.EventChatMessage, protocol.EventChatJoin, protocol.EventChatLeave,
		protocol.EventChatTopic, protocol.EventChatPrivate,
	} {
		// Not JSON at all: dropped before any session is chosen.
		reg.dispatch(protocol.Event{Type: eventType, Data: json.RawMessage(`{not json`)})
		noEvent(t, alice, eventType+" with invalid JSON")

		// Routable, but the payload's fields have the wrong types.
		alice.deliver(protocol.Event{Type: eventType, Data: json.RawMessage(`{"room":"lobby","handle":7,"text":7,"topic":7}`)})
		noEvent(t, alice, eventType+" with mistyped fields")
	}

	// Non-chat events are not the registry's business.
	reg.dispatch(chatEvent(t, protocol.EventLogon, protocol.LogonPayload{Handle: "alice"}))
	alice.deliver(chatEvent(t, protocol.EventLogon, protocol.LogonPayload{Handle: "alice"}))
	noEvent(t, alice, "logon event")

	if users := alice.Users(); len(users) != 1 || users[0] != "alice" {
		t.Errorf("malformed events changed the user list: %v", users)
	}
}

func TestSession_FullBufferAndClosedSessionDropEvents(t *testing.T) {
	l, _ := setupLeaf(t, "http://hub.invalid", &mockJAMWriter{})
	alice := newRoomSession(l, "alice", "lobby")
	reg := l.chatSessions
	msg := chatEvent(t, protocol.EventChatMessage, protocol.ChatMsgPayload{Room: "lobby", FromHandle: "bob", Text: "x"})

	// Delivery never blocks: events beyond the buffer are dropped.
	for i := 0; i < cap(alice.events)+5; i++ {
		reg.dispatch(msg)
	}
	if len(alice.events) != cap(alice.events) {
		t.Errorf("buffered %d events, want a full buffer of %d", len(alice.events), cap(alice.events))
	}
	reg.notifyReconnect() // full buffer: must not block either

	// A closed session that is somehow still routed to must not panic.
	if err := alice.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	alice.deliver(msg)
	reg.register(alice)
	reg.notifyReconnect()
	reg.deregister("alice")
}

func TestNotifyReconnect_TellsEverySession(t *testing.T) {
	l, _ := setupLeaf(t, "http://hub.invalid", &mockJAMWriter{})
	alice := newRoomSession(l, "alice", "lobby")
	bob := newRoomSession(l, "bob", "")

	l.chatSessions.notifyReconnect()

	for _, s := range []*ChatSession{alice, bob} {
		select {
		case ev := <-s.events:
			if ev.Type != chat.TypeSystem || !ev.Reconnect || ev.Text == "" {
				t.Errorf("%s got %+v, want a system reconnect event", s.handle, ev)
			}
		default:
			t.Errorf("%s was not told about the reconnect", s.handle)
		}
	}
}

// A join the hub accepted but whose reply cannot be read is undone with a
// leave, so the error means the caller is not in the room (#538). The
// session's current room is left as it was.
func TestChatSession_JoinUndoneWhenReplyUnreadable(t *testing.T) {
	var mu sync.Mutex
	var leaves []string
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v3net/v1/testnet/chat/rooms/join":
			_, _ = w.Write([]byte("not json"))
		case "/v3net/v1/testnet/chat/rooms/leave":
			var req protocol.ChatLeaveRequest
			_ = json.NewDecoder(r.Body).Decode(&req)
			mu.Lock()
			leaves = append(leaves, req.Room+"/"+req.Handle)
			mu.Unlock()
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer hub.Close()

	l, _ := setupLeaf(t, hub.URL, &mockJAMWriter{})
	sess := newRoomSession(l, "alice", "lobby", "alice")

	if _, _, err := sess.Join("den"); err == nil {
		t.Fatal("Join succeeded on an unreadable reply")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(leaves) != 1 || leaves[0] != "den/alice" {
		t.Errorf("leaves sent = %v, want [den/alice]", leaves)
	}
	if room := sess.room(); room != "lobby" {
		t.Errorf("current room = %q, want lobby unchanged", room)
	}
}
