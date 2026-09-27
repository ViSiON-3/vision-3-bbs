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
	// First by name keeps the shared outbound so its queued mail still goes out.
	if p := c.Networks["agoranet"].BinkdOutboundPath; p != "" {
		t.Errorf("agoranet should keep the global outbound, got %q", p)
	}
	for name, want := range map[string]string{
		"fsxnet":  "data/ftn/out_fsxnet",
		"zeronet": "data/ftn/out_zeronet",
		"custom":  "data/ftn/mine",
	} {
		if p := c.Networks[name].BinkdOutboundPath; p != filepath.FromSlash(want) {
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
