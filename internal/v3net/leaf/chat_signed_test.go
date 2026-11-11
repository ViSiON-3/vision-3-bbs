package leaf

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/chat"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

// rosterRoom is a network with a coordinator, Alpha and Beta on its roster
// and Gamma not, Beta holding the roster and watching its event stream.
type rosterRoom struct {
	coord, alpha, beta, gamma *Leaf
	betaEvents                <-chan protocol.Event
}

func newRosterRoom(t *testing.T) *rosterRoom {
	t.Helper()
	ts, h, hubKS := newTestHub(t, true)
	seedHubNAL(t, h, hubKS)
	r := &rosterRoom{coord: coordLeaf(t, ts, hubKS)}
	r.alpha = subscribedLeaf(t, ts, "Alpha")
	r.beta = subscribedLeaf(t, ts, "Beta")
	r.gamma = subscribedLeaf(t, ts, "Gamma")
	if _, err := r.coord.PublishRoster(context.Background(), 0, []protocol.CentralRosterMember{memberOf(r.alpha), memberOf(r.beta)}, false); err != nil {
		t.Fatal(err)
	}
	pinCoord(t, r.beta, hubKS)
	if err := r.beta.fetchRoster(context.Background()); err != nil {
		t.Fatal(err)
	}
	r.betaEvents = watchEvents(t, r.beta)
	return r
}

// join puts handle from l in the lobby.
func join(t *testing.T, l *Leaf, handle string) *ChatSession {
	t.Helper()
	s := l.NewChatSession(handle)
	t.Cleanup(func() { _ = s.Close() })
	if _, _, err := s.Join("lobby"); err != nil {
		t.Fatalf("%s joins: %v", handle, err)
	}
	return s
}

// lines collects the room lines bob sees within wait.
func lines(s *ChatSession, wait time.Duration) []string {
	var out []string
	deadline := time.After(wait)
	for {
		select {
		case ev := <-s.Events():
			if ev.Type == chat.TypeMessage {
				out = append(out, ev.Message.Handle+": "+ev.Message.Text)
			}
		case <-deadline:
			return out
		}
	}
}

// With a roster in force, a board shows room lines only from boards on
// it, signed, and the member list leaves the others out.
func TestSignedChatFilteredByRoster(t *testing.T) {
	r := newRosterRoom(t)
	gammaS := join(t, r.gamma, "Mallory")
	alphaS := join(t, r.alpha, "Alice")
	bob := join(t, r.beta, "Bob")

	for _, u := range bob.Users() {
		if u.Node == r.gamma.cfg.Keystore.NodeID() {
			t.Errorf("a board not on the roster is in the member list: %+v", u)
		}
	}
	if err := gammaS.Post("lobby", "from off the roster"); err != nil {
		t.Fatal(err)
	}
	if err := alphaS.Post("lobby", "hello from Alpha"); err != nil {
		t.Fatal(err)
	}
	got := lines(bob, 500*time.Millisecond)
	if len(got) != 1 || got[0] != "Alice: hello from Alpha" {
		t.Errorf("Bob saw %q, want Alice's line only", got)
	}
}

