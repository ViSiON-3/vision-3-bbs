package ftn

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
)

func TestBinkdOutboundDir(t *testing.T) {
	root := t.TempDir()
	// t.TempDir is absolute on every platform; a literal "/srv/mail/out" is
	// not absolute on Windows and would be joined to the root instead.
	absolute := filepath.Join(t.TempDir(), "mailout")

	tests := []struct {
		name       string
		configured string
		want       string
	}{
		{"unset falls back", "", filepath.Join(root, "data", "ftn", "out")},
		{"relative resolves against root", "data/ftn/out", filepath.Join(root, "data/ftn/out")},
		{"absolute kept as-is", absolute, absolute},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := BinkdOutboundDir(root, tc.configured); got != tc.want {
				t.Errorf("BinkdOutboundDir(%q) = %q, want %q", tc.configured, got, tc.want)
			}
		})
	}
}

// A fresh binkd.conf must point binkd at the directory the tosser packs into.
// Hardcoding data/ftn/out here is what let a wizard-written ftn.json
// (binkd_outbound_path: data/ftn/binkd_outbound) queue echomail into a
// directory binkd never read.
func TestUpdateBinkdConfHonorsOutboundPath(t *testing.T) {
	dir := t.TempDir()
	conf := filepath.Join(dir, "binkd.conf")

	cfg := BinkdConfig{
		BBSRoot:      dir,
		BoardName:    "Test BBS",
		Domains:      map[string]int{"fsxnet": 21},
		Addresses:    []string{"21:4/158@fsxnet"},
		OutboundPath: "data/ftn/binkd_outbound",
		Node: BinkdNode{
			Address:     "21:4/158@fsxnet",
			Hostname:    "hub.example:24554",
			SessionPwd:  "pw",
			NetworkName: "fsxnet",
		},
	}
	if err := UpdateBinkdConf(conf, cfg); err != nil {
		t.Fatalf("UpdateBinkdConf: %v", err)
	}
	got := readConf(t, conf)
	want := "domain fsxnet " + filepath.Join(dir, "data/ftn/binkd_outbound") + " 21"
	if !strings.Contains(got, want) {
		t.Errorf("want %q in:\n%s", want, got)
	}
}

func TestRegenerateBinkdConfHonorsOutboundPath(t *testing.T) {
	dir := t.TempDir()
	conf := filepath.Join(dir, "binkd.conf")
	outside := filepath.Join(t.TempDir(), "mailout") // absolute on every platform

	cfg := BinkdConfig{
		BBSRoot:      dir,
		BoardName:    "Test BBS",
		Domains:      map[string]int{"fsxnet": 21},
		Addresses:    []string{"21:4/158@fsxnet"},
		OutboundPath: outside,
	}
	if err := RegenerateBinkdConf(conf, cfg, nil); err != nil {
		t.Fatalf("RegenerateBinkdConf: %v", err)
	}
	want := "domain fsxnet " + outside + " 21"
	if got := readConf(t, conf); !strings.Contains(got, want) {
		t.Errorf("want %q in:\n%s", want, got)
	}
}

// EnsureBinkdConf is the recovery path for a deleted binkd.conf; it must not
// recreate the file with the drifted default.
func TestEnsureBinkdConfUsesConfiguredOutbound(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "data", "ftn"), 0755); err != nil {
		t.Fatal(err)
	}
	ftnCfg := config.FTNConfig{
		BinkdOutboundPath: "data/ftn/binkd_outbound",
		Networks: map[string]config.FTNNetworkConfig{
			"fsxnet": {
				OwnAddress: "21:4/158",
				Links:      []config.FTNLinkConfig{{Address: "21:4/100", Hostname: "hub.example", Port: 24554}},
			},
		},
	}
	created, err := EnsureBinkdConf(dir, ftnCfg, config.ServerConfig{BoardName: "Test BBS"})
	if err != nil {
		t.Fatalf("EnsureBinkdConf: %v", err)
	}
	if !created {
		t.Fatal("expected binkd.conf to be created")
	}
	got := readConf(t, filepath.Join(dir, "data", "ftn", "binkd.conf"))
	want := "domain fsxnet " + filepath.Join(dir, "data/ftn/binkd_outbound") + " 21"
	if !strings.Contains(got, want) {
		t.Errorf("want %q in:\n%s", want, got)
	}
}
