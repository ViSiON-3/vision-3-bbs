package hub

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/keystore"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

// signedRoster returns coord's roster of members at seq.
func signedRoster(t *testing.T, coord *keystore.Keystore, seq int64, members ...*keystore.Keystore) *protocol.CentralRoster {
	t.Helper()
	r := &protocol.CentralRoster{V3NetCentral: protocol.CentralRosterVersion, Network: "testnet", Seq: seq,
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
