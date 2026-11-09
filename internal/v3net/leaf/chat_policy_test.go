package leaf

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/chat"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

// With the sysop's chat switch off, a session can neither join nor speak,
// whatever the hub would allow; the hub is never asked.
func TestChatRefusedWhenNotOffered(t *testing.T) {
	ts, _, _ := newTestHub(t, true)
	l := subscribedLeaf(t, ts, "alicebbs")
	alice := l.NewChatSession("alice")
	defer alice.Close()
	if _, _, err := alice.Join("lobby"); err != nil {
		t.Fatalf("join with chat offered: %v", err)
	}

	// Chat does not need Central: only its own switch turns it off.
	l.cfg.CentralEnabled = false
	if st := l.ChatStatus(); !st.Enabled {
		t.Error("status says chat is off with Central off")
	}
	if err := alice.Post("lobby", "hello"); err != nil {
		t.Errorf("post with Central off: %v", err)
	}
	for name, off := range map[string]func(){
		"chat off": func() { l.cfg.CentralChat = false },
	} {
		t.Run(name, func(t *testing.T) {
			offerChat(l)
			off()
			defer offerChat(l)
			if st := l.ChatStatus(); st.Enabled {
				t.Error("status says chat is enabled")
			}
			for what, err := range map[string]error{
				"post":    alice.Post("lobby", "hello"),
				"private": alice.Private("bob", "", "psst"),
				"topic":   alice.SetTopic("lobby", "t"),
			} {
				if !errors.Is(err, ErrChatDisabled) {
					t.Errorf("%s = %v, want ErrChatDisabled", what, err)
				}
			}
			if _, _, err := alice.Join("den"); !errors.Is(err, ErrChatDisabled) {
				t.Errorf("join = %v, want ErrChatDisabled", err)
			}
			// Rooms are public and still readable, for the picker.
			if _, err := alice.Rooms(); err != nil {
				t.Errorf("rooms = %v", err)
			}
		})
	}
}

// ChatStatus follows the hub's feature list as the other features do.
func TestChatStatusFollowsHubFeatures(t *testing.T) {
	ts, _, _ := newTestHub(t, true)
	l := subscribedLeaf(t, ts, "alicebbs")
	if st := l.ChatStatus(); !st.Enabled || st.SupportKnown || st.Supported {
		t.Errorf("before /info: %+v", st)
	}
	if err := l.fetchHubFeatures(context.Background()); err != nil {
		t.Fatal(err)
	}
	if st := l.ChatStatus(); !st.Enabled || !st.SupportKnown || !st.Supported {
		t.Errorf("after /info from a hub that serves chat: %+v", st)
	}
	l.noteHubFeatures([]string{protocol.FeatureCentralDirectory})
	if st := l.ChatStatus(); st.Supported {
		t.Errorf("hub without chat: %+v", st)
	}
}

// CloseChatSessions tells every session why and ends it: the reason is the
// last event, the channel then closes, and the session is closed for
// good. Closing twice is harmless.
func TestCloseChatSessionsTellsAndEnds(t *testing.T) {
	ts, _, _ := newTestHub(t, true)
	l := subscribedLeaf(t, ts, "alicebbs")
	alice := l.NewChatSession("alice")
	bob := l.NewChatSession("bob")
	if _, _, err := alice.Join("lobby"); err != nil {
		t.Fatal(err)
	}

	l.CloseChatSessions("chat is over")
	for name, s := range map[string]*ChatSession{"alice": alice, "bob": bob} {
		var last chat.ChatEvent
		var got int
		timeout := time.After(2 * time.Second)
	drain:
		for {
			select {
			case ev, ok := <-s.Events():
				if !ok {
					break drain
				}
				last, got = ev, got+1
			case <-timeout:
				t.Fatalf("%s: events channel never closed", name)
			}
		}
		if got == 0 || last.Type != chat.TypeSystem || last.Text != "chat is over" {
			t.Errorf("%s: last event %+v (of %d), want the reason as a system message", name, last, got)
		}
		if err := s.Post("lobby", "x"); err == nil {
			t.Errorf("%s: post after close succeeded", name)
		}
	}
	l.CloseChatSessions("again")
	if err := alice.Close(); err != nil {
		t.Errorf("close after CloseChatSessions: %v", err)
	}

	// The leaf is retired for chat: a session a picker makes from it
	// afterwards is closed as it is made, and says why.
	late := l.NewChatSession("carol")
	if _, _, err := late.Join("lobby"); !errors.Is(err, ErrChatEnded) {
		t.Errorf("join on a retired leaf = %v, want ErrChatEnded", err)
	}
	if err := late.Private("alice", "", "hi"); !errors.Is(err, ErrChatEnded) {
		t.Errorf("private on a retired leaf = %v, want ErrChatEnded", err)
	}
	if ev, ok := <-late.Events(); !ok || ev.Text != ErrChatEnded.Error() {
		t.Errorf("late session's first event = %+v ok=%v, want the reason", ev, ok)
	}
	if _, ok := <-late.Events(); ok {
		t.Error("late session's channel still open")
	}
}

// A session whose buffer is full still gets the reason as its last event:
// the oldest unread event makes room for it.
func TestCloseChatSessionsReasonFitsFullBuffer(t *testing.T) {
	ts, _, _ := newTestHub(t, true)
	l := subscribedLeaf(t, ts, "alicebbs")
	alice := l.NewChatSession("alice")
	for i := 0; i < cap(alice.events); i++ {
		alice.events <- chat.ChatEvent{Type: chat.TypeSystem, Text: "filler"}
	}
	l.CloseChatSessions("chat is over")
	var last chat.ChatEvent
	n := 0
	for ev := range alice.Events() {
		last, n = ev, n+1
	}
	if n != cap(alice.events) || last.Text != "chat is over" {
		t.Errorf("got %d events ending %q, want a full buffer ending with the reason", n, last.Text)
	}
}

// A closed session refuses a private message as it does a post.
func TestChatPrivateRefusedOnClosedSession(t *testing.T) {
	ts, _, _ := newTestHub(t, true)
	alice := subscribedLeaf(t, ts, "alicebbs").NewChatSession("alice")
	alice.Close()
	if err := alice.Private("bob", "", "hi"); err == nil {
		t.Error("private on a closed session succeeded")
	}
}

// A session on a leaf that a reload has stopped tells the hub nothing when
// it leaves. The replacement leaf has the same node, and the hub keeps one
// membership per handle and node, so the old session's leave would take
// the replacement's caller with that handle out of the room, and their next
// post would be refused.
func TestRetiredSessionLeaveKeepsReplacementMembership(t *testing.T) {
	ts, _, _ := newTestHub(t, true)
	old := subscribedLeaf(t, ts, "alicebbs")
	alice := old.NewChatSession("alice")
	if _, _, err := alice.Join("lobby"); err != nil {
		t.Fatal(err)
	}
	old.CloseChatSessions("reload")

	// The replacement leaf runs on the same key, so it is the same node.
	next := offerChat(New(old.cfg))
	if err := next.subscribe(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(next.Close)
	again := next.NewChatSession("alice")
	defer again.Close()
	if _, _, err := again.Join("lobby"); err != nil {
		t.Fatal(err)
	}

	if err := alice.Leave("lobby"); err != nil {
		t.Errorf("leave on a retired leaf = %v, want nil", err)
	}
	if err := again.Post("lobby", "still here"); err != nil {
		t.Errorf("post on the replacement leaf after the old session left: %v", err)
	}
}
