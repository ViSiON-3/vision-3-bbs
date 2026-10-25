package config

import "testing"

// TestTwitListsOnDefaultsAndPersists checks the board-wide Twit List switch:
// an older config.json without the key keeps the feature on, and an explicit
// value survives a save and reload.
func TestTwitListsOnDefaultsAndPersists(t *testing.T) {
	cfg, err := ParseServerConfig([]byte(`{"boardName":"Old Board"}`))
	if err != nil || !cfg.TwitListsOn() {
		t.Fatalf("missing key: on=%v err=%v", cfg.TwitListsOn(), err)
	}
	if !(ServerConfig{}).TwitListsOn() {
		t.Fatal("zero config should leave Twit Lists on")
	}
	for _, want := range []bool{false, true} {
		dir := t.TempDir()
		cfg.TwitListEnabled = &want
		if err := SaveServerConfig(dir, cfg); err != nil {
			t.Fatal(err)
		}
		got, err := LoadServerConfig(dir)
		if err != nil || got.TwitListsOn() != want {
			t.Fatalf("saved %v, loaded %v (err %v)", want, got.TwitListsOn(), err)
		}
	}
}
