package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadQWKNetConfig_MissingFileGivesDefaults(t *testing.T) {
	cfg, err := LoadQWKNetConfig(t.TempDir())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.InboundPath != DefaultQWKNetInboundPath || cfg.Networks == nil {
		t.Fatalf("defaults not applied: %+v", cfg)
	}
}

func TestQWKNetConfig_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	in := QWKNetConfig{Networks: map[string]QWKNetworkConfig{
		"dovenet": {Enabled: true, Name: "DOVE-Net", HubID: "VERT", Host: "vert.synchro.net", Password: "secret"},
	}}
	if err := SaveQWKNetConfig(dir, in); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(filepath.Join(dir, "qwknet.json"))
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm()&0o077 != 0 {
		t.Errorf("qwknet.json holds a password and must not be group/world readable, got %v", st.Mode().Perm())
	}
	out, err := LoadQWKNetConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	n := out.Networks["dovenet"]
	if n.HubID != "VERT" || n.HostPort() != "vert.synchro.net:21" || out.OutboundPath != DefaultQWKNetOutboundPath {
		t.Fatalf("round trip lost data: %+v", out)
	}
}

func TestQWKNetworkConfig_NodeIDAndLogin(t *testing.T) {
	n := QWKNetworkConfig{}
	if got := n.NodeID("vision3b"); got != "VISION3B" {
		t.Errorf("NodeID fallback = %q", got)
	}
	n.OwnID = "mynode"
	if got := n.NodeID("VISION3B"); got != "MYNODE" {
		t.Errorf("NodeID own = %q", got)
	}
	if got := n.LoginUser("MYNODE"); got != "MYNODE" {
		t.Errorf("LoginUser fallback = %q", got)
	}
	n.Username = "other"
	if got := n.LoginUser("MYNODE"); got != "other" {
		t.Errorf("LoginUser explicit = %q", got)
	}
}

func TestValidateQWKNetwork(t *testing.T) {
	good := QWKNetworkConfig{HubID: "VERT", Host: "h", Password: "p"}
	if err := ValidateQWKNetwork("dovenet", good, "VISION3"); err != nil {
		t.Fatalf("valid config rejected: %v", err)
	}
	cases := map[string]QWKNetworkConfig{
		"no hub":      {Host: "h", Password: "p"},
		"no host":     {HubID: "VERT", Password: "p"},
		"no password": {HubID: "VERT", Host: "h"},
		"bad port":    {HubID: "VERT", Host: "h", Password: "p", Port: 70000},
	}
	for name, c := range cases {
		if err := ValidateQWKNetwork("x", c, "VISION3"); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
	if err := ValidateQWKNetwork("x", good, ""); err == nil {
		t.Error("no system ID and no ownId should fail")
	}
}