// An unsigned line, as a board from before signed chat sends, or a hub
// claiming a line from a board on the roster, is not shown while a roster
// is in force; a replayed signed line is shown once.
func TestSignedChatUnsignedAndReplayDropped(t *testing.T) {
	r := newRosterRoom(t)
	join(t, r.alpha, "Alice")
	bob := join(t, r.beta, "Bob")

	body, _ := json.Marshal(protocol.ChatPostRequest{Room: "lobby", Text: "unsigned", Handle: "Alice"})
	if err := r.alpha.signedPostCtx(context.Background(), fmt.Sprintf("/v3net/v1/%s/chat/rooms/post", r.alpha.cfg.Network), body); err != nil {
		t.Fatalf("unsigned post refused by the hub (it accepts older boards): %v", err)
	}
	if got := lines(bob, 400*time.Millisecond); len(got) != 0 {
		t.Errorf("Bob saw an unsigned line: %q", got)
	}

	// A signed line, delivered twice as a replaying hub would.
	signed, err := r.alpha.signChat(protocol.ChatSignedLine, "Alice", func(c *protocol.ChatSigned) { c.Room, c.Text = "lobby", "once" })
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(protocol.ChatMsgPayload{Room: "lobby", FromHandle: "Alice", FromNode: r.alpha.cfg.Keystore.NodeID(),
		Text: "once", Timestamp: time.Now().UTC().Format(time.RFC3339), Signed: signed})
	ev := protocol.Event{Type: protocol.EventChatMessage, Data: data}
	r.beta.chatSessions.dispatch(ev)
	r.beta.chatSessions.dispatch(ev)
	if got := lines(bob, 300*time.Millisecond); len(got) != 1 {
		t.Errorf("a replayed line was shown %d times", len(got))
	}

	// The hub claims Alice said something she did not sign.
	forged, _ := json.Marshal(protocol.ChatMsgPayload{Room: "lobby", FromHandle: "Alice", FromNode: r.alpha.cfg.Keystore.NodeID(),
		Text: "not what she said", Timestamp: time.Now().UTC().Format(time.RFC3339), Signed: signed})
	r.beta.chatSessions.dispatch(protocol.Event{Type: protocol.EventChatMessage, Data: forged})
	if got := lines(bob, 200*time.Millisecond); len(got) != 0 {
		t.Errorf("a line that does not match its signature was shown: %q", got)
	}
}

// Without a roster, chat is shown as before, signed or not.
func TestSignedChatNoRosterUnchanged(t *testing.T) {
	ts, _, _ := newTestHub(t, true)
	alpha := subscribedLeaf(t, ts, "Alpha")
	beta := subscribedLeaf(t, ts, "Beta")
	watchEvents(t, beta)
	join(t, alpha, "Alice")
	bob := join(t, beta, "Bob")
	body, _ := json.Marshal(protocol.ChatPostRequest{Room: "lobby", Text: "unsigned", Handle: "Alice"})
	if err := alpha.signedPostCtx(context.Background(), fmt.Sprintf("/v3net/v1/%s/chat/rooms/post", alpha.cfg.Network), body); err != nil {
		t.Fatal(err)
	}
	if got := lines(bob, 500*time.Millisecond); len(got) != 1 {
		t.Errorf("without a roster Bob saw %q, want the unsigned line", got)
	}
}

// Room history from the hub keeps only lines signed by boards on the
// roster.
func TestSignedChatHistoryFiltered(t *testing.T) {
	r := newRosterRoom(t)
	gammaS := join(t, r.gamma, "Mallory")
	alphaS := join(t, r.alpha, "Alice")
	_ = gammaS.Post("lobby", "gamma line")
	_ = alphaS.Post("lobby", "alpha line")
	bob := r.beta.NewChatSession("Bob")
	t.Cleanup(func() { _ = bob.Close() })
	_, history, err := bob.Join("lobby")
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 1 || history[0].Text != "alpha line" {
		t.Errorf("history %+v, want Alpha's line only", history)
	}
}

// A hub saying a message comes from this board is not believed on its
// word: unsigned, it is dropped like any other.
func TestSignedChatOwnBoardClaimChecked(t *testing.T) {
	r := newRosterRoom(t)
	join(t, r.beta, "Bob")
	carol := join(t, r.beta, "Carol")
	data, _ := json.Marshal(protocol.ChatMsgPayload{FromHandle: "Bob", FromNode: r.beta.cfg.Keystore.NodeID(),
		ToHandle: "Carol", ToNode: r.beta.cfg.Keystore.NodeID(), Text: "forged from home",
		Timestamp: time.Now().UTC().Format(time.RFC3339)})
	r.beta.chatSessions.dispatch(protocol.Event{Type: protocol.EventChatPrivate, Data: data})
	select {
	case ev := <-carol.Events():
		if ev.Type == chat.TypePrivate {
			t.Errorf("an unsigned private message claimed from this board was delivered: %+v", ev.Message)
		}
	case <-time.After(200 * time.Millisecond):
	}
}

