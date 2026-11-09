package leaf

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/chat"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

// Cancel a real authenticated stream, wait for hub cleanup, then reconnect.
// Successful posting must mean the membership was actually restored.
func TestChatReconnectRestoresMembership(t *testing.T) {
	ts, _, _ := newTestHub(t, true)
	board := subscribedLeaf(t, ts, "boardbbs")
	peer := subscribedLeaf(t, ts, "peerbbs")
	if err := board.checkHubKey(context.Background()); err != nil {
		t.Fatal(err)
	}
	alice := board.NewChatSession("alice")
	defer alice.Close()
	aliceSecond := board.NewChatSession("alice")
	defer aliceSecond.Close()
	carol := board.NewChatSession("carol")
	defer carol.Close()
	bob := peer.NewChatSession("bob")
	defer bob.Close()
	for _, s := range []*ChatSession{alice, aliceSecond, carol, bob} {
		if _, _, err := s.Join("lobby"); err != nil {
			t.Fatal(err)
		}
	}
	side := board.NewChatSession("alice")
	defer side.Close()
	if _, _, err := side.Join("side"); err != nil {
		t.Fatal(err)
	}
	if err := bob.SetTopic("lobby", "current topic"); err != nil {
		t.Fatal(err)
	}
	counter := &chatJoinCounter{next: http.DefaultTransport}
	board.client.Transport = counter
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- board.connectSSE(ctx, false) }()
	waitChatConnected(t, board)
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("stream did not stop")
	}
	// Hub cleanup precedes the peer's view of the departure.
	deadline := time.Now().Add(5 * time.Second)
	for {
		rooms, err := bob.Rooms()
		if err != nil {
			t.Fatal(err)
		}
		if len(rooms) == 1 && rooms[0].UserCount == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("hub retained disconnected membership")
		}
		time.Sleep(time.Millisecond)
	}
	if err := bob.Post("lobby", "sent while offline"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	go func() { done <- board.connectSSE(ctx, true) }()
	defer func() {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("reconnected stream did not stop")
		}
	}()
	for _, s := range []*ChatSession{alice, aliceSecond, carol} {
		for {
			ev := awaitChat(t, s, chat.TypeSystem)
			if ev.Reconnect {
				if ev.Topic == nil || ev.Topic.Topic != "current topic" {
					t.Fatalf("refreshed topic: %+v", ev.Topic)
				}
				break
			}
		}
	}
	for {
		ev := awaitChat(t, side, chat.TypeSystem)
		if ev.Reconnect {
			if ev.Topic == nil || ev.Topic.Room != "side" || ev.Topic.Topic != "" {
				t.Fatalf("second room topic: %+v", ev.Topic)
			}
			break
		}
	}
	if users := side.Users(); len(users) != 1 || users[0].Handle != "alice" {
		t.Fatalf("second room users: %v", users)
	}
	if err := alice.Post("lobby", "after reconnect"); err != nil {
		t.Fatalf("post after reconnect: %v", err)
	}
	for _, s := range []*ChatSession{alice, aliceSecond} {
		users := s.Users()
		if len(users) != 3 {
			t.Errorf("refreshed users: %v", users)
		}
	}
	if counter.joins.Load() != 3 {
		t.Fatalf("rejoin requests: %d, want one per unique room and handle", counter.joins.Load())
	}
	// This live post follows all rejoin events in the stream. No history or
	// synthetic local join announcements may be replayed ahead of it.
	for {
		select {
		case ev := <-aliceSecond.Events():
			if ev.Type == chat.TypeJoin {
				t.Fatalf("duplicate recovery announcement: %+v", ev)
			}
			if ev.Type == chat.TypeMessage {
				if ev.Message == nil || ev.Message.Text != "after reconnect" {
					t.Fatalf("replayed history: %+v", ev)
				}
				return
			}
		case <-time.After(5 * time.Second):
			t.Fatal("live post not delivered after recovery")
		}
	}
}

type chatJoinCounter struct {
	next  http.RoundTripper
	joins atomic.Int32
}

func (c *chatJoinCounter) RoundTrip(req *http.Request) (*http.Response, error) {
	if strings.HasSuffix(req.URL.Path, "/chat/rooms/join") {
		c.joins.Add(1)
	}
	return c.next.RoundTrip(req)
}

