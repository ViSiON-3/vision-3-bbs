package ftn

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
)

// The reported case: a node number mistyped at setup (46:1/111 for 46:1/211)
// and corrected in the wizard. binkd.conf kept announcing the old address, so
// the hub held every netmail for the node binkd never claimed to be.
func TestUpdateBinkdOwnAddressRewritesAddressLine(t *testing.T) {
	conf := settingsConf +
		"domain agoranet /bbs/data/ftn/out 46\n" +
		"address 46:1/111@agoranet\n" +
		"node 46:1/100@agoranet hub.example.org:24554 secret\n"
	path := writeConf(t, conf)

	if err := UpdateBinkdOwnAddress(path, "agoranet", "46:1/111", "46:1/211"); err != nil {
		t.Fatalf("UpdateBinkdOwnAddress: %v", err)
	}
	got := readConf(t, path)

	if !strings.Contains(got, "address 46:1/211@agoranet\n") {
		t.Errorf("corrected address missing from:\n%s", got)
	}
	if strings.Contains(got, "46:1/111") {
		t.Errorf("stale address left behind in:\n%s", got)
	}
	want := strings.Replace(conf, "46:1/111", "46:1/211", 1)
	if got != want {
		t.Errorf("only the address line should change.\ngot:\n%s\nwant:\n%s", got, want)
	}
}

// Another AKA in the same domain is the sysop's own and another network's
// address is not ours to touch, even when the node numbers coincide.
func TestUpdateBinkdOwnAddressLeavesOtherAddresses(t *testing.T) {
	conf := "domain fsxnet /bbs/out 21\n" +
		"domain othernet /bbs/out_other 21\n" +
		"  address 21:4/158@fsxnet 21:4/158.1@FSXnet\n" +
		"address 21:4/158@othernet\n"
	path := writeConf(t, conf)

	if err := UpdateBinkdOwnAddress(path, "fsxnet", "21:4/158", "21:4/159"); err != nil {
		t.Fatalf("UpdateBinkdOwnAddress: %v", err)
	}
	got := readConf(t, path)

	if !strings.Contains(got, "  address 21:4/159@fsxnet 21:4/158.1@FSXnet\n") {
		t.Errorf("want the one entry replaced and the line's other AKA and indent kept:\n%s", got)
	}
	if !strings.Contains(got, "address 21:4/158@othernet\n") {
		t.Errorf("another network's address was changed:\n%s", got)
	}
}

// A file that never declared the old address — the sysop runs a different AKA,
// or fixed the line by hand already — is not rewritten.
func TestUpdateBinkdOwnAddressNoOpWithoutOldAddress(t *testing.T) {
	for name, conf := range map[string]string{
		"sysop AKA":     "domain fsxnet /bbs/out 21\naddress 21:4/158.12@fsxnet\n",
		"already fixed": "domain fsxnet /bbs/out 21\naddress 21:4/159@fsxnet\n",
	} {
		t.Run(name, func(t *testing.T) {
			path := writeConf(t, conf)
			if err := UpdateBinkdOwnAddress(path, "fsxnet", "21:4/158", "21:4/159"); err != nil {
				t.Fatalf("UpdateBinkdOwnAddress: %v", err)
			}
			if got := readConf(t, path); got != conf {
				t.Errorf("file should be untouched:\n%s", got)
			}
		})
	}
}

