package configeditor

import (
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
)

// Changing a leaf's hub URL drops the old hub's key pin and pending key, so
// the leaf pins the new hub's key instead of pausing on a mismatch, and
// keeps the coordinator pin, which belongs to the network.
func TestV3NetLeafHubURLChangeClearsHubKey(t *testing.T) {
	m := Model{configs: &allConfigs{}}
	m.configs.V3Net.Leaves = []config.V3NetLeafConfig{{
		HubURL: "https://old.example", Network: "testnet",
		CoordinatorPubKey: "coord", PendingCoordinatorPubKey: "coord-offered",
		HubPubKey: "hub", PendingHubPubKey: "hub-offered",
	}}
	f := findField(t, m.fieldsV3NetLeaf(), "Hub URL")

	// The same URL changes nothing.
	if err := f.Set("https://old.example"); err != nil {
		t.Fatal(err)
	}
	f.AfterSet(&m, "https://old.example")
	if l := m.configs.V3Net.Leaves[0]; l.HubPubKey != "hub" || l.PendingHubPubKey != "hub-offered" || m.message != "" {
		t.Errorf("same URL: leaf = %+v, message %q; want nothing changed", l, m.message)
	}

	if err := f.Set("https://new.example"); err != nil {
		t.Fatal(err)
	}
	f.AfterSet(&m, "https://new.example")
	l := m.configs.V3Net.Leaves[0]
	if l.HubURL != "https://new.example" || l.HubPubKey != "" || l.PendingHubPubKey != "" {
		t.Errorf("new URL: leaf = %+v; want the hub key and pending hub key cleared", l)
	}
	if l.CoordinatorPubKey != "coord" || l.PendingCoordinatorPubKey != "coord-offered" {
		t.Errorf("new URL: leaf = %+v; the coordinator keys must stay", l)
	}
	if m.message == "" {
		t.Error("no note that the hub key will be pinned again")
	}
}

// Filling in the URL of a new leaf is not a hub change.
func TestV3NetLeafHubURLFirstSetKeepsKeys(t *testing.T) {
	m := Model{configs: &allConfigs{}}
	m.configs.V3Net.Leaves = []config.V3NetLeafConfig{{Network: "testnet", HubPubKey: "hub"}}
	f := findField(t, m.fieldsV3NetLeaf(), "Hub URL")
	if err := f.Set("https://hub.example"); err != nil {
		t.Fatal(err)
	}
	if l := m.configs.V3Net.Leaves[0]; l.HubPubKey != "hub" {
		t.Errorf("leaf = %+v; a first URL must not clear keys", l)
	}
}