// A private message signed for a caller on another board is not delivered
// here, whatever node the hub puts on it.
func TestSignedChatPrivateForAnotherBoard(t *testing.T) {
	r := newRosterRoom(t)
	bob := join(t, r.beta, "Bob")
	signed, err := r.alpha.signChat(protocol.ChatSignedPrivate, "Alice", func(c *protocol.ChatSigned) {
		c.ToHandle, c.ToNode, c.Text = "Bob", r.gamma.cfg.Keystore.NodeID(), "for Gamma's Bob"
	})
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(protocol.ChatMsgPayload{FromHandle: "Alice", FromNode: r.alpha.cfg.Keystore.NodeID(),
		ToHandle: "Bob", ToNode: r.beta.cfg.Keystore.NodeID(), Text: signed.Text,
		Timestamp: time.Now().UTC().Format(time.RFC3339), Signed: signed})
	r.beta.chatSessions.dispatch(protocol.Event{Type: protocol.EventChatPrivate, Data: data})
	select {
	case ev := <-bob.Events():
		if ev.Type == chat.TypePrivate {
			t.Errorf("a message signed for another board was delivered: %+v", ev.Message)
		}
	case <-time.After(200 * time.Millisecond):
	}
}

// The member list keeps this board's callers only where a session here is
// in the room: a hub cannot list a caller of this board who is not there.
func TestSignedChatLocalMembersChecked(t *testing.T) {
	r := newRosterRoom(t)
	me := r.beta.cfg.Keystore.NodeID()
	users := []chat.ChatUser{{Handle: "Bob", Node: me}, {Handle: "Ghost", Node: me}, {Handle: "Alice", Node: r.alpha.cfg.Keystore.NodeID()}}
	got := r.beta.chatAdmitsMembers(map[string]bool{"Bob": true}, users)
	if len(got) != 2 || got[0].Handle != "Bob" || got[1].Handle != "Alice" {
		t.Errorf("members %+v, want Bob and Alice", got)
	}
}

// A board rejoining after a reconnect signs the join, so boards holding a
// roster put its caller back in the member list.
func TestSignedChatRejoinSigned(t *testing.T) {
	r := newRosterRoom(t)
	alice := join(t, r.alpha, "Alice")
	bob := join(t, r.beta, "Bob")
	if err := alice.Leave("lobby"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.alpha.rejoinChat(context.Background(), chatMembership{room: "lobby", handle: "Alice"}); err != nil {
		t.Fatal(err)
	}
	deadline := time.After(3 * time.Second)
	for {
		select {
		case ev := <-bob.Events():
			if ev.Type == chat.TypeJoin && ev.Join.Handle == "Alice" {
				return
			}
		case <-deadline:
			t.Fatal("the rejoin never reached a board holding the roster")
		}
	}
}

// An ID is kept for as long as its action could pass as fresh, however
// many others arrive: when the memory is full of fresh IDs, a new action
// is refused rather than an old one forgotten; once they go stale there
// is room again.
func TestChatSeenKeepsFreshIDs(t *testing.T) {
	var c chatSeen
	now := time.Now()
	if !c.claim("n", "first", now, now) {
		t.Fatal("first claim refused")
	}
	for i := 1; i < chatSeenSize; i++ {
		if !c.claim("n", fmt.Sprintf("id%d", i), now, now) {
			t.Fatalf("claim %d refused", i)
		}
	}
	if c.claim("n", "overflow", now, now) {
		t.Error("a new action was taken with no room to remember it")
	}
	if c.claim("n", "first", now, now) {
		t.Error("the first action's replay was taken after the memory filled")
	}
	later := now.Add(protocol.MaxChatSignedAge + protocol.MaxCentralClockSkew + time.Second)
	if !c.claim("n", "after", later, later) {
		t.Error("no room once the old IDs went stale")
	}
}

// Posting to, and reading the history of, a room named as the caller typed
// it works as for its normalized name.
func TestSignedChatRoomNormalized(t *testing.T) {
	r := newRosterRoom(t)
	alice := join(t, r.alpha, "Alice")
	bob := join(t, r.beta, "Bob")
	if err := alice.Post("LOBBY", "loud room name"); err != nil {
		t.Fatalf("post to LOBBY: %v", err)
	}
	if got := lines(bob, 500*time.Millisecond); len(got) != 1 {
		t.Errorf("Bob saw %q", got)
	}
	hist, err := bob.History("LOBBY", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(hist) != 1 || hist[0].Text != "loud room name" {
		t.Errorf("history %+v, want the signed line", hist)
	}
}
