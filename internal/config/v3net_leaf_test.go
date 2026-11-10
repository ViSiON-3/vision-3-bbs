package config

import (
	"encoding/json"
	"testing"
)

func TestV3NetLeafAutoJoinEnabledDefaultsTrue(t *testing.T) {
	var l V3NetLeafConfig
	if err := json.Unmarshal([]byte(`{"hubUrl":"http://x","network":"felonynet"}`), &l); err != nil {
		t.Fatal(err)
	}
	if !l.AutoJoinEnabled() {
		t.Error("missing autoJoinAreas must default to enabled")
	}

	f := false
	l.AutoJoinAreas = &f
	if l.AutoJoinEnabled() {
		t.Error("explicit false must disable auto-join")
	}
}

// The rolling-upgrade keys of releases before signatures were required are
// ignored: a v3net.json that still holds them loads, and nothing reads them.
func TestV3NetLeafIgnoresRemovedSigningKeys(t *testing.T) {
	var l V3NetLeafConfig
	if err := json.Unmarshal([]byte(`{"hubUrl":"http://x","network":"felonynet","requireSignedHub":true,"hubSigned":true,"hubSignedClears":2}`), &l); err != nil {
		t.Fatal(err)
	}
	if l.HubURL != "http://x" || l.Network != "felonynet" {
		t.Errorf("leaf = %+v, want the known fields read", l)
	}
}
