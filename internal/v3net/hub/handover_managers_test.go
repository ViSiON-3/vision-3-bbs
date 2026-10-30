package hub

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/keystore"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/nal"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

// seedManagedNAL stores a NAL signed by coord with three areas: one managed
// by coord's node, one by other's node, and one naming coord's node ID with
// no key, as older NALs may.
func seedManagedNAL(t *testing.T, h *Hub, coord, other *keystore.Keystore) {
	t.Helper()
	area := func(tag, nodeID, key string) protocol.Area {
		return protocol.Area{Tag: tag, Name: tag, Language: "en", ManagerNodeID: nodeID, ManagerPubKeyB64: key,
			Access: protocol.AreaAccess{Mode: protocol.AccessModeOpen}, Policy: protocol.AreaPolicy{MaxBodyBytes: 65536}}
	}
	n := &protocol.NAL{V3NetNAL: "1.0", Network: "testnet", Areas: []protocol.Area{
		area("gen.coord", coord.NodeID(), coord.PubKeyBase64()),
		area("gen.other", other.NodeID(), other.PubKeyBase64()),
		area("gen.idonly", coord.NodeID(), ""),
	}}
	if err := nal.Sign(n, coord); err != nil {
		t.Fatal(err)
	}
	if err := h.nalStore.Put("testnet", n); err != nil {
		t.Fatal(err)
	}
}

// checkManagers fails unless the stored NAL verifies, is signed by signer,
// and its areas are managed as seedManagedNAL left them with the old
// coordinator's two areas moved to want.
func checkManagers(t *testing.T, h *Hub, signer, want, other *keystore.Keystore) {
	t.Helper()
	n, err := h.nalStore.Get("testnet")
	if err != nil || n == nil {
		t.Fatalf("get NAL: %v", err)
	}
	if err := nal.Verify(n); err != nil {
		t.Errorf("stored NAL does not verify: %v", err)
	}
	if n.CoordPubKeyB64 != signer.PubKeyBase64() {
		t.Errorf("NAL signed by %s, want %s", n.CoordPubKeyB64, signer.PubKeyBase64())
	}
	for _, tag := range []string{"gen.coord", "gen.idonly"} {
		a := n.FindArea(tag)
		if a == nil || a.ManagerNodeID != want.NodeID() || a.ManagerPubKeyB64 != want.PubKeyBase64() {
			t.Errorf("%s managed by %+v, want the node of %s", tag, a, want.PubKeyBase64())
		}
	}
	if a := n.FindArea("gen.other"); a == nil || a.ManagerNodeID != other.NodeID() || a.ManagerPubKeyB64 != other.PubKeyBase64() {
		t.Errorf("gen.other managed by %+v; another node's area must stay", a)
	}
}

