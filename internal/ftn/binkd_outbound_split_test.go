package ftn

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
)

// Two networks sharing one outbound directory is the misconfiguration this
// whole feature exists for: BSO flow files are named from the destination
// net/node with no zone component, so two links whose net/node pairs collide
// resolve to one filename and one network's mail is handed to the other's hub.
// A sysop who splits the directories must have that split survive the sync
// that runs before every binkd launch — previously it repointed every domain
// line at the single global path and silently undid the fix.
func TestSyncBinkdSettingsKeepsPerDomainOutbound(t *testing.T) {
	conf := settingsConf +
		"domain fsxnet /opt/v3/data/ftn/out 21\n" +
		"domain tqwnet /opt/v3/data/ftn/out 1337\n"
	path := writeConf(t, conf)

	outbound := BinkdOutbound{
		Default:  "/opt/v3/data/ftn/out",
		ByDomain: map[string]string{"tqwnet": "/opt/v3/data/ftn/out.tqw"},
	}
	if err := SyncBinkdSettings(path, 24554, 4, outbound); err != nil {
		t.Fatalf("SyncBinkdSettings: %v", err)
	}

	got := readConf(t, path)
	if !strings.Contains(got, "domain fsxnet /opt/v3/data/ftn/out 21") {
		t.Errorf("network without an override must keep the global outbound:\n%s", got)
	}
	if !strings.Contains(got, "domain tqwnet /opt/v3/data/ftn/out.tqw 1337") {
		t.Errorf("overriding network must be repointed at its own outbound:\n%s", got)
	}
}

// The override is matched case-insensitively: binkd.conf domain names are
// lower-cased by the generator, but ftn.json's network keys are whatever the
// sysop typed.
func TestSyncBinkdSettingsPerDomainOutboundIgnoresCase(t *testing.T) {
	path := writeConf(t, settingsConf+"domain TQWnet /opt/v3/data/ftn/out 1337\n")
	outbound := BinkdOutbound{
		Default:  "/opt/v3/data/ftn/out",
		ByDomain: map[string]string{"tqwnet": "/opt/v3/data/ftn/out.tqw"},
	}
	if err := SyncBinkdSettings(path, 24554, 4, outbound); err != nil {
		t.Fatalf("SyncBinkdSettings: %v", err)
	}
	if got := readConf(t, path); !strings.Contains(got, "domain TQWnet /opt/v3/data/ftn/out.tqw 1337") {
		t.Errorf("override must match the domain name case-insensitively:\n%s", got)
	}
}

// An already-split conf must not be rewritten on every launch, or the file's
// mtime churns and a watcher-driven reload loop is possible.
func TestSyncBinkdSettingsSplitAlreadyCorrectLeavesFile(t *testing.T) {
	conf := settingsConf +
		"domain fsxnet /opt/v3/data/ftn/out 21\n" +
		"domain tqwnet /opt/v3/data/ftn/out.tqw 1337\n"
	path := writeConf(t, conf)
	outbound := BinkdOutbound{
		Default:  "/opt/v3/data/ftn/out",
		ByDomain: map[string]string{"tqwnet": "/opt/v3/data/ftn/out.tqw"},
	}
	if err := SyncBinkdSettings(path, 24554, 4, outbound); err != nil {
		t.Fatalf("SyncBinkdSettings: %v", err)
	}
	if got := readConf(t, path); got != conf {
		t.Errorf("file rewritten despite no change:\ngot:\n%s\nwant:\n%s", got, conf)
	}
}

func TestBinkdOutboundFor(t *testing.T) {
	o := BinkdOutbound{
		Default:  "/out",
		ByDomain: map[string]string{"tqwnet": "/out.tqw", "blank": ""},
	}
	for _, tc := range []struct{ domain, want string }{
		{"tqwnet", "/out.tqw"},
		{"TQWNET", "/out.tqw"},
		{"fsxnet", "/out"},
		{"blank", "/out"}, // an empty override falls back, never blanks the line
	} {
		if got := o.For(tc.domain); got != tc.want {
			t.Errorf("For(%q) = %q, want %q", tc.domain, got, tc.want)
		}
	}
}

