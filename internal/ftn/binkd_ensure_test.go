package ftn

import (
	"os"
	"path/filepath"
	"strings"
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

func ensureTestFTNConfig() config.FTNConfig {
	return config.FTNConfig{
		Networks: map[string]config.FTNNetworkConfig{
			"fsxnet": {
				OwnAddress: "21:4/999",
				Links: []config.FTNLinkConfig{{
					Address: "21:4/158", SessionPassword: "s3cret",
					Hostname: "hub.example.org", Port: 24556,
				}},
			},
		},
		Binkd: config.BinkdServerConfig{Port: 25555, LogLevel: 6},
	}
}

func TestEnsureBinkdConfCreatesWhenMissing(t *testing.T) {
	root := t.TempDir()
	server := config.ServerConfig{BoardName: "Test Board", SysOpName: "Sysop", BBSLocation: "Testville"}

	created, err := EnsureBinkdConf(root, ensureTestFTNConfig(), server)
	if err != nil {
		t.Fatalf("EnsureBinkdConf: %v", err)
	}
	if !created {
		t.Fatal("expected binkd.conf to be created")
	}

	confPath := filepath.Join(root, "data", "ftn", "binkd.conf")
	info, err := os.Stat(confPath)
	if err != nil {
		t.Fatalf("stat regenerated conf: %v", err)
	}
	if info.Mode().Perm() != 0600 {
		t.Errorf("want mode 0600, got %v", info.Mode().Perm())
	}
	data, err := os.ReadFile(confPath)
	if err != nil {
		t.Fatal(err)
	}
	conf := string(data)
	for _, want := range []string{
		"address 21:4/999@fsxnet",
		"node 21:4/158@fsxnet hub.example.org:24556 s3cret",
		"iport 25555", // synced from Binkd.Port, not the template default
		"loglevel 6",  // synced from Binkd.LogLevel
		`sysname "Test Board"`,
	} {
		if !strings.Contains(conf, want) {
			t.Errorf("regenerated conf missing %q:\n%s", want, conf)
		}
	}
	if HasPlaceholders(conf, root) {
		t.Error("regenerated conf must not contain template placeholders")
	}
}

func TestEnsureBinkdConfNoOpWhenPresent(t *testing.T) {
	root := t.TempDir()
	confPath := filepath.Join(root, "data", "ftn", "binkd.conf")
	if err := os.MkdirAll(filepath.Dir(confPath), 0755); err != nil {
		t.Fatal(err)
	}
	sentinel := "# hand-edited conf\niport 24554\n"
	if err := os.WriteFile(confPath, []byte(sentinel), 0600); err != nil {
		t.Fatal(err)
	}

	created, err := EnsureBinkdConf(root, ensureTestFTNConfig(), config.ServerConfig{})
	if err != nil {
		t.Fatalf("EnsureBinkdConf: %v", err)
	}
	if created {
		t.Error("existing conf must not be reported as created")
	}
	data, err := os.ReadFile(confPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != sentinel {
		t.Errorf("existing conf was modified:\n%s", data)
	}
}

func TestEnsureBinkdConfNoNetworksNoFile(t *testing.T) {
	root := t.TempDir()
	created, err := EnsureBinkdConf(root, config.FTNConfig{}, config.ServerConfig{})
	if err != nil {
		t.Fatalf("EnsureBinkdConf: %v", err)
	}
	if created {
		t.Error("nothing to write must not be reported as created")
	}
	if _, err := os.Stat(filepath.Join(root, "data", "ftn", "binkd.conf")); !os.IsNotExist(err) {
		t.Error("no file must be written when no network is configured")
	}
}
