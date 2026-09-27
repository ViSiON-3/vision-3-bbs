package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNetworkOutboundPath(t *testing.T) {
	cases := []struct{ global, network, want string }{
		{"data/ftn/out", "fsxnet", "data/ftn/out_fsxnet"},
		{"", "zeronet", "data/ftn/out_zeronet"},
		{"/opt/v3/data/ftn/out/", "Agora.Net", "/opt/v3/data/ftn/out_agora_net"},
	}
	for _, c := range cases {
		got := NetworkOutboundPath(c.global, c.network)
		if got != filepath.FromSlash(c.want) {
			t.Errorf("NetworkOutboundPath(%q, %q) = %q, want %q", c.global, c.network, got, c.want)
		}
		if err := ValidateBinkdOutboundPath(got); err != nil {
			t.Errorf("%q fails binkd validation: %v", got, err)
		}
	}
}

func TestAssignSharedOutbounds(t *testing.T) {
	c := FTNConfig{
		BinkdOutboundPath: "data/ftn/out",
		Networks: map[string]FTNNetworkConfig{
			"fsxnet":   {},
			"agoranet": {},
			"zeronet":  {},
			"custom":   {BinkdOutboundPath: "data/ftn/mine"},
		},
	}
	got := c.AssignSharedOutbounds()
	if len(got) != 2 {
		t.Fatalf("assigned %d networks, want 2: %+v", len(got), got)
	}
	// First by name keeps the shared outbound so its queued mail still goes
	// out, and is pinned to it so the choice survives a save.
	if p := c.Networks["agoranet"].BinkdOutboundPath; p != "data/ftn/out" {
		t.Errorf("agoranet should keep the global outbound, got %q", p)
	}
	for name, want := range map[string]string{
		"fsxnet":  "data/ftn/out_fsxnet",
		"zeronet": "data/ftn/out_zeronet",
		"custom":  "data/ftn/mine",
	} {
		if p := c.Networks[name].BinkdOutboundPath; filepath.ToSlash(p) != want {
			t.Errorf("%s outbound = %q, want %q", name, p, want)
		}
	}
	if again := c.AssignSharedOutbounds(); again != nil {
		t.Errorf("second pass reassigned %+v", again)
	}
}

func TestAssignSharedOutboundsSingleNetworkUntouched(t *testing.T) {
	c := FTNConfig{Networks: map[string]FTNNetworkConfig{"fsxnet": {}}}
	if got := c.AssignSharedOutbounds(); got != nil {
		t.Errorf("single network reassigned: %+v", got)
	}
}

func TestLoadFTNConfigSplitsSharedOutbounds(t *testing.T) {
	dir := t.TempDir()
	body := `{"binkd_outbound_path":"data/ftn/out","networks":{"fsxnet":{"own_address":"21:1/1"},"zeronet":{"own_address":"99:1/1"}}}`
	if err := os.WriteFile(filepath.Join(dir, "ftn.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := LoadFTNConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	if a, b := c.BinkdOutboundFor("fsxnet"), c.BinkdOutboundFor("zeronet"); a == b {
		t.Errorf("fsxnet and zeronet still share outbound %q", a)
	}
}

// Once the keeper's path is saved, a network added later without a path must
// not take the global outbound over, even if it sorts first.
func TestAssignSharedOutboundsKeepsPinnedKeeper(t *testing.T) {
	c := FTNConfig{
		BinkdOutboundPath: "data/ftn/out",
		Networks: map[string]FTNNetworkConfig{
			"zeronet":  {BinkdOutboundPath: "data/ftn/out/"},
			"agoranet": {},
		},
	}
	got := c.AssignSharedOutbounds()
	if len(got) != 1 || got[0].Network != "agoranet" || got[0].KeptBy != "zeronet" {
		t.Fatalf("assigned %+v, want agoranet moved and zeronet kept", got)
	}
	if p := filepath.ToSlash(c.Networks["agoranet"].BinkdOutboundPath); p != "data/ftn/out_agoranet" {
		t.Errorf("agoranet outbound = %q", p)
	}
}

// A disabled or placeholder network has no mail queued, so it never keeps the
// global outbound from a working one.
func TestAssignSharedOutboundsPrefersWorkingNetwork(t *testing.T) {
	c := FTNConfig{Networks: map[string]FTNNetworkConfig{
		"aaa_placeholder": {},
		"zeronet":         {InternalTosserEnabled: true, OwnAddress: "99:1/1"},
	}}
	got := c.AssignSharedOutbounds()
	if len(got) != 1 || got[0].KeptBy != "zeronet" {
		t.Fatalf("assigned %+v, want zeronet to keep the global outbound", got)
	}
}

// A network that names the global outbound explicitly still shares it; when
// two do, one of them is moved.
func TestAssignSharedOutboundsSplitsExplicitAliases(t *testing.T) {
	c := FTNConfig{
		BinkdOutboundPath: "data/ftn/out",
		Networks: map[string]FTNNetworkConfig{
			"fsxnet":  {BinkdOutboundPath: "data/ftn/out"},
			"zeronet": {BinkdOutboundPath: "./data/ftn/out"},
		},
	}
	got := c.AssignSharedOutbounds()
	if len(got) != 1 || got[0].Network != "zeronet" {
		t.Fatalf("assigned %+v, want zeronet moved", got)
	}
	if c.BinkdOutboundFor("fsxnet") == c.BinkdOutboundFor("zeronet") {
		t.Error("fsxnet and zeronet still share an outbound")
	}
}

// Network names NetworkOutboundPath reduces to the same directory get
// different ones.
func TestAssignSharedOutboundsAvoidsDerivedCollisions(t *testing.T) {
	c := FTNConfig{
		BinkdOutboundPath: "data/ftn/out",
		Networks: map[string]FTNNetworkConfig{
			"alpha":   {},
			"foo.bar": {},
			"foo_bar": {},
			"other":   {BinkdOutboundPath: "data/ftn/out_foo_bar_2"},
		},
	}
	c.AssignSharedOutbounds()
	seen := map[string]string{}
	for name := range c.Networks {
		p := filepath.ToSlash(c.BinkdOutboundFor(name))
		if prev, dup := seen[p]; dup {
			t.Errorf("%s and %s share outbound %q", prev, name, p)
		}
		seen[p] = name
	}
}

func TestFreeNetworkOutboundPath(t *testing.T) {
	if p := FreeNetworkOutboundPath("data/ftn/out", "fsxnet", nil); filepath.ToSlash(p) != "data/ftn/out_fsxnet" {
		t.Errorf("free path = %q", p)
	}
	inUse := []string{"data/ftn/out_foo_bar", filepath.FromSlash("data/ftn/out_foo_bar_2")}
	if p := FreeNetworkOutboundPath("data/ftn/out", "foo.bar", inUse); filepath.ToSlash(p) != "data/ftn/out_foo_bar_3" {
		t.Errorf("path beside two taken = %q, want data/ftn/out_foo_bar_3", p)
	}
}
