package hub

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/keystore"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/nal"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

// newKey returns a fresh keystore named name.
func newKey(t *testing.T, name string) *keystore.Keystore {
	t.Helper()
	ks, _, err := keystore.Load(filepath.Join(t.TempDir(), name))
	if err != nil {
		t.Fatalf("load keystore: %v", err)
	}
	return ks
}

// signHandover returns from's handover of network to to.
func signHandover(t *testing.T, network string, from, to *keystore.Keystore) *protocol.CoordHandover {
	t.Helper()
	h, err := protocol.NewCoordHandover(from, network, to.PubKeyBase64(), time.Now())
	if err != nil {
		t.Fatalf("NewCoordHandover: %v", err)
	}
	return h
}

// postJSON sends v as a request signed by ks and returns the status.
func postJSON(t *testing.T, ks *keystore.Keystore, url string, v any) int {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(signedRequest(t, ks, "POST", url, string(data)))
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode
}

// getNALBody fetches the served NAL's raw JSON.
func getNALBody(t *testing.T, ts *httptest.Server) []byte {
	t.Helper()
	resp, err := http.Get(ts.URL + "/v3net/v1/testnet/nal")
	if err != nil {
		t.Fatalf("GET /nal: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /nal: status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return body
}

// A handover signed by the coordinator key in force is stored once and
// served with the NAL; from then on only the new key may post the NAL.
func TestHandoverStoredAndServed(t *testing.T) {
	h, hubKS := setupTestHub(t)
	ts := httptest.NewServer(h.newMux())
	defer ts.Close()
	seedTestNAL(t, h, hubKS)
	newKS, poster := newKey(t, "new.key"), newKey(t, "poster.key")
	registerLeaf(t, ts, poster)
	registerLeaf(t, ts, newKS)
	registerLeaf(t, ts, hubKS)

	ho := signHandover(t, "testnet", hubKS, newKS)
	url := ts.URL + "/v3net/v1/testnet/nal/handover"
	// Any subscriber may post it, and posting it again changes nothing.
	for i := 0; i < 2; i++ {
		if got := postJSON(t, poster, url, ho); got != http.StatusOK {
			t.Fatalf("POST handover #%d: status %d", i+1, got)
		}
	}

	var served protocol.NAL
	if err := json.Unmarshal(getNALBody(t, ts), &served); err != nil {
		t.Fatal(err)
	}
	if len(served.Handovers) != 1 || served.Handovers[0] != *ho {
		t.Fatalf("served handovers = %+v, want the one posted", served.Handovers)
	}
	if err := nal.Verify(&served); err != nil {
		t.Errorf("served NAL does not verify: %v", err)
	}

	// The old key may no longer post the NAL; the new key may.
	oldNAL := &protocol.NAL{V3NetNAL: "1.0", Network: "testnet", Areas: served.Areas}
	if err := nal.Sign(oldNAL, hubKS); err != nil {
		t.Fatal(err)
	}
	if got := postJSON(t, hubKS, ts.URL+"/v3net/v1/testnet/nal", oldNAL); got != http.StatusForbidden {
		t.Errorf("POST NAL by the old key: status %d, want 403", got)
	}
	newNAL := &protocol.NAL{V3NetNAL: "1.0", Network: "testnet", Areas: served.Areas,
		Handovers: []protocol.CoordHandover{*signHandover(t, "testnet", newKS, poster)}}
	if err := nal.Sign(newNAL, newKS); err != nil {
		t.Fatal(err)
	}
	if got := postJSON(t, newKS, ts.URL+"/v3net/v1/testnet/nal", newNAL); got != http.StatusOK {
		t.Errorf("POST NAL by the new key: status %d, want 200", got)
	}
	// Handovers sent inside a posted NAL are not stored.
	if hs, err := h.nalStore.Handovers("testnet"); err != nil || len(hs) != 1 {
		t.Errorf("stored handovers = %d, %v; want only the posted handover", len(hs), err)
	}
}

// The hub refuses a handover that does not verify, names another network,
// is not signed by the key in force, or returns to a key already used.
func TestHandoverRejected(t *testing.T) {
	h, hubKS := setupTestHub(t)
	ts := httptest.NewServer(h.newMux())
	defer ts.Close()
	seedTestNAL(t, h, hubKS)
	k1, k2, poster := newKey(t, "k1.key"), newKey(t, "k2.key"), newKey(t, "poster.key")
	registerLeaf(t, ts, poster)
	url := ts.URL + "/v3net/v1/testnet/nal/handover"

	tampered := signHandover(t, "testnet", hubKS, k1)
	tampered.NewKeyB64 = k2.PubKeyBase64()
	cases := []struct {
		name string
		ho   *protocol.CoordHandover
		want int
	}{
		{"tampered", tampered, http.StatusUnprocessableEntity},
		{"other network", signHandover(t, "othernet", hubKS, k1), http.StatusBadRequest},
		{"not the coordinator", signHandover(t, "testnet", k1, k2), http.StatusConflict},
	}
	for _, tc := range cases {
		if got := postJSON(t, poster, url, tc.ho); got != tc.want {
			t.Errorf("%s: status %d, want %d", tc.name, got, tc.want)
		}
	}

	if got := postJSON(t, poster, url, signHandover(t, "testnet", hubKS, k1)); got != http.StatusOK {
		t.Fatalf("valid handover: status %d", got)
	}
	// k1 is now in force: the hub key cannot hand over again, and k1
	// cannot hand back to it.
	if got := postJSON(t, poster, url, signHandover(t, "testnet", hubKS, k2)); got != http.StatusConflict {
		t.Errorf("handover by the replaced key: status %d, want 409", got)
	}
	if got := postJSON(t, poster, url, signHandover(t, "testnet", k1, hubKS)); got != http.StatusConflict {
		t.Errorf("handover back to the old key: status %d, want 409", got)
	}
	if got := postJSON(t, poster, url, signHandover(t, "testnet", k1, k2)); got != http.StatusOK {
		t.Errorf("second step: status %d, want 200", got)
	}
	if hs, err := h.nalStore.Handovers("testnet"); err != nil || len(hs) != 2 {
		t.Errorf("stored handovers = %d, %v; want 2", len(hs), err)
	}
}

// legacyNAL is protocol.NAL as leaves knew it before handovers.
type legacyNAL struct {
	V3NetNAL       string          `json:"v3net_nal"`
	Network        string          `json:"network"`
	CoordNodeID    string          `json:"coordinator_node_id"`
	CoordPubKeyB64 string          `json:"coordinator_pubkey_b64"`
	Updated        string          `json:"updated"`
	SignatureB64   string          `json:"signature_b64"`
	Areas          []protocol.Area `json:"areas"`
}

// A leaf from before handovers still decodes and verifies a NAL served with
// them.
func TestHandoverNALReadableByOldLeaf(t *testing.T) {
	h, hubKS := setupTestHub(t)
	ts := httptest.NewServer(h.newMux())
	defer ts.Close()
	seedTestNAL(t, h, hubKS)
	if err := h.nalStore.AddHandover("testnet", signHandover(t, "testnet", hubKS, newKey(t, "new.key"))); err != nil {
		t.Fatal(err)
	}

	var old legacyNAL
	if err := json.Unmarshal(getNALBody(t, ts), &old); err != nil {
		t.Fatalf("legacy decode: %v", err)
	}
	n := protocol.NAL{
		V3NetNAL: old.V3NetNAL, Network: old.Network, CoordNodeID: old.CoordNodeID,
		CoordPubKeyB64: old.CoordPubKeyB64, Updated: old.Updated, SignatureB64: old.SignatureB64, Areas: old.Areas,
	}
	if err := nal.Verify(&n); err != nil {
		t.Errorf("legacy verify: %v", err)
	}
}

// A hub restarted with the key its network was handed to re-signs the NAL
// with it; one restarted with any other key leaves the NAL alone.
func TestHubAdoptsHandoverOnRestart(t *testing.T) {
	dir := t.TempDir()
	oldKS, newKS, stranger := newKey(t, "old.key"), newKey(t, "new.key"), newKey(t, "stranger.key")
	open := func(ks *keystore.Keystore) *Hub {
		t.Helper()
		h, err := New(Config{ListenAddr: ":0", DataDir: dir, Keystore: ks,
			Networks: []NetworkConfig{{Name: "testnet"}}})
		if err != nil {
			t.Fatalf("create hub: %v", err)
		}
		return h
	}
	coordKey := func(h *Hub) string {
		t.Helper()
		n, err := h.nalStore.Get("testnet")
		if err != nil || n == nil {
			t.Fatalf("get NAL: %v", err)
		}
		if err := nal.Verify(n); err != nil {
			t.Errorf("stored NAL does not verify: %v", err)
		}
		return n.CoordPubKeyB64
	}

	h := open(oldKS)
	seedTestNAL(t, h, oldKS)
	if err := h.nalStore.AddHandover("testnet", signHandover(t, "testnet", oldKS, newKS)); err != nil {
		t.Fatal(err)
	}
	_ = h.Close()

	h = open(stranger)
	if got := coordKey(h); got != oldKS.PubKeyBase64() {
		t.Errorf("a hub with an unrelated key re-signed the NAL")
	}
	_ = h.Close()

	h = open(newKS)
	defer h.Close()
	if got := coordKey(h); got != newKS.PubKeyBase64() {
		t.Errorf("NAL key after restart with the new key = %s, want %s", got, newKS.PubKeyBase64())
	}
	// Running with the key in force, the hub changes the NAL as usual.
	if err := h.updateNALArea("testnet", "gen.general", func(a *protocol.Area) { a.Description = "after" }); err != nil {
		t.Errorf("updateNALArea with the key in force: %v", err)
	}
	if got := coordKey(h); got != newKS.PubKeyBase64() {
		t.Errorf("NAL key after an edit = %s, want the new key", got)
	}
}

// Between accepting a handover and restarting with the new key, the hub
// refuses every change it would sign with the old key: re-signing would
// undo the handover for leaves that already follow the new key.
func TestHandoverWindowRefusesHubSigning(t *testing.T) {
	h, hubKS := setupTestHub(t)
	ts := httptest.NewServer(h.newMux())
	defer ts.Close()
	seedTestNAL(t, h, hubKS)
	registerHubAsLeaf(t, ts, hubKS)
	proposer := newKey(t, "proposer.key")
	registerLeaf(t, ts, proposer)
	before, err := h.nalStore.Get("testnet")
	if err != nil {
		t.Fatal(err)
	}

	// Without a handover the hub signs its changes as before.
	if err := h.SignNAL(before); err != nil {
		t.Fatalf("SignNAL before the handover: %v", err)
	}
	if err := h.nalStore.AddHandover("testnet", signHandover(t, "testnet", hubKS, newKey(t, "new.key"))); err != nil {
		t.Fatal(err)
	}

	if err := h.SignNAL(before); !errors.Is(err, ErrHandedOver) {
		t.Errorf("SignNAL = %v, want ErrHandedOver", err)
	}
	if err := h.updateNALArea("testnet", "gen.general", func(*protocol.Area) {}); !errors.Is(err, ErrHandedOver) {
		t.Errorf("updateNALArea = %v, want ErrHandedOver", err)
	}
	if got := postJSON(t, hubKS, ts.URL+"/v3net/v1/testnet/areas/gen.general/access/mode",
		protocol.AccessModeRequest{Mode: protocol.AccessModeClosed}); got != http.StatusConflict {
		t.Errorf("set access mode: status %d, want 409", got)
	}

	// A proposal is not auto-approved; approving it by hand is refused.
	data, _ := json.Marshal(protocol.AreaProposalRequest{Tag: "gen.new", Name: "New", Language: "en", AccessMode: protocol.AccessModeOpen})
	resp, err := http.DefaultClient.Do(signedRequest(t, proposer, "POST", ts.URL+"/v3net/v1/testnet/areas/propose", string(data)))
	if err != nil {
		t.Fatal(err)
	}
	var pr protocol.ProposalResponse
	_ = json.NewDecoder(resp.Body).Decode(&pr)
	resp.Body.Close()
	if pr.Status != "pending" || pr.ProposalID == "" {
		t.Fatalf("proposal response = %+v, want pending", pr)
	}
	if got := postJSON(t, hubKS, ts.URL+"/v3net/v1/testnet/areas/proposals/"+pr.ProposalID+"/approve", struct{}{}); got != http.StatusConflict {
		t.Errorf("approve proposal: status %d, want 409", got)
	}

	after, err := h.nalStore.Get("testnet")
	if err != nil {
		t.Fatal(err)
	}
	if after.SignatureB64 != before.SignatureB64 || after.FindArea("gen.new") != nil || after.FindArea("gen.general").Access.Mode != protocol.AccessModeOpen {
		t.Error("the stored NAL changed during the handover window")
	}
}

// Every posted NAL must be signed by the coordinator key in force, with or
// without handovers, so no other key's NAL replaces the stored one.
func TestPostNALRequiresKeyInForce(t *testing.T) {
	dir := t.TempDir()
	oldKS, newKS, other := newKey(t, "old.key"), newKey(t, "new.key"), newKey(t, "other.key")
	open := func(ks *keystore.Keystore) (*Hub, *httptest.Server) {
		t.Helper()
		h, err := New(Config{ListenAddr: ":0", DataDir: dir, Keystore: ks, AutoApprove: true,
			RequireSignedSubscribe: true, Networks: []NetworkConfig{{Name: "testnet"}}})
		if err != nil {
			t.Fatalf("create hub: %v", err)
		}
		return h, httptest.NewServer(h.newMux())
	}
	post := func(ts *httptest.Server, sender, signer *keystore.Keystore) int {
		t.Helper()
		n := &protocol.NAL{V3NetNAL: "1.0", Network: "testnet", Areas: []protocol.Area{}}
		if err := nal.Sign(n, signer); err != nil {
			t.Fatal(err)
		}
		return postJSON(t, sender, ts.URL+"/v3net/v1/testnet/nal", n)
	}

	h, ts := open(oldKS)
	registerLeaf(t, ts, oldKS)
	registerLeaf(t, ts, newKS)
	if got := post(ts, oldKS, other); got != http.StatusBadRequest {
		t.Errorf("initial NAL signed by another key: status %d, want 400", got)
	}
	// Naming the hub's node does not make another key's NAL the hub's.
	forged := &protocol.NAL{V3NetNAL: "1.0", Network: "testnet", Areas: []protocol.Area{}}
	if err := nal.Sign(forged, other); err != nil {
		t.Fatal(err)
	}
	forged.CoordNodeID = oldKS.NodeID()
	if got := postJSON(t, oldKS, ts.URL+"/v3net/v1/testnet/nal", forged); got != http.StatusForbidden {
		t.Errorf("initial NAL naming the hub but signed by another key: status %d, want 403", got)
	}
	if got := post(ts, oldKS, oldKS); got != http.StatusOK {
		t.Fatalf("initial NAL: status %d", got)
	}
	// No handovers: the stored key in force, not just its node, must sign.
	if got := post(ts, oldKS, other); got != http.StatusForbidden {
		t.Errorf("coordinator node posting another key's NAL: status %d, want 403", got)
	}
	if got := post(ts, oldKS, oldKS); got != http.StatusOK {
		t.Errorf("same key again: status %d, want 200", got)
	}
	if err := h.nalStore.AddHandover("testnet", signHandover(t, "testnet", oldKS, newKS)); err != nil {
		t.Fatal(err)
	}
	ts.Close()
	_ = h.Close()

	// Restarted with the new key, the stored NAL is the new key's, and
	// neither the old key nor any other may replace it.
	h, ts = open(newKS)
	defer h.Close()
	defer ts.Close()
	for name, signer := range map[string]*keystore.Keystore{"old key": oldKS, "other key": other} {
		if got := post(ts, newKS, signer); got != http.StatusForbidden {
			t.Errorf("new node posting a NAL signed by the %s: status %d, want 403", name, got)
		}
	}
	if got := post(ts, newKS, newKS); got != http.StatusOK {
		t.Errorf("new key: status %d, want 200", got)
	}
}
