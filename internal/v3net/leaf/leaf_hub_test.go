package leaf

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

const (
	testUUID1 = "550e8400-e29b-41d4-a716-446655440001"
	testUUID2 = "550e8400-e29b-41d4-a716-446655440002"
	testUUID3 = "550e8400-e29b-41d4-a716-446655440003"
)

// chanJAMWriter hands each written message to the test over a channel.
type chanJAMWriter struct {
	written chan protocol.Message
}

func (w *chanJAMWriter) WriteMessage(msg protocol.Message) (int64, error) {
	w.written <- msg
	return 1, nil
}

// failingJAMWriter refuses the message with UUID failUUID and records the rest.
type failingJAMWriter struct {
	mockJAMWriter
	failUUID string
}

func (w *failingJAMWriter) WriteMessage(msg protocol.Message) (int64, error) {
	if msg.MsgUUID == w.failUUID {
		return 0, errors.New("message base is read-only")
	}
	return w.mockJAMWriter.WriteMessage(msg)
}

func TestSubscribe_RegistersWithHub(t *testing.T) {
	ts, h, hubKS := newTestHub(t, true)
	seedHubNAL(t, h, hubKS, "gen.general")

	l := subscribedLeaf(t, ts, "leafbbs", "gen.general")
	if l.HubURL() != ts.URL || l.Network() != "testnet" {
		t.Errorf("HubURL/Network = %q/%q", l.HubURL(), l.Network())
	}

	sub := h.Subscribers().Get(l.cfg.Keystore.NodeID(), "testnet")
	if sub == nil {
		t.Fatal("hub has no record of the leaf after subscribe")
	}
	if sub.BBSName != "leafbbs" || sub.BBSHost != "leafbbs.example.net" || sub.Status != "active" ||
		sub.PubKeyB64 != l.cfg.Keystore.PubKeyBase64() {
		t.Errorf("hub recorded subscriber %+v", sub)
	}

	// The requested area is live: the leaf can post to it straight away.
	if err := l.SendMessage(testMessage(testUUID1)); err != nil {
		t.Errorf("post to subscribed area: %v", err)
	}
	// Subscribing again is harmless.
	if err := l.subscribe(context.Background()); err != nil {
		t.Errorf("repeat subscribe: %v", err)
	}
}

func TestSubscribe_Failures(t *testing.T) {
	t.Run("hub requires manual approval", func(t *testing.T) {
		ts, _, _ := newTestHub(t, false)
		l, _ := setupLeaf(t, ts.URL, &mockJAMWriter{})
		err := l.subscribe(context.Background())
		if err == nil || !strings.Contains(err.Error(), `"pending"`) {
			t.Errorf("subscribe = %v, want an error naming the pending status", err)
		}
	})

	t.Run("network unknown to hub", func(t *testing.T) {
		ts, _, _ := newTestHub(t, true)
		l, _ := setupLeaf(t, ts.URL, &mockJAMWriter{})
		l.cfg.Network = "nonet"
		err := l.subscribe(context.Background())
		if err == nil || !strings.Contains(err.Error(), "404") || !strings.Contains(err.Error(), "unknown network") {
			t.Errorf("subscribe = %v, want a 404 carrying the hub's message", err)
		}
	})

	t.Run("unparseable reply", func(t *testing.T) {
		l, _ := setupLeaf(t, cannedHub(t, 200, `<html>`).URL, &mockJAMWriter{})
		if err := l.subscribe(context.Background()); err == nil {
			t.Error("subscribe should fail on a non-JSON reply")
		}
	})

	t.Run("hub unreachable", func(t *testing.T) {
		l, _ := setupLeaf(t, deadHubURL(t), &mockJAMWriter{})
		if err := l.subscribe(context.Background()); err == nil {
			t.Error("subscribe should fail when the hub is down")
		}
	})

	t.Run("malformed hub URL", func(t *testing.T) {
		l, _ := setupLeaf(t, "http://bad host", &mockJAMWriter{})
		if err := l.subscribe(context.Background()); err == nil {
			t.Error("subscribe should fail on an invalid hub URL")
		}
		if err := l.SendLogon("alice"); err == nil {
			t.Error("signed POST should fail on an invalid hub URL")
		}
		if _, err := l.get("/v3net/v1/testnet/info"); err == nil {
			t.Error("GET should fail on an invalid hub URL")
		}
	})
}