// Dirs drives directory creation, so it must be deduplicated (two networks may
// legitimately name the same path) and deterministic.
func TestBinkdOutboundDirs(t *testing.T) {
	o := BinkdOutbound{
		Default: "/out",
		ByDomain: map[string]string{
			"tqwnet":   "/out.tqw",
			"agoranet": "/out.ago",
			"fsxnet":   "/out", // same as default: must not appear twice
			"blank":    "",
		},
	}
	got := o.Dirs()
	want := []string{"/out", "/out.ago", "/out.tqw"}
	if len(got) != len(want) {
		t.Fatalf("Dirs() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Dirs() = %v, want %v", got, want)
		}
	}
}

func TestBinkdOutboundForResolvesAgainstRoot(t *testing.T) {
	root := filepath.FromSlash("/bbs")
	cfg := config.FTNConfig{
		BinkdOutboundPath: "data/ftn/out",
		Networks: map[string]config.FTNNetworkConfig{
			"fsxnet": {},
			"TQWnet": {BinkdOutboundPath: "data/ftn/out.tqw"},
		},
	}
	o := BinkdOutboundFor(root, cfg)

	if want := filepath.Join(root, "data", "ftn", "out"); o.Default != want {
		t.Errorf("Default = %q, want %q", o.Default, want)
	}
	if want := filepath.Join(root, "data", "ftn", "out.tqw"); o.For("tqwnet") != want {
		t.Errorf("For(tqwnet) = %q, want %q", o.For("tqwnet"), want)
	}
	if want := filepath.Join(root, "data", "ftn", "out"); o.For("fsxnet") != want {
		t.Errorf("network without an override must get the default, got %q want %q", o.For("fsxnet"), want)
	}
	if _, ok := o.ByDomain["fsxnet"]; ok {
		t.Error("a network without an override must not appear in ByDomain")
	}
}

// A fresh conf must emit each network's own outbound on its domain line, or
// binkd and the tosser disagree from the first launch.
func TestWriteFreshBinkdConfPerDomainOutbound(t *testing.T) {
	var out strings.Builder
	cfg := BinkdConfig{
		BBSRoot: filepath.FromSlash("/bbs"),
		Domains: map[string]int{"fsxnet": 21, "tqwnet": 1337},
	}
	outbound := BinkdOutbound{
		Default:  "/bbs/data/ftn/out",
		ByDomain: map[string]string{"tqwnet": "/bbs/data/ftn/out.tqw"},
	}
	writeFreshBinkdConf(&out, cfg, outbound, "/log", "/sec", "/in", "/v3mail", "BBS", "Sysop", "Earth")

	got := out.String()
	for _, want := range []string{
		"domain fsxnet /bbs/data/ftn/out 21",
		"domain tqwnet /bbs/data/ftn/out.tqw 1337",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

// NetworkOutbounds feeds BinkdConfig.NetworkOutbound on regeneration; a config
// with no overrides must yield nil so nothing is emitted per-domain.
func TestNetworkOutbounds(t *testing.T) {
	none := config.FTNConfig{Networks: map[string]config.FTNNetworkConfig{"fsxnet": {}}}
	if got := NetworkOutbounds(none); got != nil {
		t.Errorf("no overrides must give nil, got %v", got)
	}

	some := config.FTNConfig{Networks: map[string]config.FTNNetworkConfig{
		"fsxnet": {},
		"TQWnet": {BinkdOutboundPath: "data/ftn/out.tqw"},
	}}
	got := NetworkOutbounds(some)
	if len(got) != 1 || got["tqwnet"] != "data/ftn/out.tqw" {
		t.Errorf("NetworkOutbounds = %v, want {tqwnet: data/ftn/out.tqw}", got)
	}
}