// When a hub re-signs the NAL for the key it was handed over to, the areas
// the old coordinator's node managed move to the hub's node in the same
// update, on restart and when a late handover arrives. Areas other nodes
// manage stay theirs.
func TestHandoverMovesCoordinatorAreas(t *testing.T) {
	t.Run("restart with the new key", func(t *testing.T) {
		dir := t.TempDir()
		oldKS, newKS, other := newKey(t, "old.key"), newKey(t, "new.key"), newKey(t, "other.key")
		h := openTestHubAt(t, dir, oldKS)
		seedManagedNAL(t, h, oldKS, other)
		if err := h.nalStore.AddHandover("testnet", signHandover(t, "testnet", oldKS, newKS)); err != nil {
			t.Fatal(err)
		}
		_ = h.Close()
		h = openTestHubAt(t, dir, newKS)
		defer h.Close()
		checkManagers(t, h, newKS, newKS, other)
	})

	t.Run("late handover, NAL still the old key's", func(t *testing.T) {
		dir := t.TempDir()
		oldKS, newKS, other, poster := newKey(t, "old.key"), newKey(t, "new.key"), newKey(t, "other.key"), newKey(t, "poster.key")
		h := openTestHubAt(t, dir, oldKS)
		seedManagedNAL(t, h, oldKS, other)
		ts := httptest.NewServer(h.newMux())
		registerLeaf(t, ts, poster)
		ts.Close()
		_ = h.Close()

		h = openTestHubAt(t, dir, newKS)
		defer h.Close()
		ts = httptest.NewServer(h.newMux())
		defer ts.Close()
		if got := postJSON(t, poster, ts.URL+"/v3net/v1/testnet/nal/handover", signHandover(t, "testnet", oldKS, newKS)); got != http.StatusOK {
			t.Fatalf("POST late handover: status %d", got)
		}
		checkManagers(t, h, newKS, newKS, other)
	})

	t.Run("late handover from the previous signer", func(t *testing.T) {
		dir := t.TempDir()
		oldKS, newKS, other, poster := newKey(t, "old.key"), newKey(t, "new.key"), newKey(t, "other.key"), newKey(t, "poster.key")
		h := openTestHubAt(t, dir, oldKS)
		seedManagedNAL(t, h, oldKS, other)
		ts := httptest.NewServer(h.newMux())
		registerLeaf(t, ts, poster)
		ts.Close()
		_ = h.Close()

		h = openTestHubAt(t, dir, newKS)
		defer h.Close()
		ts = httptest.NewServer(h.newMux())
		defer ts.Close()
		// The lost-key path re-signs without a handover and moves nothing:
		// with no handover there is no word from the old key.
		if err := h.updateNALArea("testnet", "gen.other", func(a *protocol.Area) { a.Description = "switched" }); err != nil {
			t.Fatalf("updateNALArea after the switch: %v", err)
		}
		if n, _ := h.nalStore.Get("testnet"); n.FindArea("gen.coord").ManagerNodeID != oldKS.NodeID() {
			t.Fatal("an area moved without a handover")
		}
		if got := postJSON(t, poster, ts.URL+"/v3net/v1/testnet/nal/handover", signHandover(t, "testnet", oldKS, newKS)); got != http.StatusOK {
			t.Fatalf("POST late handover: status %d", got)
		}
		checkManagers(t, h, newKS, newKS, other)
	})
}

// A hub that took a handover but still runs the old key moves nothing: the
// NAL is not re-signed until the hub runs the new key.
func TestHandoverKeepsAreasUntilAdopted(t *testing.T) {
	dir := t.TempDir()
	oldKS, newKS, other, poster := newKey(t, "old.key"), newKey(t, "new.key"), newKey(t, "other.key"), newKey(t, "poster.key")
	h := openTestHubAt(t, dir, oldKS)
	defer h.Close()
	seedManagedNAL(t, h, oldKS, other)
	ts := httptest.NewServer(h.newMux())
	defer ts.Close()
	registerLeaf(t, ts, poster)
	if got := postJSON(t, poster, ts.URL+"/v3net/v1/testnet/nal/handover", signHandover(t, "testnet", oldKS, newKS)); got != http.StatusOK {
		t.Fatalf("POST handover: status %d", got)
	}
	n, err := h.nalStore.Get("testnet")
	if err != nil || n == nil {
		t.Fatalf("get NAL: %v", err)
	}
	if n.CoordPubKeyB64 != oldKS.PubKeyBase64() || n.FindArea("gen.coord").ManagerNodeID != oldKS.NodeID() ||
		n.FindArea("gen.idonly").ManagerPubKeyB64 != "" {
		t.Errorf("NAL changed before the hub runs the new key: %+v", n)
	}
}

// openTestHubAt opens a hub on dir running ks for testnet.
func openTestHubAt(t *testing.T, dir string, ks *keystore.Keystore) *Hub {
	t.Helper()
	h, err := New(Config{ListenAddr: ":0", DataDir: dir, Keystore: ks, AutoApprove: true,
		RequireSignedSubscribe: true, Networks: []NetworkConfig{{Name: "testnet"}}})
	if err != nil {
		t.Fatalf("create hub: %v", err)
	}
	return h
}
