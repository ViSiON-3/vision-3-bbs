package configeditor

import (
	"crypto/ed25519"
	"encoding/base64"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
)

func TestV3NetPinFieldValidatesAndClearsPending(t *testing.T) {
	pub, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	key := base64.StdEncoding.EncodeToString(pub)
	pin, pending := "old", key
	f := v3netPinField("Coord Key", "help", 9, &pin, &pending)

	if err := f.Set("not a key"); err == nil {
		t.Error("accepted a value that is not a base64 ed25519 key")
	}
	if pin != "old" || pending != key {
		t.Errorf("a rejected value changed state: pin %q, pending %q", pin, pending)
	}

	if err := f.Set("  " + key + " "); err != nil {
		t.Fatalf("Set(valid key): %v", err)
	}
	if pin != key || pending != "" {
		t.Errorf("after accepting: pin %q, pending %q; want the key and none", pin, pending)
	}

	pending = "offered"
	if err := f.Set(""); err != nil {
		t.Fatalf("Set(blank): %v", err)
	}
	if pin != "" || pending != "" {
		t.Errorf("after blanking: pin %q, pending %q; want both blank", pin, pending)
	}

	g := v3netPendingField("Offered Coord", 11, &pending)
	if got := g.Get(); got != "(none)" {
		t.Errorf("empty pending shows %q", got)
	}
}

func TestV3NetLeafSignedMessagesField(t *testing.T) {
	m := Model{configs: &allConfigs{}}
	m.configs.V3Net.Leaves = []config.V3NetLeafConfig{{HubURL: "http://hub.example", Network: "testnet"}}
	f := findField(t, m.fieldsV3NetLeaf(), "Signed Messages")

	if got := f.Get(); got != "N" {
		t.Fatalf("default = %q, want N", got)
	}
	if err := f.Set("Y"); err != nil {
		t.Fatal(err)
	}
	if !m.configs.V3Net.Leaves[0].RequireSignedMessages {
		t.Error("Set(Y) did not enable requireSignedMessages")
	}
	if err := f.Set("N"); err != nil {
		t.Fatal(err)
	}
	if m.configs.V3Net.Leaves[0].RequireSignedMessages {
		t.Error("Set(N) did not disable requireSignedMessages")
	}
}

func TestV3NetLeafSignedHubFields(t *testing.T) {
	m := Model{configs: &allConfigs{}}
	m.configs.V3Net.Leaves = []config.V3NetLeafConfig{{HubURL: "http://hub.example", Network: "testnet", HubSigned: true}}
	l := &m.configs.V3Net.Leaves[0]
	require := findField(t, m.fieldsV3NetLeaf(), "Signed Hub")
	signs := findField(t, m.fieldsV3NetLeaf(), "Hub Signs")

	if got := require.Get(); got != "N" {
		t.Fatalf("Signed Hub default = %q, want N", got)
	}
	if err := require.Set("Y"); err != nil {
		t.Fatal(err)
	}
	if !l.RequireSignedHub {
		t.Error("Set(Y) did not enable requireSignedHub")
	}
	if err := require.Set("N"); err != nil {
		t.Fatal(err)
	}
	if l.RequireSignedHub {
		t.Error("Set(N) did not disable requireSignedHub")
	}

	// Clearing the leaf's record is the recovery from a hub that stopped
	// signing.
	if got := signs.Get(); got != "Y" {
		t.Fatalf("Hub Signs = %q, want Y", got)
	}
	if err := signs.Set("N"); err != nil {
		t.Fatal(err)
	}
	if l.HubSigned {
		t.Error("Set(N) did not clear hubSigned")
	}
	if l.HubSignedClears != 1 {
		t.Errorf("hubSignedClears = %d after Set(N), want 1", l.HubSignedClears)
	}

	// Setting it to N again is a clear again; setting it to Y is not one.
	if err := signs.Set("N"); err != nil {
		t.Fatal(err)
	}
	if err := signs.Set("Y"); err != nil {
		t.Fatal(err)
	}
	if !l.HubSigned || l.HubSignedClears != 2 {
		t.Errorf("hubSigned = %v, hubSignedClears = %d; want true and 2", l.HubSigned, l.HubSignedClears)
	}
}