func TestPresence_LogonLogoffReachOtherNodes(t *testing.T) {
	ts, _, _ := newTestHub(t, true)
	l := subscribedLeaf(t, ts, "leafbbs")
	events := watchEvents(t, subscribedLeaf(t, ts, "observer"))
	ctx := context.Background()

	isAlice := func(p protocol.LogonPayload) bool { return p.Handle == "alice" }
	isBob := func(p protocol.LogonPayload) bool { return p.Handle == "bob" }

	if err := l.SendLogon("alice"); err != nil {
		t.Fatalf("SendLogon: %v", err)
	}
	logon := awaitEvent(t, events, protocol.EventLogon, isAlice)
	if logon.Node != "leafbbs.example.net" || logon.Timestamp == "" {
		t.Errorf("logon event = %+v", logon)
	}

	if err := l.SendLogoff("alice"); err != nil {
		t.Fatalf("SendLogoff: %v", err)
	}
	logoff := awaitEvent(t, events, protocol.EventLogoff, isAlice)
	if logoff.Node != "leafbbs.example.net" {
		t.Errorf("logoff event = %+v", logoff)
	}

	if err := l.SendLogonCtx(ctx, "bob"); err != nil {
		t.Fatalf("SendLogonCtx: %v", err)
	}
	awaitEvent(t, events, protocol.EventLogon, isBob)
	if err := l.SendLogoffCtx(ctx, "bob"); err != nil {
		t.Fatalf("SendLogoffCtx: %v", err)
	}
	awaitEvent(t, events, protocol.EventLogoff, isBob)

	// A cancelled context stops the request before it reaches the hub.
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if err := l.SendLogonCtx(cancelled, "carol"); !errors.Is(err, context.Canceled) {
		t.Errorf("SendLogonCtx with cancelled context = %v, want context.Canceled", err)
	}

	// A node the hub has never registered is refused.
	stranger, _ := setupLeaf(t, ts.URL, &mockJAMWriter{})
	if err := stranger.SendLogon("mallory"); err == nil || !strings.Contains(err.Error(), "401") {
		t.Errorf("SendLogon from unregistered node = %v, want a 401 error", err)
	}
}

func TestSendChat_JoinsLobbyAndPosts(t *testing.T) {
	ts, _, _ := newTestHub(t, true)
	l := subscribedLeaf(t, ts, "leafbbs")

	if err := l.SendChat("anyone there?", "alice"); err != nil {
		t.Fatalf("SendChat: %v", err)
	}
	s := l.NewChatSession("reader")
	defer s.Close()
	msgs, err := s.History("lobby", 10)
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if len(msgs) != 1 || msgs[0].Text != "anyone there?" || msgs[0].Handle != "alice" || msgs[0].BBS != "leafbbs" {
		t.Errorf("lobby history = %+v", msgs)
	}
	rooms, err := s.Rooms()
	if err != nil || len(rooms) != 1 || rooms[0].Name != "lobby" || rooms[0].UserCount != 1 {
		t.Errorf("Rooms = %+v, %v; want lobby with 1 user", rooms, err)
	}

	// The hub rate-limits chat per node, so an immediate second message is
	// joined but not posted, and the caller is told.
	err = l.SendChatCtx(context.Background(), "hello?", "alice")
	if err == nil || !strings.Contains(err.Error(), "chat post returned 429") {
		t.Errorf("second SendChat = %v, want a 429 post error", err)
	}

	stranger, _ := setupLeaf(t, ts.URL, &mockJAMWriter{})
	err = stranger.SendChat("hi", "mallory")
	if err == nil || !strings.Contains(err.Error(), "chat join returned 401") {
		t.Errorf("SendChat from unregistered node = %v, want a 401 join error", err)
	}

	down, _ := setupLeaf(t, deadHubURL(t), &mockJAMWriter{})
	if err := down.SendChat("hi", "alice"); err == nil {
		t.Error("SendChat should fail when the hub is down")
	}
}

