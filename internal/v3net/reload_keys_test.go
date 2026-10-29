package v3net_test

import (
	"path/filepath"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/keystore"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/leaf"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/nal"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

func reloadTestKey(t *testing.T) *keystore.Keystore {
	t.Helper()
	ks, _, err := keystore.Load(filepath.Join(t.TempDir(), "coord.key"))
	if err != nil {
		t.Fatal(err)
	}
	return ks
}

func reloadTestNAL(t *testing.T, ks *keystore.Keystore, network, tag string) *protocol.NAL {
	t.Helper()
	n := &protocol.NAL{V3NetNAL: "1.0", Network: network, Areas: []protocol.Area{{Tag: tag, Name: tag}}}
	if err := nal.Sign(n, ks); err != nil {
		t.Fatal(err)
	}
	return n
}

// A reload that only changes pinned or pending keys (the leaf recording a
// key it was offered, or the sysop accepting one) updates the running leaf
// in place: no rebuild, and the last accepted NAL stays. A reload that
// changes anything else rebuilds the leaf but carries that NAL over while
// the coordinator pin is unchanged.
func TestReloadLeavesKeyOnlyChangeKeepsLeafAndNAL(t *testing.T) {
	svc, mm := newReloadFixture(t, "ALPHA")
	oldKey, newKey := reloadTestKey(t), reloadTestKey(t)

	cfg := leafCfg("net-a", "ALPHA")
	cfg.CoordinatorPubKey = oldKey.PubKeyBase64()
	svc.ConfigureLeaves([]config.V3NetLeafConfig{cfg}, mm)
	first := svc.Leaves()[0]
	if !first.SeedNAL(reloadTestNAL(t, oldKey, "net-a", "gen.trusted")) {
		t.Fatal("could not seed the leaf with a NAL signed by its pinned key")
	}

	// The leaf records the key the hub offered: pending only.
	cfg.PendingCoordinatorPubKey = newKey.PubKeyBase64()
	if err := svc.ReloadLeaves([]config.V3NetLeafConfig{cfg}, mm, nil); err != nil {
		t.Fatal(err)
	}
	if got := svc.Leaves()[0]; got != first {
		t.Fatal("a pending-key-only edit rebuilt the leaf")
	}
	if n := first.LastNAL(); n == nil || n.Areas[0].Tag != "gen.trusted" {
		t.Errorf("last accepted NAL lost: %+v", n)
	}
	if got := first.PinnedKey(leaf.KeyCoordinator); got != oldKey.PubKeyBase64() {
		t.Errorf("pin changed to %q", got)
	}

	// The sysop accepts the new key: applied to the same leaf.
	cfg.CoordinatorPubKey = newKey.PubKeyBase64()
	if err := svc.ReloadLeaves([]config.V3NetLeafConfig{cfg}, mm, nil); err != nil {
		t.Fatal(err)
	}
	if got := svc.Leaves()[0]; got != first {
		t.Fatal("accepting a key rebuilt the leaf")
	}
	if got := first.PinnedKey(leaf.KeyCoordinator); got != newKey.PubKeyBase64() {
		t.Errorf("accepted pin not applied: %q", got)
	}

	// Any other edit rebuilds, carrying the NAL the old leaf accepted when
	// it is signed by the (unchanged) pin.
	first.SeedNAL(reloadTestNAL(t, newKey, "net-a", "gen.accepted"))
	cfg.Origin = "Changed origin"
	if err := svc.ReloadLeaves([]config.V3NetLeafConfig{cfg}, mm, nil); err != nil {
		t.Fatal(err)
	}
	second := svc.Leaves()[0]
	if second == first {
		t.Fatal("an origin edit did not rebuild the leaf")
	}
	if n := second.LastNAL(); n == nil || n.Areas[0].Tag != "gen.accepted" {
		t.Errorf("rebuilt leaf did not inherit the accepted NAL: %+v", n)
	}

	// A rebuild that also moves the pin does not carry a NAL signed by the
	// old key.
	cfg.CoordinatorPubKey = oldKey.PubKeyBase64()
	cfg.Origin = "Again"
	if err := svc.ReloadLeaves([]config.V3NetLeafConfig{cfg}, mm, nil); err != nil {
		t.Fatal(err)
	}
	if n := svc.Leaves()[0].LastNAL(); n != nil {
		t.Errorf("carried a NAL signed by a key that is no longer pinned: %+v", n)
	}
}

// hubSigned is handed to the leaf it configures and, being the leaf's own
// record, a change to it alone (the leaf storing it, or the sysop clearing
// it after a hub stopped signing) is applied in place. requireSignedHub is
// a setting and rebuilds the leaf like any other.
func TestReloadLeavesHubSignedInPlace(t *testing.T) {
	svc, mm := newReloadFixture(t, "ALPHA")
	cfg := leafCfg("net-a", "ALPHA")
	cfg.HubSigned = true
	svc.ConfigureLeaves([]config.V3NetLeafConfig{cfg}, mm)
	first := svc.Leaves()[0]
	if !first.HubSigned() {
		t.Fatal("hubSigned from the config did not reach the leaf")
	}

	cfg.HubSigned = false
	if err := svc.ReloadLeaves([]config.V3NetLeafConfig{cfg}, mm, nil); err != nil {
		t.Fatal(err)
	}
	if got := svc.Leaves()[0]; got != first {
		t.Fatal("clearing hubSigned rebuilt the leaf")
	}
	if first.HubSigned() {
		t.Error("clearing hubSigned in the config did not reach the running leaf")
	}

	cfg.RequireSignedHub = true
	if err := svc.ReloadLeaves([]config.V3NetLeafConfig{cfg}, mm, nil); err != nil {
		t.Fatal(err)
	}
	if got := svc.Leaves()[0]; got == first {
		t.Error("turning on requireSignedHub did not rebuild the leaf")
	}
}