// With the new address already declared, replacing the old entry would give
// binkd the same AKA twice, so the stale one is removed.
func TestUpdateBinkdOwnAddressDropsStaleWhenNewDeclared(t *testing.T) {
	path := writeConf(t, "domain fsxnet /bbs/out 21\naddress 21:4/158@fsxnet\naddress 21:4/159@fsxnet\niport 24554\n")

	if err := UpdateBinkdOwnAddress(path, "fsxnet", "21:4/158", "21:4/159"); err != nil {
		t.Fatalf("UpdateBinkdOwnAddress: %v", err)
	}
	got := readConf(t, path)

	if want := "domain fsxnet /bbs/out 21\naddress 21:4/159@fsxnet\niport 24554\n"; got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

// The tosser's outbound filenames carry no zone, so binkd reads them as the
// domain's default zone. An address moved to another zone takes the domain
// line with it — but only that network's, and only while it holds the old zone.
func TestUpdateBinkdOwnAddressFollowsZoneChange(t *testing.T) {
	path := writeConf(t, "domain agoranet /bbs/out 64\ndomain fsxnet /bbs/out_fsx 21\naddress 64:1/211@agoranet\n")

	if err := UpdateBinkdOwnAddress(path, "agoranet", "64:1/211", "46:1/211"); err != nil {
		t.Fatalf("UpdateBinkdOwnAddress: %v", err)
	}
	got := readConf(t, path)

	if want := "domain agoranet /bbs/out 46\ndomain fsxnet /bbs/out_fsx 21\naddress 46:1/211@agoranet\n"; got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestUpdateBinkdOwnAddressKeepsSysopZone(t *testing.T) {
	path := writeConf(t, "domain agoranet /bbs/out 1\naddress 64:1/211@agoranet\n")

	if err := UpdateBinkdOwnAddress(path, "agoranet", "64:1/211", "46:1/211"); err != nil {
		t.Fatalf("UpdateBinkdOwnAddress: %v", err)
	}
	if got, want := readConf(t, path), "domain agoranet /bbs/out 1\naddress 46:1/211@agoranet\n"; got != want {
		t.Errorf("a zone that was never the old address's is the sysop's choice.\ngot:\n%s\nwant:\n%s", got, want)
	}
}

func TestUpdateBinkdOwnAddressMissingFileIsNoOp(t *testing.T) {
	path := filepath.Join(t.TempDir(), "binkd.conf")
	if err := UpdateBinkdOwnAddress(path, "fsxnet", "21:4/158", "21:4/159"); err != nil {
		t.Fatalf("UpdateBinkdOwnAddress: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("binkd.conf must not be created (stat: %v)", err)
	}
}

// An install that drifted before the editor followed address changes has no
// old address left to go by, so the sync cannot repair it — but it can say so.
func TestSyncBinkdNetworksWarnsOnAddressMismatch(t *testing.T) {
	var logged bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logged, &slog.HandlerOptions{Level: slog.LevelWarn})))
	defer slog.SetDefault(prev)

	conf := settingsConf + "domain agoranet /bbs/out 46\naddress 46:1/111@agoranet\n"
	path := writeConf(t, conf)
	cfg := config.FTNConfig{
		Networks: map[string]config.FTNNetworkConfig{"agoranet": netCfg("46:1/211", "46:1/100")},
	}
	if err := SyncBinkdNetworks(path, "/bbs", cfg); err != nil {
		t.Fatalf("SyncBinkdNetworks: %v", err)
	}

	if got := readConf(t, path); got != conf {
		t.Errorf("file should be untouched:\n%s", got)
	}
	for _, want := range []string{"46:1/211", "46:1/111@agoranet"} {
		if !strings.Contains(logged.String(), want) {
			t.Errorf("warning should name %s, got: %q", want, logged.String())
		}
	}
}

func TestSyncBinkdNetworksQuietWhenAddressMatches(t *testing.T) {
	var logged bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logged, &slog.HandlerOptions{Level: slog.LevelWarn})))
	defer slog.SetDefault(prev)

	// Second on its line, and in the case the sysop typed it.
	path := writeConf(t, settingsConf+"domain agoranet /bbs/out 46\naddress 46:1/211.1@agoranet 46:1/211@AgoraNet\n")
	cfg := config.FTNConfig{
		Networks: map[string]config.FTNNetworkConfig{"agoranet": netCfg("46:1/211", "46:1/100")},
	}
	if err := SyncBinkdNetworks(path, "/bbs", cfg); err != nil {
		t.Fatalf("SyncBinkdNetworks: %v", err)
	}
	if logged.Len() != 0 {
		t.Errorf("no warning expected, got: %q", logged.String())
	}
}