func TestSendChat_PostTransportFailure(t *testing.T) {
	// The hub accepts the join, then drops the connection on the post.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/join") {
			_, _ = w.Write([]byte(`{}`))
			return
		}
		panic(http.ErrAbortHandler)
	}))
	defer ts.Close()

	l, _ := setupLeaf(t, ts.URL, &mockJAMWriter{})
	err := l.SendChat("hi", "alice")
	if err == nil || !strings.Contains(err.Error(), "chat post") {
		t.Errorf("SendChat = %v, want a chat post error", err)
	}
}

func TestStart_SubscribesFetchesNALAndPolls(t *testing.T) {
	ts, h, hubKS := newTestHub(t, true)
	seedHubNAL(t, h, hubKS, "gen.general")

	poster := subscribedLeaf(t, ts, "poster", "gen.general")
	if err := poster.SendMessage(testMessage(testUUID1)); err != nil {
		t.Fatalf("post first message: %v", err)
	}

	writer := &chanJAMWriter{written: make(chan protocol.Message, 8)}
	nals := make(chan *protocol.NAL, 8)
	l, ix := setupLeaf(t, ts.URL, writer)
	l.cfg.BBSName = "startbbs"
	l.cfg.AreaTags = []string{"gen.general"}
	l.cfg.PollInterval = 20 * time.Millisecond
	l.cfg.OnNAL = func(n *protocol.NAL) { nals <- n }

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		l.Start(ctx)
	}()
	stop := func() {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("Start did not return after context cancel")
		}
	}
	defer stop()

	recv := func(what string) protocol.Message {
		t.Helper()
		select {
		case m := <-writer.written:
			return m
		case <-time.After(5 * time.Second):
			t.Fatalf("timed out waiting for %s", what)
			return protocol.Message{}
		}
	}

	// Start hands the verified NAL to OnNAL before polling begins.
	select {
	case n := <-nals:
		if n.Network != "testnet" || n.FindArea("gen.general") == nil {
			t.Errorf("OnNAL got %+v", n)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("OnNAL was not called after subscribing")
	}
	if h.Subscribers().Get(l.cfg.Keystore.NodeID(), "testnet") == nil {
		t.Error("Start did not register the leaf with the hub")
	}

	// The message already on the hub arrives with the initial poll.
	if m := recv("backlog message"); m.MsgUUID != testUUID1 {
		t.Errorf("first message = %s, want %s", m.MsgUUID, testUUID1)
	}
	// One posted afterwards arrives on a later poll tick.
	if err := poster.SendMessage(testMessage(testUUID2)); err != nil {
		t.Fatalf("post second message: %v", err)
	}
	if m := recv("second message"); m.MsgUUID != testUUID2 {
		t.Errorf("second message = %s, want %s", m.MsgUUID, testUUID2)
	}

	stop()
	for _, uuid := range []string{testUUID1, testUUID2} {
		if seen, err := ix.Seen(uuid); err != nil || !seen {
			t.Errorf("dedup Seen(%s) = %v, %v; want true", uuid, seen, err)
		}
	}
	select {
	case m := <-writer.written:
		t.Errorf("message %s was written more than once", m.MsgUUID)
	default:
	}
}

func TestStart_GivesUpOnCancelWhileHubUnreachable(t *testing.T) {
	nalCalled := false
	l, _ := setupLeaf(t, deadHubURL(t), &mockJAMWriter{})
	l.cfg.OnNAL = func(*protocol.NAL) { nalCalled = true }

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		l.Start(ctx)
	}()
	// Subscribe fails and Start waits to retry; cancelling ends the wait.
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Start did not return after context cancel")
	}
	if nalCalled {
		t.Error("OnNAL must not run when the leaf never subscribed")
	}
}

