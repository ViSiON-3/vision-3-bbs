package hub

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/nal"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

// The signer history is served to a subscribed node, signed and bound to the
// request's nonce, and follows the record: the keys that signed the NAL
// before a chain is posted, the handovers held after. An unknown node is
// refused, and a network with no NAL has no history.
func TestSignerHistoryServed(t *testing.T) {
	dir := t.TempDir()
	a, b, c, poster, stranger := newKey(t, "a.key"), newKey(t, "b.key"), newKey(t, "c.key"), newKey(t, "poster.key"), newKey(t, "stranger.key")
	_, ts := switchedHub(t, dir, poster, a, b, c)
	url := ts.URL + "/v3net/v1/testnet/nal/signers"

	get := func() protocol.SignerHistory {
		t.Helper()
		req := signedRequest(t, poster, "GET", url, "")
		req.Header.Set(protocol.HeaderNonce, "signers-nonce")
		resp, body := doRequest(t, req)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET signers: %d %s", resp.StatusCode, body)
		}
		if err := protocol.VerifyResponse(c.PubKey(), req.Header.Get(headerSignature), "signers-nonce", resp.StatusCode, body, resp.Header.Get(protocol.HeaderHubSignature)); err != nil {
			t.Fatalf("reply signature: %v", err)
		}
		var history protocol.SignerHistory
		if err := json.Unmarshal(body, &history); err != nil {
			t.Fatal(err)
		}
		return history
	}

	history := get()
	if !equalKeys(history.PrevSigners, []string{a.PubKeyBase64(), b.PubKeyBase64()}) {
		t.Errorf("previous signers = %q, want A then B", history.PrevSigners)
	}
	if history.NALKeyB64 != c.PubKeyBase64() || history.KeyInForceB64 != c.PubKeyBase64() || len(history.Handovers) != 0 {
		t.Errorf("history = %+v, want the NAL and key in force C, no handovers", history)
	}

	if got := postJSON(t, poster, ts.URL+"/v3net/v1/testnet/nal/handover", chainOf(t, a, b, c)); got != http.StatusOK {
		t.Fatalf("A to B to C: status %d", got)
	}
	history = get()
	if len(history.PrevSigners) != 0 {
		t.Errorf("previous signers after the chain = %q, want none", history.PrevSigners)
	}
	want := []protocol.HandoverLink{{OldKeyB64: a.PubKeyBase64(), NewKeyB64: b.PubKeyBase64()}, {OldKeyB64: b.PubKeyBase64(), NewKeyB64: c.PubKeyBase64()}}
	if len(history.Handovers) != 2 || history.Handovers[0] != want[0] || history.Handovers[1] != want[1] {
		t.Errorf("handovers = %+v, want %+v", history.Handovers, want)
	}

	if resp, _ := doRequest(t, signedRequest(t, stranger, "GET", url, "")); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("unknown node: status %d, want 401", resp.StatusCode)
	}
	if resp, _ := doRequest(t, httptestGet(t, url)); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("unsigned request: status %d, want 401", resp.StatusCode)
	}
}

// A hub with no NAL has no history to give, and the tool treats that as
// nothing to check.
func TestSignerHistoryNoNAL(t *testing.T) {
	h, _ := setupTestHub(t)
	ts := httptest.NewServer(h.newMux())
	defer ts.Close()
	poster := newKey(t, "poster.key")
	registerLeaf(t, ts, poster)
	resp, body := doRequest(t, signedRequest(t, poster, "GET", ts.URL+"/v3net/v1/testnet/nal/signers", ""))
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status %d %s, want 404", resp.StatusCode, body)
	}
}

// Reposting a link the hub adopted long ago, alone or at the head of a chain
// whose next link a later re-sign adopts, moves nothing of the old link's: an
// area the operator gave back to A's node stays there.
func TestRepeatedLinkMovesNoAreas(t *testing.T) {
	dir := t.TempDir()
	a, b, c, poster := newKey(t, "a.key"), newKey(t, "b.key"), newKey(t, "c.key"), newKey(t, "poster.key")
	h := openTestHubAt(t, dir, a)
	seedTestNAL(t, h, a)
	ts := httptest.NewServer(h.newMux())
	registerLeaf(t, ts, poster)
	if got := postJSON(t, poster, ts.URL+"/v3net/v1/testnet/nal/handover", chainOf(t, a, b)[0]); got != http.StatusOK {
		t.Fatalf("A to B: status %d", got)
	}
	ts.Close()
	_ = h.Close()

	h = openTestHubAt(t, dir, b)
	ts = httptest.NewServer(h.newMux())
	if err := h.setNALAreaManager("testnet", "gen.general", func(area *protocol.Area) {
		area.ManagerNodeID, area.ManagerPubKeyB64 = a.NodeID(), a.PubKeyBase64()
	}); err != nil {
		t.Fatal(err)
	}
	url := ts.URL + "/v3net/v1/testnet/nal/handover"
	if got := postJSON(t, poster, url, chainOf(t, a, b)[0]); got != http.StatusOK {
		t.Fatalf("A to B again: status %d", got)
	}
	ts.Close()
	_ = h.Close()

	h = openTestHubAt(t, dir, c)
	defer h.Close()
	ts = httptest.NewServer(h.newMux())
	defer ts.Close()
	url = ts.URL + "/v3net/v1/testnet/nal/handover"
	if got := postJSON(t, poster, url, chainOf(t, a, b, c)); got != http.StatusOK {
		t.Fatalf("A to B to C, A to B repeated: status %d", got)
	}
	n, err := h.nalStore.Get("testnet")
	if err != nil || n.CoordPubKeyB64 != c.PubKeyBase64() {
		t.Fatalf("NAL after the chain: %+v, %v; want C's", n, err)
	}
	if area := n.FindArea("gen.general"); area == nil || area.ManagerNodeID != a.NodeID() {
		t.Errorf("gen.general managed by %+v; the repeated link must leave A's node alone", area)
	}
}