func waitChatConnected(t *testing.T, l *Leaf) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !l.sseConnected.Load() {
		if time.Now().After(deadline) {
			t.Fatal("stream did not connect")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestChatRecoveryFailureStaysUnreadyAndRetries(t *testing.T) {
	var attempts atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if attempts.Add(1) == 1 {
			http.Error(w, "try again", http.StatusServiceUnavailable)
			return
		}
		json.NewEncoder(w).Encode(protocol.ChatJoinResponse{Users: []string{"alice"}, Rooms: []protocol.ProtoChatRoomInfo{{Name: "lobby", Topic: "restored"}}})
	}))
	defer ts.Close()
	l, _ := setupLeaf(t, ts.URL, &mockJAMWriter{})
	offerChat(l)
	alice := newRoomSession(l, "alice", "lobby")
	defer alice.Close()
	if err := l.recoverChat(context.Background()); err == nil {
		t.Fatal("failed recovery was accepted")
	}
	if err := alice.Post("lobby", "not ready"); err == nil || !strings.Contains(err.Error(), "reconnecting") {
		t.Fatalf("post during failed recovery: %v", err)
	}
	if err := alice.SetTopic("lobby", "not ready"); err == nil {
		t.Fatal("topic accepted during failed recovery")
	}
	ev := awaitChat(t, alice, chat.TypeSystem)
	if ev.Reconnect || !strings.Contains(ev.Text, "failed") {
		t.Fatalf("failure notice: %+v", ev)
	}
	if err := l.recoverChat(context.Background()); err != nil {
		t.Fatal(err)
	}
	ev = awaitChat(t, alice, chat.TypeSystem)
	if !ev.Reconnect || ev.Topic == nil || ev.Topic.Topic != "restored" {
		t.Fatalf("retry ready notice: %+v", ev)
	}
	if attempts.Load() != 2 {
		t.Fatalf("requests: %d", attempts.Load())
	}
}

func TestChatRecoveryAbandonedWhileRequestInFlight(t *testing.T) {
	for _, action := range []string{"close", "leave", "cancel"} {
		t.Run(action, func(t *testing.T) {
			started := make(chan struct{})
			release := make(chan struct{}, 1)
			var leaves atomic.Int32
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/leave") {
					leaves.Add(1)
					w.WriteHeader(http.StatusNoContent)
					return
				}
				close(started)
				select {
				case <-release:
				case <-r.Context().Done():
					return
				}
				json.NewEncoder(w).Encode(protocol.ChatJoinResponse{Users: []string{"alice"}})
			}))
			defer ts.Close()
			// Always unblock handlers even when a test assertion fails.
			defer close(release)
			l, _ := setupLeaf(t, ts.URL, &mockJAMWriter{})
			offerChat(l)
			alice := newRoomSession(l, "alice", "lobby")
			defer alice.Close()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			go func() { done <- l.recoverChat(ctx) }()
			select {
			case <-started:
			case <-time.After(5 * time.Second):
				t.Fatal("rejoin did not start")
			}
			var leaveDone chan error
			switch action {
			case "close":
				alice.Close()
			case "leave":
				leaveDone = make(chan error, 1)
				go func() { leaveDone <- alice.Leave("lobby") }()
				deadline := time.Now().Add(5 * time.Second)
				for alice.room() != "" {
					if time.Now().After(deadline) {
						t.Fatal("leave did not abandon recovery")
					}
					time.Sleep(time.Millisecond)
				}
			case "cancel":
				cancel()
			}
			// Allow the join response; subsequent recovery must recheck intent.
			release <- struct{}{}
			select {
			case err := <-done:
				if action == "cancel" && !errors.Is(err, context.Canceled) {
					t.Fatalf("canceled recovery: %v", err)
				}
				if action != "cancel" && err != nil {
					t.Fatal(err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("recovery did not finish")
			}
			if leaveDone != nil {
				select {
				case err := <-leaveDone:
					if err != nil {
						t.Fatal(err)
					}
				case <-time.After(5 * time.Second):
					t.Fatal("Leave blocked")
				}
			}
			if action != "cancel" && leaves.Load() == 0 {
				t.Fatal("abandoned join not undone")
			}
			if action == "cancel" && alice.chatReady() == nil {
				t.Fatal("canceled recovery marked ready")
			}
			if action == "close" && alice.room() != "lobby" {
				t.Fatal("closed session unexpectedly changed rooms")
			}
		})
	}
}