func TestFetchNALAndAreas(t *testing.T) {
	ts, h, hubKS := newTestHub(t, true)
	l, _ := setupLeaf(t, ts.URL, &mockJAMWriter{})
	ctx := context.Background()

	// Nothing published yet.
	if _, err := l.FetchNAL(ctx); err == nil {
		t.Error("FetchNAL should fail before the hub has a NAL")
	}
	if _, err := l.Areas(ctx); err == nil {
		t.Error("Areas should fail before the hub has a NAL")
	}

	seedHubNAL(t, h, hubKS, "gen.general", "gen.retro")
	n, err := l.FetchNAL(ctx)
	if err != nil {
		t.Fatalf("FetchNAL: %v", err)
	}
	if n.CoordNodeID != hubKS.NodeID() || len(n.Areas) != 2 {
		t.Errorf("FetchNAL = %+v", n)
	}

	areas, err := l.Areas(ctx)
	if err != nil {
		t.Fatalf("Areas: %v", err)
	}
	if len(areas) != 2 || areas[0].Tag != "gen.general" || areas[1].Tag != "gen.retro" {
		t.Errorf("Areas = %+v", areas)
	}

	// Areas is served from the cache from now on: a newer NAL on the hub is
	// not picked up until something refreshes it.
	seedHubNAL(t, h, hubKS, "gen.general")
	if areas, err = l.Areas(ctx); err != nil || len(areas) != 2 {
		t.Errorf("cached Areas = %+v, %v; want the 2 cached areas", areas, err)
	}
	if err := l.refreshNAL(ctx); err != nil {
		t.Fatalf("refreshNAL: %v", err)
	}
	if areas, err = l.Areas(ctx); err != nil || len(areas) != 1 {
		t.Errorf("Areas after refresh = %+v, %v; want 1 area", areas, err)
	}

	l.nalCache = nil
	if _, err := l.Areas(ctx); err == nil {
		t.Error("Areas should fail without a NAL cache")
	}
	if err := l.refreshNAL(ctx); err == nil {
		t.Error("refreshNAL should fail without a NAL cache")
	}
}

func TestFetchNAL_RejectsTamperedNAL(t *testing.T) {
	// A NAL whose contents were changed after signing must not be accepted.
	tampered := strings.Replace(string(signedTestNAL(t)), `"network":"testnet"`, `"network":"evilnet"`, 1)
	if !strings.Contains(tampered, "evilnet") {
		t.Fatal("test NAL did not contain the expected network field")
	}
	l, _ := setupLeaf(t, cannedHub(t, 200, tampered).URL, &mockJAMWriter{})
	if _, err := l.FetchNAL(context.Background()); err == nil || !strings.Contains(err.Error(), "verify NAL") {
		t.Errorf("FetchNAL = %v, want a verification error", err)
	}
}

func TestProposeArea(t *testing.T) {
	ts, h, hubKS := newTestHub(t, true)
	seedHubNAL(t, h, hubKS)
	l := subscribedLeaf(t, ts, "leafbbs")

	resp, err := l.ProposeArea(protocol.AreaProposalRequest{
		Tag: "gen.retro", Name: "Retro Computing", Description: "Old iron", AllowANSI: true,
	})
	if err != nil {
		t.Fatalf("ProposeArea: %v", err)
	}
	if !resp.OK || resp.Status != "approved" || resp.ProposalID == "" {
		t.Errorf("ProposeArea response = %+v", resp)
	}
	n, err := l.FetchNAL(context.Background())
	if err != nil {
		t.Fatalf("FetchNAL: %v", err)
	}
	area := n.FindArea("gen.retro")
	if area == nil {
		t.Fatal("approved proposal is missing from the NAL")
	}
	if area.Name != "Retro Computing" || area.ManagerNodeID != l.cfg.Keystore.NodeID() {
		t.Errorf("proposed area = %+v", area)
	}

	// The hub's own explanation is surfaced for a rejected proposal.
	_, err = l.ProposeArea(protocol.AreaProposalRequest{Tag: "Not A Tag", Name: "Bad"})
	if err == nil || !strings.HasPrefix(err.Error(), "hub: ") {
		t.Errorf("ProposeArea with bad tag = %v, want the hub's error", err)
	}
}

