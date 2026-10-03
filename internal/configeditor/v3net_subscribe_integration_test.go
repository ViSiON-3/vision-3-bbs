package configeditor

import (
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/hub"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/keystore"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/nal"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

// Exercise the actual editor command against mandatory hub authentication,
// including reloading an existing node key and updating its registration.
func TestSubscribeToAreas_StrictHub(t *testing.T) {
	dir := t.TempDir()
	hubKS, _, err := keystore.Load(filepath.Join(dir, "hub.key"))
	if err != nil {
		t.Fatal(err)
	}
	h, err := hub.New(hub.Config{DataDir: dir, Keystore: hubKS, AutoApprove: true,
		RequireSignedSubscribe: true, Networks: []hub.NetworkConfig{{Name: "testnet"}}})
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	n := &protocol.NAL{V3NetNAL: "1.0", Network: "testnet", CoordNodeID: hubKS.NodeID(), CoordPubKeyB64: hubKS.PubKeyBase64(), Areas: []protocol.Area{{Tag: "gen.general", Name: "General", Language: "en", Access: protocol.AreaAccess{Mode: protocol.AccessModeOpen}}}}
	if err := nal.Sign(n, hubKS); err != nil {
		t.Fatal(err)
	}
	if err := h.NALStore().Put("testnet", n); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(h.Mux())
	defer ts.Close()
	keyPath := testKeystorePath(t)
	var nodeID string
	for _, name := range []string{"Original BBS", "Renamed BBS"} {
		ks, _, err := keystore.Load(keyPath)
		if err != nil {
			t.Fatal(err)
		}
		if nodeID != "" && ks.NodeID() != nodeID {
			t.Fatal("node identity changed")
		}
		nodeID = ks.NodeID()
		msg := subscribeToAreas(ts.URL, "testnet", []string{"gen.general"}, ks, name, "bbs.example.net")().(subscribeAreasMsg)
		if msg.err != nil || len(msg.statuses) != 1 || msg.statuses[0].Status != "active" {
			t.Fatalf("subscribe: %+v", msg)
		}
		sub := h.Subscribers().Get(nodeID, "testnet")
		if sub == nil || sub.BBSName != name || sub.Status != "active" || sub.PubKeyB64 != ks.PubKeyBase64() {
			t.Fatalf("registration: %+v", sub)
		}
	}
}
