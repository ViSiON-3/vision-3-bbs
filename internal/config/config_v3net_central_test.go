package config

import (
	"encoding/json"
	"strings"
	"testing"
)

// A config written before Central has no central block, and every switch
// reads off; a block with only one switch set leaves the other off.
func TestV3NetCentralConfigDefaultsOff(t *testing.T) {
	cfg, err := ParseV3NetConfig([]byte(`{"enabled":true,"leaves":[{"hubUrl":"https://hub.example","network":"testnet","boards":["t.gen"]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	l := cfg.Leaves[0]
	if l.Central != nil || l.CentralEnabled() || l.CentralPublishes() {
		t.Errorf("central should be off on an old config: %+v", l.Central)
	}

	cfg, err = ParseV3NetConfig([]byte(`{"leaves":[{"hubUrl":"h","network":"n","central":{"publishListing":true,"description":"A board","addresses":["ssh://x.example:22"]}}]}`))
	if err != nil {
		t.Fatal(err)
	}
	l = cfg.Leaves[0]
	if l.CentralEnabled() || !l.CentralPublishes() || l.Central.Description != "A board" || len(l.Central.Addresses) != 1 {
		t.Errorf("central block: %+v", l.Central)
	}
}

// Saving a config without a central block does not invent one, so an
// upgrade changes nothing in the file; a block that was set is kept.
func TestV3NetCentralConfigRoundTrip(t *testing.T) {
	cfg := V3NetConfig{Leaves: []V3NetLeafConfig{{HubURL: "h", Network: "n"}}}
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "central") {
		t.Errorf("unset central block written: %s", data)
	}
	cfg.Leaves[0].Central = &V3NetCentralConfig{Enabled: true}
	data, _ = json.Marshal(cfg)
	back, err := ParseV3NetConfig(data)
	if err != nil {
		t.Fatal(err)
	}
	if !back.Leaves[0].CentralEnabled() || back.Leaves[0].CentralPublishes() {
		t.Errorf("round trip: %+v", back.Leaves[0].Central)
	}
}
