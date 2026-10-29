package v3net

import (
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/leaf"
)

// persistKeyEvent writes first-use pins and offered keys into the matching
// leaf entry of v3net.json, never overwrites a pin, and drops the pending
// key once it has become the pin.
func TestPersistKeyEvent(t *testing.T) {
	dir := t.TempDir()
	if err := config.SaveV3NetConfig(dir, config.V3NetConfig{Leaves: []config.V3NetLeafConfig{
		{HubURL: "https://other.example", Network: "testnet"},
		{HubURL: "https://hub.example", Network: "testnet", HubPubKey: "sysop-set"},
	}}); err != nil {
		t.Fatal(err)
	}
	s := &Service{cfg: config.V3NetConfig{ConfigPath: dir}}
	load := func() config.V3NetLeafConfig {
		t.Helper()
		cfg, err := config.LoadV3NetConfig(dir)
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Leaves[0].CoordinatorPubKey != "" || cfg.Leaves[0].PendingCoordinatorPubKey != "" {
			t.Errorf("the other hub's leaf was changed: %+v", cfg.Leaves[0])
		}
		return cfg.Leaves[1]
	}
	ev := func(kind leaf.KeyKind, pinned, offered string) leaf.KeyEvent {
		return leaf.KeyEvent{Network: "testnet", HubURL: "https://hub.example", Kind: kind, Pinned: pinned, Offered: offered}
	}

	s.persistKeyEvent(ev(leaf.KeyCoordinator, "A", "A"))
	s.persistKeyEvent(ev(leaf.KeyHub, "B", "B")) // already pinned by the sysop
	got := load()
	if got.CoordinatorPubKey != "A" || got.HubPubKey != "sysop-set" {
		t.Fatalf("after first use: coord %q, hub %q", got.CoordinatorPubKey, got.HubPubKey)
	}

	s.persistKeyEvent(ev(leaf.KeyCoordinator, "A", "C"))
	if got = load(); got.CoordinatorPubKey != "A" || got.PendingCoordinatorPubKey != "C" {
		t.Fatalf("after change: coord %q, pending %q; want A and C", got.CoordinatorPubKey, got.PendingCoordinatorPubKey)
	}

	// The sysop pins C by hand; the leaf reports it in use.
	if err := config.UpdateV3NetConfig(dir, func(c *config.V3NetConfig) error {
		c.Leaves[1].CoordinatorPubKey = "C"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	s.persistKeyEvent(ev(leaf.KeyCoordinator, "C", "C"))
	if got = load(); got.CoordinatorPubKey != "C" || got.PendingCoordinatorPubKey != "" {
		t.Errorf("after acceptance: coord %q, pending %q; want C and none", got.CoordinatorPubKey, got.PendingCoordinatorPubKey)
	}

	// No config path: nothing to write, nothing to fail.
	(&Service{}).persistKeyEvent(ev(leaf.KeyCoordinator, "Z", "Z"))
}
