package configeditor

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/keystore"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/nal"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

func browseKey(t *testing.T) *keystore.Keystore {
	t.Helper()
	ks, _, err := keystore.Load(filepath.Join(t.TempDir(), "coord.key"))
	if err != nil {
		t.Fatal(err)
	}
	return ks
}

// serveNAL serves n as the hub's NAL.
func serveNAL(t *testing.T, n *protocol.NAL) string {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(n)
	}))
	t.Cleanup(ts.Close)
	return ts.URL
}

func browseNAL(t *testing.T, ks *keystore.Keystore) *protocol.NAL {
	t.Helper()
	n := &protocol.NAL{V3NetNAL: "1.0", Network: "felnet", Areas: []protocol.Area{{Tag: "fel.general", Name: "General"}}}
	if err := nal.Sign(n, ks); err != nil {
		t.Fatal(err)
	}
	return n
}

// The area browser lists a NAL only if it is validly signed, for the right
// network, has a matching coordinator node ID and, when a key is pinned, is
// signed by that key.
func TestFetchHubNALVerifies(t *testing.T) {
	trusted, other := browseKey(t), browseKey(t)
	good := browseNAL(t, trusted)
	url := serveNAL(t, good)

	msg := fetchHubNAL(url, "felnet", trusted.PubKeyBase64())().(fetchNALMsg)
	if msg.err != nil || len(msg.areas) != 1 || msg.coordKey != trusted.PubKeyBase64() || msg.hubURL != url || msg.network != "felnet" {
		t.Fatalf("pinned fetch = %+v", msg)
	}
	if msg := fetchHubNAL(url, "felnet", "")().(fetchNALMsg); msg.err != nil || msg.coordKey != trusted.PubKeyBase64() {
		t.Errorf("unpinned fetch = %+v", msg)
	}
	if msg := fetchHubNAL(url, "felnet", other.PubKeyBase64())().(fetchNALMsg); !errors.Is(msg.err, nal.ErrCoordKeyMismatch) || len(msg.areas) != 0 {
		t.Errorf("fetch pinned to another key = %+v, want ErrCoordKeyMismatch and no areas", msg)
	}
	if msg := fetchHubNAL(url, "othernet", "")().(fetchNALMsg); msg.err == nil {
		t.Error("accepted a NAL for another network")
	}

	tampered := *good
	tampered.Areas = []protocol.Area{{Tag: "fel.evil", Name: "Evil"}}
	if msg := fetchHubNAL(serveNAL(t, &tampered), "felnet", "")().(fetchNALMsg); !errors.Is(msg.err, nal.ErrInvalidSignature) {
		t.Errorf("tampered fetch = %+v, want ErrInvalidSignature", msg)
	}

	wrongID := browseNAL(t, trusted)
	wrongID.CoordNodeID = "0123456789abcdef" // breaks the signature too
	if msg := fetchHubNAL(serveNAL(t, wrongID), "felnet", "")().(fetchNALMsg); msg.err == nil {
		t.Error("accepted a NAL with a foreign coordinator node ID")
	}
}

// The join wizard pins the registry's keys, refuses to save when the areas
// were chosen from a NAL signed by a different key, and pins the browsed
// key when the registry publishes none.
func TestLeafWizardPinsVerifiedKeys(t *testing.T) {
	regKey, hubKey, otherKey := browseKey(t), browseKey(t), browseKey(t)
	open := func(t *testing.T) (Model, string) {
		m, dir := newDiskModel(t)
		makeKey(t, m.configs.V3Net.KeystorePath)
		m = press(t, m, "6", "2", "i")
		m.wizard.hubURL = "https://hub.example"
		m.wizard.networkName = "felnet"
		m.wizard.selectedAreas = []areaBrowserItem{{Tag: "fel.general", Name: "General", Subscribed: true}}
		return m, dir
	}
	entry := &protocol.RegistryEntry{
		Name: "felnet", HubURL: "https://hub.example",
		CoordPubKeyB64: regKey.PubKeyBase64(), HubPubKeyB64: hubKey.PubKeyBase64(),
	}
	browsed := func(m Model, key string) {
		m.wizard.browsedCoordKey, m.wizard.browsedHub, m.wizard.browsedNetwork = key, "https://hub.example", "felnet"
	}

	t.Run("mismatch refuses to save", func(t *testing.T) {
		m, dir := open(t)
		m.wizard.regEntry = entry
		browsed(m, otherKey.PubKeyBase64())
		m = press(t, m, "S")
		if !strings.HasPrefix(m.message, "NOT SAVED") {
			t.Fatalf("message = %q, want a refusal", m.message)
		}
		if ac := reloadConfigs(t, dir); len(ac.V3Net.Leaves) != 0 {
			t.Errorf("saved %+v despite the key mismatch", ac.V3Net.Leaves)
		}
	})

	t.Run("registry keys pinned", func(t *testing.T) {
		m, dir := open(t)
		m.wizard.regEntry = entry
		browsed(m, regKey.PubKeyBase64())
		m = press(t, m, "S")
		ac := reloadConfigs(t, dir)
		if len(ac.V3Net.Leaves) != 1 {
			t.Fatalf("leaves = %+v, msg %q", ac.V3Net.Leaves, m.message)
		}
		if l := ac.V3Net.Leaves[0]; l.CoordinatorPubKey != regKey.PubKeyBase64() || l.HubPubKey != hubKey.PubKeyBase64() {
			t.Errorf("leaf pins = %q / %q", l.CoordinatorPubKey, l.HubPubKey)
		}
	})

	t.Run("browsed key pinned without a registry key", func(t *testing.T) {
		m, dir := open(t)
		browsed(m, otherKey.PubKeyBase64())
		m = press(t, m, "S")
		ac := reloadConfigs(t, dir)
		if len(ac.V3Net.Leaves) != 1 || ac.V3Net.Leaves[0].CoordinatorPubKey != otherKey.PubKeyBase64() {
			t.Errorf("leaves = %+v, msg %q", ac.V3Net.Leaves, m.message)
		}
	})
}
