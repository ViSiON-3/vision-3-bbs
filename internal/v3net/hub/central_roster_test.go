package hub

import (
	"encoding/json"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/keystore"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

// signedRoster returns coord's roster of members at seq, edited from the
// roster at seq-1 (0, none, for the first).
func signedRoster(t *testing.T, coord *keystore.Keystore, seq int64, members ...*keystore.Keystore) *protocol.CentralRoster {
	t.Helper()
	return signedRosterFrom(t, coord, seq, seq-1, members...)
}

// signedRosterFrom is signedRoster edited from the roster at prev.
func signedRosterFrom(t *testing.T, coord *keystore.Keystore, seq, prev int64, members ...*keystore.Keystore) *protocol.CentralRoster {
	t.Helper()
	r := &protocol.CentralRoster{V3NetCentral: protocol.CentralRosterVersion, Network: "testnet", Seq: seq, PrevSeq: prev,
		PublishedAt: time.Now().UTC().Format(time.RFC3339)}
	for _, m := range members {
		r.Members = append(r.Members, protocol.CentralRosterMember{NodeID: m.NodeID(), PubKeyB64: m.PubKeyBase64()})
	}
	if err := r.Sign(coord); err != nil {
		t.Fatal(err)
	}
	return r
}

func rosterDo(t *testing.T, ts *httptest.Server, as *keystore.Keystore, method string, r *protocol.CentralRoster) (int, []byte) {
	t.Helper()
	body := ""
	if r != nil {
		b, _ := json.Marshal(r)
		body = string(b)
	}
	resp, err := http.DefaultClient.Do(signedRequest(t, as, method, ts.URL+"/v3net/v1/testnet/central/roster", body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, b
}

// The hub names the roster in /info.
func TestNetworkInfoAdvertisesRoster(t *testing.T) {
	if !slices.Contains(hubFeatures, protocol.FeatureCentralRoster) {
		t.Error("hub does not advertise central.roster")
	}
}

// The coordinator publishes a roster, every board reads it back as signed,
// a newer one replaces it, an older one is refused, and an event goes out.
func TestCentralRosterPublishAndServe(t *testing.T) {
	h, hubKS := setupTestHub(t)
	seedNALWithAreas(t, h, hubKS, nil) // the hub's key coordinates testnet
	ts := httptest.NewServer(h.newMux())
	defer ts.Close()
	a, b := newLeafKS(t), newLeafKS(t)
	registerHubAsLeaf(t, ts, hubKS)
	registerLeaf(t, ts, a)
	registerLeaf(t, ts, b)
	events, cancel := h.broadcaster.Subscribe("testnet")
	defer cancel()

	if code, _ := rosterDo(t, ts, a, http.MethodGet, nil); code != http.StatusNotFound {
		t.Errorf("before any roster: %d, want 404", code)
	}
	first := signedRoster(t, hubKS, 1, a)
	if code, body := rosterDo(t, ts, hubKS, http.MethodPost, first); code != http.StatusOK {
		t.Fatalf("publish: %d %s", code, body)
	}
	select {
	case ev := <-events:
		if ev.Type != protocol.EventCentralRosterUpdated {
			t.Errorf("event %q", ev.Type)
		}
	case <-time.After(time.Second):
		t.Error("no event")
	}
	code, body := rosterDo(t, ts, b, http.MethodGet, nil)
	var got protocol.CentralRoster
	if code != http.StatusOK || json.Unmarshal(body, &got) != nil || got.Verify() != nil || !got.Admits(a.NodeID(), a.PubKeyBase64()) {
		t.Fatalf("served %d %s", code, body)
	}

	second := signedRoster(t, hubKS, 2, a, b)
	if code, _ := rosterDo(t, ts, hubKS, http.MethodPost, second); code != http.StatusOK {
		t.Fatalf("second publish: %d", code)
	}
	if code, _ := rosterDo(t, ts, hubKS, http.MethodPost, first); code != http.StatusConflict {
		t.Errorf("replaying the older roster: %d, want 409", code)
	}
	_, body = rosterDo(t, ts, a, http.MethodGet, nil)
	_ = json.Unmarshal(body, &got)
	if got.Seq != 2 || len(got.Members) != 2 {
		t.Errorf("served seq %d with %d members", got.Seq, len(got.Members))
	}
}

// Only the coordinator in force may publish: not another board, not with
// another key, and not on a network with no NAL.
func TestCentralRosterRefusals(t *testing.T) {
	h, hubKS := setupTestHub(t)
	ts := httptest.NewServer(h.newMux())
	defer ts.Close()
	a := newLeafKS(t)
	registerHubAsLeaf(t, ts, hubKS)
	registerLeaf(t, ts, a)

	if code, _ := rosterDo(t, ts, hubKS, http.MethodPost, signedRoster(t, hubKS, 1, a)); code != http.StatusConflict {
		t.Errorf("no NAL: %d, want 409", code)
	}
	seedNALWithAreas(t, h, hubKS, nil)
	// A board signs a roster of its own and posts it.
	if code, _ := rosterDo(t, ts, a, http.MethodPost, signedRoster(t, a, 1, a)); code != http.StatusForbidden {
		t.Errorf("a board's roster: %d, want 403", code)
	}
	// The coordinator's node posts a roster signed by another key.
	if code, _ := rosterDo(t, ts, hubKS, http.MethodPost, signedRoster(t, a, 1, a)); code != http.StatusForbidden {
		t.Errorf("another key's roster: %d, want 403", code)
	}
	// A member added after signing.
	forged := signedRoster(t, hubKS, 1, a)
	forged.Members = append(forged.Members, protocol.CentralRosterMember{NodeID: hubKS.NodeID(), PubKeyB64: hubKS.PubKeyBase64()})
	if code, _ := rosterDo(t, ts, hubKS, http.MethodPost, forged); code != http.StatusForbidden {
		t.Errorf("forged roster: %d, want 403", code)
	}
}

// Two edits made from the same roster cannot both land: the second names
// a prev_seq that is no longer the stored roster's, and is refused, so it
// cannot silently drop the board the first admitted.
func TestCentralRosterConflictingEdits(t *testing.T) {
	h, hubKS := setupTestHub(t)
	seedNALWithAreas(t, h, hubKS, nil)
	ts := httptest.NewServer(h.newMux())
	defer ts.Close()
	a, b := newLeafKS(t), newLeafKS(t)
	registerHubAsLeaf(t, ts, hubKS)

	if code, _ := rosterDo(t, ts, hubKS, http.MethodPost, signedRoster(t, hubKS, 1)); code != http.StatusOK {
		t.Fatal("first roster")
	}
	if code, _ := rosterDo(t, ts, hubKS, http.MethodPost, signedRosterFrom(t, hubKS, 2, 1, a)); code != http.StatusOK {
		t.Fatal("edit admitting a")
	}
	code, body := rosterDo(t, ts, hubKS, http.MethodPost, signedRosterFrom(t, hubKS, 3, 1, b))
	if code != http.StatusConflict || !strings.Contains(string(body), "changed since") {
		t.Errorf("edit from the older roster: %d %s, want a conflict", code, body)
	}
	// A coordinator that had not yet seen any roster cannot replace one.
	if code, _ := rosterDo(t, ts, hubKS, http.MethodPost, signedRosterFrom(t, hubKS, 9, 0, b)); code != http.StatusConflict {
		t.Errorf("first-roster edit over a stored one: %d, want 409", code)
	}
}

// A new coordinator's roster replaces the old coordinator's whatever its
// seq, so an old roster at the highest seq cannot lock the network.
func TestCentralRosterNewCoordinatorStartsOwnSeq(t *testing.T) {
	h, _ := setupTestHub(t)
	oldCoord, newCoord := newLeafKS(t), newLeafKS(t)
	old := signedRosterFrom(t, oldCoord, math.MaxInt64, 0)
	raw, _ := json.Marshal(old)
	if err := h.roster.Put(old, raw); err != nil {
		t.Fatal(err)
	}
	fresh := signedRoster(t, newCoord, 1)
	raw, _ = json.Marshal(fresh)
	if err := h.roster.Put(fresh, raw); err != nil {
		t.Fatalf("new coordinator refused: %v", err)
	}
}

// The hub relays a board's signed line with the line and keeps it in
// history; it refuses one that does not match the request or is signed by
// another key, and still takes an unsigned line from an older board.
func TestSignedChatAtTheHub(t *testing.T) {
	h, _ := setupTestHub(t)
	ts := httptest.NewServer(h.newMux())
	defer ts.Close()
	a, other := newLeafKS(t), newLeafKS(t)
	registerLeaf(t, ts, a)
	// Several posts in a row: the per-user limit is not under test here.
	h.chatLimiter.Stop()
	h.chatLimiter = newRateLimiter(time.Nanosecond)
	events, cancel := h.broadcaster.Subscribe("testnet")
	defer cancel()
	post := func(as *keystore.Keystore, path string, body any) int {
		b, _ := json.Marshal(body)
		resp, err := http.DefaultClient.Do(signedRequest(t, as, http.MethodPost, ts.URL+"/v3net/v1/testnet/chat/rooms/"+path, string(b)))
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	join, _ := protocol.NewChatSigned(a, protocol.ChatSignedJoin, "testnet", "Alice", func(c *protocol.ChatSigned) { c.Room = "lobby" })
	if code := post(a, "join", protocol.ChatJoinRequest{Room: "lobby", Handle: "Alice", Signed: join}); code != http.StatusOK {
		t.Fatalf("signed join: %d", code)
	}
	line, _ := protocol.NewChatSigned(a, protocol.ChatSignedLine, "testnet", "Alice", func(c *protocol.ChatSigned) { c.Room, c.Text = "lobby", "hi" })
	if code := post(a, "post", protocol.ChatPostRequest{Room: "lobby", Handle: "Alice", Text: "hi", Signed: line}); code != http.StatusNoContent {
		t.Fatalf("signed post: %d", code)
	}
	deadline := time.After(time.Second)
	for got := false; !got; {
		select {
		case ev := <-events:
			if ev.Type == protocol.EventChatMessage {
				var p protocol.ChatMsgPayload
				_ = json.Unmarshal(ev.Data, &p)
				if p.Signed == nil || p.Signed.Verify() != nil || p.Signed.ID != line.ID {
					t.Errorf("relayed without its signed line: %+v", p.Signed)
				}
				got = true
			}
		case <-deadline:
			t.Fatal("no chat_message")
		}
	}
	hist, _ := h.chatStore.RoomHistory("testnet", "lobby", 10)
	if len(hist) != 1 || hist[0].Signed == nil || hist[0].Signed.ID != line.ID {
		t.Errorf("history %+v", hist)
	}
	if code := post(a, "post", protocol.ChatPostRequest{Room: "lobby", Handle: "Alice", Text: "changed", Signed: line}); code != http.StatusUnprocessableEntity {
		t.Errorf("mismatched text: %d, want 422", code)
	}
	foreign, _ := protocol.NewChatSigned(other, protocol.ChatSignedLine, "testnet", "Alice", func(c *protocol.ChatSigned) { c.Room, c.Text = "lobby", "hi" })
	if code := post(a, "post", protocol.ChatPostRequest{Room: "lobby", Handle: "Alice", Text: "hi", Signed: foreign}); code != http.StatusForbidden {
		t.Errorf("another key's line: %d, want 403", code)
	}
	if code := post(a, "post", protocol.ChatPostRequest{Room: "lobby", Handle: "Alice", Text: "unsigned"}); code != http.StatusNoContent {
		t.Errorf("unsigned line from an older board: %d, want 204", code)
	}
}
