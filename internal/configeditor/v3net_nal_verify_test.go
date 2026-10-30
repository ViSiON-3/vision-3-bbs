package configeditor

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
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

// handOver returns the signed handovers keys[0]->keys[1]->... for felnet.
func handOver(t *testing.T, keys ...*keystore.Keystore) []protocol.CoordHandover {
	t.Helper()
	var hs []protocol.CoordHandover
	for i := 0; i+1 < len(keys); i++ {
		h, err := protocol.NewCoordHandover(keys[i], "felnet", keys[i+1].PubKeyBase64(), time.Now())
		if err != nil {
			t.Fatal(err)
		}
		hs = append(hs, *h)
	}
	return hs
}

// After the network moved keys, the area browser accepts a NAL signed by
// the new key when its handovers lead there from the pin, over one step or
// several, and says how it got there. With no chain from the pin it is
// refused as before.
func TestFetchHubNALFollowsHandovers(t *testing.T) {
	a, b, c, other := browseKey(t), browseKey(t), browseKey(t), browseKey(t)
	pin := a.PubKeyBase64()

	moved := browseNAL(t, c)
	moved.Handovers = handOver(t, a, b, c)
	msg := fetchHubNAL(serveNAL(t, moved), "felnet", pin)().(fetchNALMsg)
	if msg.err != nil || len(msg.areas) != 1 {
		t.Fatalf("fetch along A->B->C = %+v", msg)
	}
	if msg.coordKey != c.PubKeyBase64() || msg.handedOverFrom != pin || msg.handoverSteps != 2 {
		t.Errorf("fetch along A->B->C: coordKey %s, from %s, steps %d", msg.coordKey, msg.handedOverFrom, msg.handoverSteps)
	}
	// Signed by the pinned key itself, nothing is followed.
	if msg := fetchHubNAL(serveNAL(t, browseNAL(t, a)), "felnet", pin)().(fetchNALMsg); msg.err != nil || msg.handedOverFrom != "" {
		t.Errorf("fetch of the pinned key's NAL = %+v", msg)
	}

	gap := browseNAL(t, c)
	gap.Handovers = handOver(t, b, c) // nothing from the pin
	foreign := browseNAL(t, c)
	foreign.Handovers = handOver(t, a, c)
	foreign.Handovers[0].Network = "othernet" // breaks its signature too
	stray := browseNAL(t, c)
	stray.Handovers = handOver(t, a, other)
	refused := map[string]*protocol.NAL{
		"no handovers":                 browseNAL(t, c),
		"chain not from the pin":       gap,
		"handover for another network": foreign,
		"chain to another key":         stray,
	}
	for name, n := range refused {
		msg := fetchHubNAL(serveNAL(t, n), "felnet", pin)().(fetchNALMsg)
		if !errors.Is(msg.err, nal.ErrCoordKeyMismatch) || len(msg.areas) != 0 {
			t.Errorf("%s: fetch = %+v, want ErrCoordKeyMismatch and no areas", name, msg)
		}
	}
}

// The browser shows a handed-over NAL's areas and says the network moved
// keys, without touching the pin. In the join wizard the registry key the
// chain started at is what the new leaf pins, so saving is not refused; the
// leaf moves the pin itself at its first fetch.
func TestAreaBrowserHandedOverNAL(t *testing.T) {
	regKey, newCoord := browseKey(t), browseKey(t)
	moved := fetchNALMsg{
		areas:    []protocol.Area{{Tag: "fel.general", Name: "General"}},
		coordKey: newCoord.PubKeyBase64(), hubURL: "https://hub.example", network: "felnet",
		handedOverFrom: regKey.PubKeyBase64(), handoverSteps: 1,
	}

	t.Run("record edit", func(t *testing.T) {
		m, _ := newDiskModel(t)
		m.configs.V3Net.Leaves = append(m.configs.V3Net.Leaves, config.V3NetLeafConfig{
			HubURL: "https://hub.example", Network: "felnet", CoordinatorPubKey: regKey.PubKeyBase64(),
		})
		m.recordEditIdx = len(m.configs.V3Net.Leaves) - 1
		m.areaBrowserReturn, m.areaBrowserHub, m.areaBrowserNetwork = modeRecordEdit, "https://hub.example", "felnet"
		m = asModel(t, first(m.handleFetchNALMsg(moved)))
		if len(m.areaBrowserAreas) != 1 || m.areaBrowserError != "" {
			t.Fatalf("areas %+v, error %q", m.areaBrowserAreas, m.areaBrowserError)
		}
		if m.message != handoverNotice {
			t.Errorf("message = %q, want the handover notice", m.message)
		}
		if !strings.Contains(m.viewV3NetAreaBrowser(), handoverNotice) {
			t.Error("the browser does not show the handover notice")
		}
		if got := m.configs.V3Net.Leaves[m.recordEditIdx].CoordinatorPubKey; got != regKey.PubKeyBase64() {
			t.Errorf("pin changed to %s; the running leaf moves it", got)
		}
	})

	t.Run("join wizard", func(t *testing.T) {
		m, dir := newDiskModel(t)
		makeKey(t, m.configs.V3Net.KeystorePath)
		m = press(t, m, "6", "2", "i")
		m.wizard.hubURL, m.wizard.networkName = "https://hub.example", "felnet"
		m.wizard.regEntry = &protocol.RegistryEntry{Name: "felnet", HubURL: "https://hub.example", CoordPubKeyB64: regKey.PubKeyBase64()}
		m.areaBrowserHub, m.areaBrowserNetwork = "https://hub.example", "felnet"
		m = asModel(t, first(m.handleFetchNALMsg(moved)))
		if m.wizard.browsedCoordKey != regKey.PubKeyBase64() {
			t.Fatalf("browsed key = %s, want the registry key the chain started at", m.wizard.browsedCoordKey)
		}
		m.wizard.selectedAreas = []areaBrowserItem{{Tag: "fel.general", Name: "General", Subscribed: true}}
		m = press(t, m, "S")
		ac := reloadConfigs(t, dir)
		if len(ac.V3Net.Leaves) != 1 || ac.V3Net.Leaves[0].CoordinatorPubKey != regKey.PubKeyBase64() {
			t.Errorf("leaves = %+v, msg %q; want the registry key pinned", ac.V3Net.Leaves, m.message)
		}
	})
}