// A chain A to B to C stored while the NAL is B's, whose re-sign failed, can
// next be overtaken by a hub-side NAL change rather than a retry. That change
// is the re-sign: it moves A's and B's areas, and leaves no previous signer
// behind for a later re-sign to move again over the operator's choice.
func TestHubChangeAfterFailedAdoptMovesAreasOnce(t *testing.T) {
	dir := t.TempDir()
	a, b, c, d, poster := newKey(t, "a.key"), newKey(t, "b.key"), newKey(t, "c.key"), newKey(t, "d.key"), newKey(t, "poster.key")
	h := unsignedSwitchHub(t, dir, a, b, c, poster)
	ts := httptest.NewServer(h.newMux())
	defer ts.Close()
	url := ts.URL + "/v3net/v1/testnet/nal/handover"

	if _, err := h.db.Exec(`CREATE TRIGGER fail_nal_update BEFORE UPDATE ON network_nal
		BEGIN SELECT RAISE(ABORT, 'injected'); END`); err != nil {
		t.Fatal(err)
	}
	if status, body := postJSONBody(t, poster, url, chainOf(t, a, b, c)); status != http.StatusInternalServerError {
		t.Fatalf("A to B to C with the store failing: %d %s; want 500", status, body)
	}
	if _, err := h.db.Exec(`DROP TRIGGER fail_nal_update`); err != nil {
		t.Fatal(err)
	}

	if err := h.updateNALArea("testnet", "gen.general", func(area *protocol.Area) { area.Description = "overtaken" }); err != nil {
		t.Fatal(err)
	}
	checkMovedTo(t, h, c)
	if prev, err := h.nalStore.PrevSigners("testnet"); err != nil || len(prev) != 0 {
		t.Errorf("previous signers after the change = %q, %v; want none", prev, err)
	}

	if err := h.setNALAreaManager("testnet", "gen.general", func(area *protocol.Area) {
		area.ManagerNodeID, area.ManagerPubKeyB64 = a.NodeID(), a.PubKeyBase64()
	}); err != nil {
		t.Fatal(err)
	}
	if got := postJSON(t, poster, url, chainOf(t, c, d)[0]); got != http.StatusOK {
		t.Fatalf("C to D: status %d", got)
	}
	ts.Close()
	_ = h.Close()
	h = openTestHubAt(t, dir, d)
	defer h.Close()
	n, err := h.nalStore.Get("testnet")
	if err != nil || n.CoordPubKeyB64 != d.PubKeyBase64() || nal.Verify(n) != nil {
		t.Fatalf("NAL after restarting with D: %+v, %v", n, err)
	}
	if area := n.FindArea("gen.general"); area == nil || area.ManagerNodeID != a.NodeID() {
		t.Errorf("gen.general managed by %+v; the operator's choice of A's node must stay", area)
	}
}

// A NAL the coordinator stores under the new key while the re-sign is pending
// also ends the previous signers' say, so a later re-sign moves nothing of
// theirs. A store under the same key, or with no handover held, keeps them.
func TestStoringNewKeyNALClearsPrevSigners(t *testing.T) {
	dir := t.TempDir()
	a, b, c, poster := newKey(t, "a.key"), newKey(t, "b.key"), newKey(t, "c.key"), newKey(t, "poster.key")
	h := unsignedSwitchHub(t, dir, a, b, c, poster)

	n, err := h.nalStore.Get("testnet")
	if err != nil {
		t.Fatal(err)
	}
	if err := h.nalStore.Put("testnet", n); err != nil {
		t.Fatal(err)
	}
	if prev, err := h.nalStore.PrevSigners("testnet"); err != nil || len(prev) == 0 {
		t.Fatalf("previous signers after a store under the same key = %q, %v; want them kept", prev, err)
	}

	if err := h.nalStore.AddHandovers("testnet", chainOf(t, a, b, c)); err != nil {
		t.Fatal(err)
	}
	if err := nal.Sign(n, c); err != nil {
		t.Fatal(err)
	}
	if err := h.nalStore.Put("testnet", n); err != nil {
		t.Fatal(err)
	}
	if prev, err := h.nalStore.PrevSigners("testnet"); err != nil || len(prev) != 0 {
		t.Errorf("previous signers after a store under the new key = %q, %v; want none", prev, err)
	}
}