// Exercise the production reconnect/backoff loop and an actual hub, including
// a transient failure of the first restoration request.
func TestChatReconnectLoopRetriesRecovery(t *testing.T) {
	original := sseBackoffBase
	sseBackoffBase = 10 * time.Millisecond
	t.Cleanup(func() { sseBackoffBase = original })
	ts, _, _ := newTestHub(t, true)
	board := subscribedLeaf(t, ts, "boardbbs")
	if err := board.checkHubKey(context.Background()); err != nil {
		t.Fatal(err)
	}
	alice := board.NewChatSession("alice")
	defer alice.Close()
	if _, _, err := alice.Join("lobby"); err != nil {
		t.Fatal(err)
	}
	streams := &chatStreamObserver{first: make(chan *http.Response, 1)}
	board.sseClient.Transport = streams
	joins := &chatFailFirstRejoin{}
	board.client.Transport = joins
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); board.runSSE(ctx) }()
	defer func() {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("SSE loop did not stop")
		}
	}()
	select {
	case resp := <-streams.first:
		resp.Body.Close()
	case <-time.After(5 * time.Second):
		t.Fatal("first stream did not open")
	}
	failed := false
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	for {
		select {
		case ev := <-alice.Events():
			if strings.Contains(ev.Text, "recovery failed") {
				failed = true
			}
			if ev.Reconnect {
				if !failed {
					t.Fatal("ready without observing transient failure")
				}
				if joins.calls.Load() != 2 || streams.calls.Load() < 3 {
					t.Fatalf("join attempts %d; streams %d", joins.calls.Load(), streams.calls.Load())
				}
				if err := alice.Post("lobby", "recovered by loop"); err != nil {
					t.Fatal(err)
				}
				return
			}
		case <-deadline.C:
			t.Fatal("SSE loop did not restore chat after retry")
		}
	}
}

type chatStreamObserver struct {
	calls atomic.Int32
	first chan *http.Response
}

func (o *chatStreamObserver) RoundTrip(req *http.Request) (*http.Response, error) {
	n := o.calls.Add(1)
	resp, err := http.DefaultTransport.RoundTrip(req)
	if n == 1 && err == nil {
		o.first <- resp
	}
	return resp, err
}

type chatFailFirstRejoin struct{ calls atomic.Int32 }

func (f *chatFailFirstRejoin) RoundTrip(req *http.Request) (*http.Response, error) {
	if strings.HasSuffix(req.URL.Path, "/chat/rooms/join") && f.calls.Add(1) == 1 {
		return nil, errors.New("temporary rejoin failure")
	}
	return http.DefaultTransport.RoundTrip(req)
}

func TestChatRecoveryClosesEarlierGroupWhileLaterGroupWaits(t *testing.T) {
	first := make(chan string, 1)
	blocked := make(chan struct{})
	release := make(chan struct{}, 1)
	leaves := make(chan string, 2)
	var joins atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req protocol.ChatJoinRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/leave") {
			leaves <- req.Handle
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if joins.Add(1) == 1 {
			first <- req.Handle
		} else {
			close(blocked)
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
		}
		json.NewEncoder(w).Encode(protocol.ChatJoinResponse{Users: []string{req.Handle}})
	}))
	defer ts.Close()
	defer close(release)
	l, _ := setupLeaf(t, ts.URL, &mockJAMWriter{})
	offerChat(l)
	sessions := map[string]*ChatSession{"alice": newRoomSession(l, "alice", "lobby"), "bob": newRoomSession(l, "bob", "lobby")}
	for _, s := range sessions {
		defer s.Close()
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- l.recoverChat(ctx) }()
	var earlier string
	select {
	case earlier = <-first:
	case <-time.After(5 * time.Second):
		t.Fatal("first rejoin missing")
	}
	select {
	case <-blocked:
	case <-time.After(5 * time.Second):
		t.Fatal("second rejoin missing")
	}
	sessions[earlier].Close()
	// Complete the second request after the first session has closed.
	release <- struct{}{}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("recovery did not finish")
	}
	select {
	case handle := <-leaves:
		if handle != earlier {
			t.Fatalf("cleaned up %s, want %s", handle, earlier)
		}
	default:
		t.Fatal("earlier closed session retained membership")
	}
}

func TestChatRecoveryExistingMembershipDoesNotHideLaterJoin(t *testing.T) {
	ts, _, _ := newTestHub(t, true)
	board := subscribedLeaf(t, ts, "boardbbs")
	alice := board.NewChatSession("alice")
	defer alice.Close()
	bob := board.NewChatSession("bob")
	defer bob.Close()
	for _, s := range []*ChatSession{alice, bob} {
		if _, _, err := s.Join("lobby"); err != nil {
			t.Fatal(err)
		}
	}
	watchEvents(t, board)
	// Overlapping streams can reconnect while memberships still exist; the
	// hub emits no join echoes for these idempotent restoration requests.
	if err := board.recoverChat(context.Background()); err != nil {
		t.Fatal(err)
	}
	awaitChat(t, bob, chat.TypeSystem)
	if err := alice.Leave("lobby"); err != nil {
		t.Fatal(err)
	}
	awaitChat(t, bob, chat.TypeLeave)
	if _, _, err := alice.Join("lobby"); err != nil {
		t.Fatal(err)
	}
	ev := awaitChat(t, bob, chat.TypeJoin)
	if ev.Join == nil || ev.Join.Handle != "alice" {
		t.Fatalf("normal join hidden: %+v", ev)
	}
	if users := bob.Users(); len(users) != 2 {
		t.Fatalf("normal rejoin users: %v", users)
	}
}
