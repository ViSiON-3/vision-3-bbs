package configeditor

import (
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
)

func TestBuildBinkdRegenDerivesEverythingFromConfig(t *testing.T) {
	ftnCfg := config.FTNConfig{
		Networks: map[string]config.FTNNetworkConfig{
			"fsxnet": {
				OwnAddress: "21:4/999",
				Links: []config.FTNLinkConfig{{
					Address: "21:4/158", SessionPassword: "s3cret",
					Hostname: "pointhub.example.org", Port: 24556,
				}},
			},
			// A link without a hostname cannot produce a node line.
			"othernet": {
				OwnAddress: "1:2/3",
				Links:      []config.FTNLinkConfig{{Address: "1:2/1"}},
			},
		},
	}
	server := config.ServerConfig{BoardName: "Test Board", SysOpName: "Sysop", BBSLocation: "Testville"}

	cfg, nodes, ok := buildBinkdRegen(ftnCfg, server, "/real/root")
	if !ok {
		t.Fatal("expected regeneration data to be available")
	}
	if cfg.Domains["fsxnet"] != 21 || cfg.Domains["othernet"] != 1 {
		t.Errorf("zones not derived from own addresses: %v", cfg.Domains)
	}
	found := false
	for _, a := range cfg.Addresses {
		if a == "21:4/999@fsxnet" {
			found = true
		}
	}
	if !found {
		t.Errorf("own address missing: %v", cfg.Addresses)
	}
	if len(nodes) != 1 {
		t.Fatalf("want 1 node (only links with hostnames), got %d: %v", len(nodes), nodes)
	}
	n := nodes[0]
	if n.Address != "21:4/158@fsxnet" || n.Hostname != "pointhub.example.org:24556" || n.SessionPwd != "s3cret" {
		t.Errorf("node not built from link: %+v", n)
	}
	if cfg.BoardName != "Test Board" {
		t.Errorf("identity not carried: %+v", cfg)
	}
}

func TestBuildBinkdRegenNoNetworksNotOK(t *testing.T) {
	_, _, ok := buildBinkdRegen(config.FTNConfig{}, config.ServerConfig{}, "/root")
	if ok {
		t.Error("no configured networks must not claim regeneration data")
	}
}

func TestBuildBinkdRegenBadOwnAddressSkipped(t *testing.T) {
	ftnCfg := config.FTNConfig{
		Networks: map[string]config.FTNNetworkConfig{
			"badnet": {OwnAddress: "notanaddress"},
		},
	}
	_, _, ok := buildBinkdRegen(ftnCfg, config.ServerConfig{}, "/root")
	if ok {
		t.Error("network with unparseable own address must not count")
	}
}
