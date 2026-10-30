package v3net

import (
	"errors"
	"reflect"
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

	// A handover moves the pin it started from, and clears a pending key
	// it made the pin.
	s.persistKeyEvent(ev(leaf.KeyCoordinator, "C", "D"))
	moved := ev(leaf.KeyCoordinator, "D", "D")
	moved.Previous = "C"
	s.persistKeyEvent(moved)
	if got = load(); got.CoordinatorPubKey != "D" || got.PendingCoordinatorPubKey != "" {
		t.Errorf("after handover: coord %q, pending %q; want D and none", got.CoordinatorPubKey, got.PendingCoordinatorPubKey)
	}
	// One that started from a pin the sysop has since replaced does not.
	moved = ev(leaf.KeyHub, "E", "E")
	moved.Previous = "not-the-pin"
	s.persistKeyEvent(moved)
	if got = load(); got.HubPubKey != "sysop-set" {
		t.Errorf("stale handover overwrote the hub pin: %q", got.HubPubKey)
	}

	// No config path: nothing to write, nothing to fail.
	(&Service{}).persistKeyEvent(ev(leaf.KeyCoordinator, "Z", "Z"))
}

// persistHubSigned sets hubSigned in the matching leaf entry of v3net.json
// only, and leaves it alone once set.
func TestPersistHubSigned(t *testing.T) {
	dir := t.TempDir()
	if err := config.SaveV3NetConfig(dir, config.V3NetConfig{Leaves: []config.V3NetLeafConfig{
		{HubURL: "https://other.example", Network: "testnet"},
		{HubURL: "https://hub.example", Network: "testnet"},
	}}); err != nil {
		t.Fatal(err)
	}
	s := &Service{cfg: config.V3NetConfig{ConfigPath: dir}}
	for range 2 {
		if err := s.persistHubSigned("testnet", "https://hub.example"); err != nil {
			t.Fatalf("persistHubSigned: %v", err)
		}
	}
	cfg, err := config.LoadV3NetConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Leaves[0].HubSigned || !cfg.Leaves[1].HubSigned {
		t.Errorf("hubSigned = %v, %v; want only the matching leaf set", cfg.Leaves[0].HubSigned, cfg.Leaves[1].HubSigned)
	}

	// No config path: nothing to write, nothing to fail.
	if err := (&Service{}).persistHubSigned("testnet", "https://hub.example"); err != nil {
		t.Errorf("persistHubSigned without a config path: %v", err)
	}
}

// hubSignedStored reads the matching entry's hubSigned and count of clears from
// v3net.json as it is when called.
func TestHubSignedStored(t *testing.T) {
	dir := t.TempDir()
	s := &Service{cfg: config.V3NetConfig{ConfigPath: dir}}
	stored := s.hubSignedStored("testnet", "https://hub.example")
	if got, err := stored(); !errors.Is(err, leaf.ErrNoStoredEntry) {
		t.Fatalf("with no v3net.json: %+v, %v; want no entry", got, err)
	}
	if err := config.SaveV3NetConfig(dir, config.V3NetConfig{Leaves: []config.V3NetLeafConfig{
		{HubURL: "https://other.example", Network: "testnet", HubSigned: true},
		{HubURL: "https://hub.example", Network: "testnet", HubSignedClears: 2},
	}}); err != nil {
		t.Fatal(err)
	}
	if got, err := stored(); err != nil || got != (leaf.StoredHubSigned{Clears: 2}) {
		t.Fatalf("before the store: %+v, %v; want unset with 2 clears", got, err)
	}
	if err := s.persistHubSigned("testnet", "https://hub.example"); err != nil {
		t.Fatal(err)
	}
	if got, err := stored(); err != nil || got != (leaf.StoredHubSigned{Signed: true, Clears: 2}) {
		t.Errorf("after the store: %+v, %v; want set with 2 clears", got, err)
	}
	other := s.hubSignedStored("othernet", "https://hub.example")
	if got, err := other(); !errors.Is(err, leaf.ErrNoStoredEntry) {
		t.Errorf("with no entry: %+v, %v; want no entry", got, err)
	}
	if (&Service{}).hubSignedStored("testnet", "https://hub.example") != nil {
		t.Error("a service without a config path has a store to read")
	}
}

