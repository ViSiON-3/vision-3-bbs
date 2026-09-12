package ftn

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
)

// netCfg is a network with one link, as ftnsetup or the manual editor leaves it.
func netCfg(own, link string) config.FTNNetworkConfig {
	return config.FTNNetworkConfig{
		InternalTosserEnabled: true,
		OwnAddress:            own,
		Links:                 []config.FTNLinkConfig{{Address: link, Hostname: "hub.example.org"}},
	}
}

// A network imported by "helper ftnsetup" or added by hand got a node line from
// SyncBinkdConf and nothing else, because only the wizard's generator wrote
// domain and address lines. binkd then refused the poll with "fidonet: unknown
// domain" and, our AKA being undeclared, refused the uplink's inbound session
// too.
func TestSyncBinkdNetworksDeclaresMissingNetwork(t *testing.T) {
	path := writeConf(t, settingsConf+
		"domain fsxnet /bbs/data/ftn/out_fsx 21\naddress 21:4/158.1@fsxnet\n")

	cfg := config.FTNConfig{
		BinkdOutboundPath: "data/ftn/out",
		Networks: map[string]config.FTNNetworkConfig{
			"fsxnet":  netCfg("21:4/158.1", "21:4/158"),
			"fidonet": netCfg("3:633/2744.11", "3:633/2744"),
		},
	}
	cfg.Networks["fidonet"] = config.FTNNetworkConfig{
		InternalTosserEnabled: true,
		OwnAddress:            "3:633/2744.11",
		Links:                 cfg.Networks["fidonet"].Links,
		BinkdOutboundPath:     "data/ftn/out_fido",
	}

	if err := SyncBinkdNetworks(path, "/bbs", cfg); err != nil {
		t.Fatalf("SyncBinkdNetworks: %v", err)
	}

	got := readConf(t, path)
	for _, want := range []string{
		"domain fidonet " + filepath.Join("/bbs", "data/ftn/out_fido") + " 3",
		"address 3:633/2744.11@fidonet",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	// The already-declared network must not be duplicated: binkd aborts on a
	// duplicate domain, and a second address line changes which AKA it presents.
	if n := strings.Count(got, "domain fsxnet"); n != 1 {
		t.Errorf("fsxnet domain declared %d times, want 1:\n%s", n, got)
	}
	if n := strings.Count(got, "address 21:4/158.1@fsxnet"); n != 1 {
		t.Errorf("fsxnet address declared %d times, want 1:\n%s", n, got)
	}
}

// The zone on the domain line comes from own_address, so each network gets its
// own rather than everything landing in one zone's outbound.
func TestSyncBinkdNetworksUsesOwnAddressZone(t *testing.T) {
	path := writeConf(t, settingsConf)
	cfg := config.FTNConfig{
		BinkdOutboundPath: "data/ftn/out",
		Networks: map[string]config.FTNNetworkConfig{
			"tqwnet": netCfg("1337:3/123.1", "1337:3/123"),
		},
	}
	if err := SyncBinkdNetworks(path, "/bbs", cfg); err != nil {
		t.Fatalf("SyncBinkdNetworks: %v", err)
	}
	want := "domain tqwnet " + filepath.Join("/bbs", "data/ftn/out") + " 1337"
	if got := readConf(t, path); !strings.Contains(got, want) {
		t.Errorf("want %q, got:\n%s", want, got)
	}
}

// Nothing to add means the file is not touched, so this can run before every
// binkd launch without churning mtimes.
func TestSyncBinkdNetworksNoChangeLeavesFile(t *testing.T) {
	conf := settingsConf + "domain fsxnet /bbs/data/ftn/out 21\naddress 21:4/158.1@fsxnet\n"
	path := writeConf(t, conf)
	cfg := config.FTNConfig{
		BinkdOutboundPath: "data/ftn/out",
		Networks:          map[string]config.FTNNetworkConfig{"fsxnet": netCfg("21:4/158.1", "21:4/158")},
	}
	if err := SyncBinkdNetworks(path, "/bbs", cfg); err != nil {
		t.Fatalf("SyncBinkdNetworks: %v", err)
	}
	if got := readConf(t, path); got != conf {
		t.Errorf("file rewritten with nothing to add:\ngot:\n%s\nwant:\n%s", got, conf)
	}
}

// A sysop who declared a different AKA of their own in this domain keeps it:
// binkd treats every address line as ours, so adding a second changes which
// address it presents to the uplink.
func TestSyncBinkdNetworksKeepsSysopAKA(t *testing.T) {
	conf := settingsConf + "domain fidonet /bbs/out_fido 3\naddress 3:633/2744.12@fidonet\n"
	path := writeConf(t, conf)
	cfg := config.FTNConfig{
		Networks: map[string]config.FTNNetworkConfig{"fidonet": netCfg("3:633/2744.11", "3:633/2744")},
	}
	if err := SyncBinkdNetworks(path, "/bbs", cfg); err != nil {
		t.Fatalf("SyncBinkdNetworks: %v", err)
	}
	got := readConf(t, path)
	if strings.Contains(got, "address 3:633/2744.11@fidonet") {
		t.Errorf("must not add a second AKA in a domain that already has one:\n%s", got)
	}
	if got != conf {
		t.Errorf("file should be untouched:\n%s", got)
	}
}

// An unparseable own_address gives no zone, and guessing one would declare a
// domain whose outbound silently belongs to the wrong zone.
func TestSyncBinkdNetworksSkipsUnparseableAddress(t *testing.T) {
	path := writeConf(t, settingsConf)
	cfg := config.FTNConfig{
		Networks: map[string]config.FTNNetworkConfig{
			"brokennet": {InternalTosserEnabled: true, OwnAddress: "not-an-address"},
		},
	}
	if err := SyncBinkdNetworks(path, "/bbs", cfg); err != nil {
		t.Fatalf("SyncBinkdNetworks: %v", err)
	}
	if got := readConf(t, path); strings.Contains(got, "brokennet") {
		t.Errorf("must not declare a network with no usable address:\n%s", got)
	}
}

// A missing binkd.conf is EnsureBinkdConf's job, not this one's.
func TestSyncBinkdNetworksMissingFileIsNoOp(t *testing.T) {
	cfg := config.FTNConfig{
		Networks: map[string]config.FTNNetworkConfig{"fsxnet": netCfg("21:4/158.1", "21:4/158")},
	}
	if err := SyncBinkdNetworks(t.TempDir()+"/absent.conf", "/bbs", cfg); err != nil {
		t.Errorf("missing file must be a no-op, got: %v", err)
	}
}

// binkd matches domain names case-insensitively, so a sysop's hand-written
// "domain TQWnet" already declares tqwnet. Comparing case-sensitively treated
// it as missing and appended a second declaration on every sync.
func TestSyncBinkdNetworksMatchesDomainCaseInsensitively(t *testing.T) {
	// Mixed case in the keywords as well as the domain: binkd accepts both.
	path := writeConf(t, settingsConf+
		"DOMAIN TQWnet /bbs/data/ftn/out_tqw 1337\nAddress 1337:3/123.1@TQWnet\n")

	cfg := config.FTNConfig{
		BinkdOutboundPath: "data/ftn/out",
		Networks: map[string]config.FTNNetworkConfig{
			"tqwnet": netCfg("1337:3/123.1", "1337:3/123"),
		},
	}
	if err := SyncBinkdNetworks(path, "/bbs", cfg); err != nil {
		t.Fatalf("SyncBinkdNetworks: %v", err)
	}

	got := readConf(t, path)
	if n := strings.Count(strings.ToLower(got), "domain tqwnet"); n != 1 {
		t.Errorf("tqwnet domain declared %d times, want 1:\n%s", n, got)
	}
	if n := strings.Count(strings.ToLower(got), "@tqwnet"); n != 1 {
		t.Errorf("tqwnet address declared %d times, want 1:\n%s", n, got)
	}
}