func TestProposeArea_HubFailures(t *testing.T) {
	req := protocol.AreaProposalRequest{Tag: "gen.retro", Name: "Retro"}

	l, _ := setupLeaf(t, cannedHub(t, 502, "bad gateway").URL, &mockJAMWriter{})
	if _, err := l.ProposeArea(req); err == nil || !strings.Contains(err.Error(), "502: bad gateway") {
		t.Errorf("ProposeArea on 502 = %v, want status and body in the error", err)
	}

	l, _ = setupLeaf(t, cannedHub(t, 200, "<html>").URL, &mockJAMWriter{})
	if _, err := l.ProposeArea(req); err == nil || !strings.Contains(err.Error(), "decode proposal response") {
		t.Errorf("ProposeArea on non-JSON reply = %v, want a decode error", err)
	}

	l, _ = setupLeaf(t, deadHubURL(t), &mockJAMWriter{})
	if _, err := l.ProposeArea(req); err == nil {
		t.Error("ProposeArea should fail when the hub is down")
	}
}

func TestDispatchNALEvent_OnlyNALUpdatedSchedulesRefetch(t *testing.T) {
	l, _ := setupLeaf(t, "http://hub.invalid", &mockJAMWriter{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	for _, ev := range []protocol.Event{
		chatEvent(t, protocol.EventAreaAccessRequested, protocol.AreaAccessRequestedPayload{Network: "testnet", Tag: "gen.a"}),
		chatEvent(t, protocol.EventProposalRejected, protocol.ProposalRejectedPayload{Network: "testnet", Tag: "gen.a"}),
		chatEvent(t, protocol.EventSubscriptionDenied, protocol.SubscriptionDeniedPayload{Network: "testnet", Tag: "gen.a"}),
		{Type: protocol.EventProposalRejected, Data: json.RawMessage(`{bad`)},
		chatEvent(t, protocol.EventLogon, protocol.LogonPayload{Handle: "alice"}),
	} {
		l.dispatchNALEvent(ctx, ev)
		if l.refetchPending.Load() {
			t.Errorf("%s event scheduled a NAL re-fetch", ev.Type)
		}
	}

	l.dispatchNALEvent(ctx, chatEvent(t, protocol.EventNALUpdated, protocol.NALUpdatedPayload{Network: "testnet"}))
	if !l.refetchPending.Load() {
		t.Error("nal_updated did not schedule a NAL re-fetch")
	}
}

// pagedHub serves one page of messages per request, in order, and records the
// cursor each request carried.
type pagedHub struct {
	mu     sync.Mutex
	pages  [][]protocol.Message
	cursor []string
}

func (p *pagedHub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p.mu.Lock()
	defer p.mu.Unlock()
	n := len(p.cursor)
	p.cursor = append(p.cursor, r.URL.Query().Get("since"))
	var page []protocol.Message
	if n < len(p.pages) {
		page = p.pages[n]
	}
	if n+1 < len(p.pages) {
		w.Header().Set("X-V3Net-Has-More", "true")
	}
	data, _ := json.Marshal(page)
	_, _ = w.Write(data)
}

func TestPoll_FollowsHasMoreWithCursor(t *testing.T) {
	ph := &pagedHub{pages: [][]protocol.Message{
		{testMessage(testUUID1)},
		{testMessage(testUUID2), testMessage(testUUID3)},
	}}
	ts := httptest.NewServer(ph)
	defer ts.Close()

	writer := &mockJAMWriter{}
	l, ix := setupLeaf(t, ts.URL, writer)
	count, err := l.Poll(context.Background())
	if err != nil {
		t.Fatalf("Poll: %v", err)
	}
	if count != 3 || writer.count() != 3 {
		t.Errorf("Poll returned %d, wrote %d; want 3 and 3", count, writer.count())
	}
	// The second request resumes after the last message of the first page.
	if len(ph.cursor) != 2 || ph.cursor[0] != "" || ph.cursor[1] != testUUID1 {
		t.Errorf("request cursors = %q, want [\"\" %s]", ph.cursor, testUUID1)
	}
	if last, err := ix.LastSeen("testnet"); err != nil || last != testUUID3 {
		t.Errorf("LastSeen = %q, %v; want %s", last, err, testUUID3)
	}
}

func TestPoll_SkipsInvalidAndUnwritableMessages(t *testing.T) {
	invalid := testMessage("not-a-uuid")
	oversized := testMessage(testUUID3)
	oversized.Body = strings.Repeat("x", protocol.MaxBodyBytes+500)
	ts, _ := newMockHub(t, []protocol.Message{invalid, testMessage(testUUID1), testMessage(testUUID2), oversized})

	writer := &failingJAMWriter{failUUID: testUUID1}
	l, ix := setupLeaf(t, ts.URL, writer)
	count, err := l.Poll(context.Background())
	if err != nil {
		t.Fatalf("Poll: %v", err)
	}
	if count != 2 || writer.count() != 2 {
		t.Fatalf("Poll returned %d, wrote %d; want 2 and 2", count, writer.count())
	}
	if writer.messages[0].MsgUUID != testUUID2 {
		t.Errorf("first written message = %s, want %s", writer.messages[0].MsgUUID, testUUID2)
	}
	// An over-long body is cut to the protocol limit and flagged.
	if got := writer.messages[1]; len(got.Body) > protocol.MaxBodyBytes || !got.IsTruncated() {
		t.Errorf("oversized message: body %d bytes, truncated flag %v", len(got.Body), got.IsTruncated())
	}
	// A message the JAM base refused is not marked seen.
	if seen, _ := ix.Seen(testUUID1); seen {
		t.Error("unwritable message was marked seen")
	}
	if seen, _ := ix.Seen(testUUID2); !seen {
		t.Error("written message was not marked seen")
	}
}

func TestPoll_Errors(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		want   string
	}{
		{"hub refuses", 401, `{"error":"unknown or inactive node"}`, "returned 401"},
		{"reply is not JSON", 200, `<html>`, "decode messages"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			writer := &mockJAMWriter{}
			l, _ := setupLeaf(t, cannedHub(t, tc.status, tc.body).URL, writer)
			count, err := l.Poll(context.Background())
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("Poll = %v, want an error containing %q", err, tc.want)
			}
			if count != 0 || writer.count() != 0 {
				t.Errorf("failed poll processed %d messages, wrote %d", count, writer.count())
			}
		})
	}

	t.Run("context already cancelled", func(t *testing.T) {
		ts, _ := newMockHub(t, []protocol.Message{testMessage(testUUID1)})
		writer := &mockJAMWriter{}
		l, _ := setupLeaf(t, ts.URL, writer)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := l.Poll(ctx); !errors.Is(err, context.Canceled) {
			t.Errorf("Poll = %v, want context.Canceled", err)
		}
		if writer.count() != 0 {
			t.Errorf("cancelled poll wrote %d messages", writer.count())
		}
	})

	t.Run("hub unreachable", func(t *testing.T) {
		l, _ := setupLeaf(t, deadHubURL(t), &mockJAMWriter{})
		if _, err := l.Poll(context.Background()); err == nil || !strings.Contains(err.Error(), "fetch messages") {
			t.Errorf("Poll = %v, want a fetch error", err)
		}
	})

	t.Run("dedup index closed", func(t *testing.T) {
		ts, _ := newMockHub(t, []protocol.Message{testMessage(testUUID1)})
		l, ix := setupLeaf(t, ts.URL, &mockJAMWriter{})
		ix.Close()
		if _, err := l.Poll(context.Background()); err == nil || !strings.Contains(err.Error(), "get cursor") {
			t.Errorf("Poll = %v, want a cursor error", err)
		}
	})
}

func TestSendMessage_HubRejectionIsAnError(t *testing.T) {
	ts, h, hubKS := newTestHub(t, true)
	seedHubNAL(t, h, hubKS, "gen.general")
	l := subscribedLeaf(t, ts, "leafbbs") // no area subscriptions

	err := l.SendMessage(testMessage(testUUID1))
	if err == nil || !strings.Contains(err.Error(), "403") {
		t.Errorf("SendMessage to unsubscribed area = %v, want a 403 error", err)
	}
	msg := testMessage(testUUID1)
	msg.AreaTag = "gen.missing"
	err = l.SendMessageCtx(context.Background(), msg)
	if err == nil || !strings.Contains(err.Error(), fmt.Sprint(http.StatusUnprocessableEntity)) {
		t.Errorf("SendMessage to unknown area = %v, want a 422 error", err)
	}
}