// pinStored reads the matching entry's pins from v3net.json as it is when
// called.
func TestPinStored(t *testing.T) {
	dir := t.TempDir()
	s := &Service{cfg: config.V3NetConfig{ConfigPath: dir}}
	stored := s.pinStored("testnet", "https://hub.example")
	if got, err := stored(leaf.KeyCoordinator); !errors.Is(err, leaf.ErrNoStoredEntry) {
		t.Fatalf("with no v3net.json: %+v, %v; want no entry", got, err)
	}
	if err := config.SaveV3NetConfig(dir, config.V3NetConfig{Leaves: []config.V3NetLeafConfig{
		{HubURL: "https://other.example", Network: "testnet", CoordinatorPubKey: "other"},
		{
			HubURL: "https://hub.example", Network: "testnet",
			CoordinatorPubKey: "coord", PendingCoordinatorPubKey: "coord-pending",
			HubPubKey: "hub", PendingHubPubKey: "hub-pending",
		},
	}}); err != nil {
		t.Fatal(err)
	}
	if got, err := stored(leaf.KeyCoordinator); err != nil || got != (leaf.StoredPin{Pin: "coord", Pending: "coord-pending"}) {
		t.Errorf("coordinator pin = %+v, %v", got, err)
	}
	if got, err := stored(leaf.KeyHub); err != nil || got != (leaf.StoredPin{Pin: "hub", Pending: "hub-pending"}) {
		t.Errorf("hub pin = %+v, %v", got, err)
	}
	other := s.pinStored("othernet", "https://hub.example")
	if got, err := other(leaf.KeyCoordinator); !errors.Is(err, leaf.ErrNoStoredEntry) {
		t.Errorf("with no entry: %+v, %v; want no entry", got, err)
	}
	if (&Service{}).pinStored("testnet", "https://hub.example") != nil {
		t.Error("a service without a config path has a store to read")
	}
}

// keyPinned tells the key observer about a pin a handover moved, once it is
// stored, and stays quiet for a first-use pin.
func TestKeyPinnedNotifiesHandover(t *testing.T) {
	dir := t.TempDir()
	if err := config.SaveV3NetConfig(dir, config.V3NetConfig{Leaves: []config.V3NetLeafConfig{
		{HubURL: "https://hub.example", Network: "testnet"},
	}}); err != nil {
		t.Fatal(err)
	}
	s := &Service{cfg: config.V3NetConfig{ConfigPath: dir}}
	var seen []leaf.KeyEvent
	s.SetKeyChangeObserver(func(ev leaf.KeyEvent) { seen = append(seen, ev) })
	ev := leaf.KeyEvent{Network: "testnet", HubURL: "https://hub.example", Kind: leaf.KeyCoordinator, Pinned: "A", Offered: "A"}

	if err := s.keyPinned(ev); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 0 {
		t.Fatalf("first-use pin notified the observer: %+v", seen)
	}
	ev.Pinned, ev.Offered, ev.Previous = "B", "B", "A"
	if err := s.keyPinned(ev); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 1 || seen[0].Previous != "A" || seen[0].Pinned != "B" {
		t.Errorf("observer saw %+v, want the move from A to B", seen)
	}
}

// A reload's baseline goes back only for the fields it could not check, so a clear
// it applied is not seen as a clear again after a pin's read failed (#821).
func TestWithUncheckedFrom(t *testing.T) {
	before := config.V3NetLeafConfig{
		CoordinatorPubKey: "old-coord", PendingCoordinatorPubKey: "old-coord-pending",
		HubPubKey: "old-hub", PendingHubPubKey: "old-hub-pending",
		HubSigned: true, HubSignedClears: 1,
	}
	checked := config.V3NetLeafConfig{
		CoordinatorPubKey: "new-coord", PendingCoordinatorPubKey: "new-coord-pending",
		HubPubKey: "new-hub", PendingHubPubKey: "new-hub-pending",
		HubSignedClears: 2,
	}
	check := func(name string, p leaf.Pins, want config.V3NetLeafConfig) {
		t.Helper()
		if got := withUncheckedFrom(checked, before, p); !reflect.DeepEqual(got, want) {
			t.Errorf("%s: %+v, want %+v", name, got, want)
		}
	}
	want := before
	want.HubSigned, want.HubSignedClears = false, 2
	check("pins unread", leaf.Pins{CoordUnread: true, HubUnread: true}, want)
	want = checked
	want.HubSigned, want.HubSignedClears = true, 1
	check("hubSigned unread", leaf.Pins{HubSignedUnread: true}, want)
	check("all read", leaf.Pins{}, checked)
}
