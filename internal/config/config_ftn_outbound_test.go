package config

import (
	"path/filepath"
	"testing"
)

func TestBinkdOutboundFor(t *testing.T) {
	cfg := FTNConfig{
		BinkdOutboundPath: "data/ftn/out",
		Networks: map[string]FTNNetworkConfig{
			"fsxnet": {},
			"TQWnet": {BinkdOutboundPath: "data/ftn/out.tqw"},
			"blank":  {BinkdOutboundPath: ""},
		},
	}
	for _, tc := range []struct{ network, want string }{
		{"TQWnet", "data/ftn/out.tqw"},
		{"tqwnet", "data/ftn/out.tqw"}, // binkd.conf domain names are lower-cased
		{"fsxnet", "data/ftn/out"},
		{"blank", "data/ftn/out"},
		{"nosuchnetwork", "data/ftn/out"},
	} {
		if got := cfg.BinkdOutboundFor(tc.network); got != tc.want {
			t.Errorf("BinkdOutboundFor(%q) = %q, want %q", tc.network, got, tc.want)
		}
	}
}

// ResolvePaths makes the global paths absolute for the running BBS; a
// per-network override left relative would be resolved against the process
// working directory instead, putting one network's queue somewhere neither
// binkd nor the tosser looks.
func TestResolvePathsResolvesNetworkOutbound(t *testing.T) {
	root := filepath.FromSlash("/bbs")
	abs := filepath.Join(root, "elsewhere", "out.ago")
	cfg := FTNConfig{
		BinkdOutboundPath: "data/ftn/out",
		Networks: map[string]FTNNetworkConfig{
			"fsxnet":   {},
			"tqwnet":   {BinkdOutboundPath: "data/ftn/out.tqw"},
			"agoranet": {BinkdOutboundPath: abs},
		},
	}
	cfg.ResolvePaths(root)

	if want := filepath.Join(root, "data", "ftn", "out.tqw"); cfg.Networks["tqwnet"].BinkdOutboundPath != want {
		t.Errorf("relative override = %q, want %q", cfg.Networks["tqwnet"].BinkdOutboundPath, want)
	}
	if cfg.Networks["agoranet"].BinkdOutboundPath != abs {
		t.Errorf("absolute override must be left alone, got %q", cfg.Networks["agoranet"].BinkdOutboundPath)
	}
	if cfg.Networks["fsxnet"].BinkdOutboundPath != "" {
		t.Errorf("network without an override must stay empty, got %q", cfg.Networks["fsxnet"].BinkdOutboundPath)
	}
	if want := filepath.Join(root, "data", "ftn", "out"); cfg.BinkdOutboundFor("fsxnet") != want {
		t.Errorf("fallback after resolve = %q, want %q", cfg.BinkdOutboundFor("fsxnet"), want)
	}
}

// The override is optional and omitted from ftn.json when unset, so an
// existing config without it keeps sharing the global outbound.
func TestNetworkOutboundOmittedByDefault(t *testing.T) {
	cfg := FTNConfig{
		BinkdOutboundPath: "data/ftn/out",
		Networks:          map[string]FTNNetworkConfig{"fsxnet": {OwnAddress: "21:4/158.1"}},
	}
	if got := cfg.BinkdOutboundFor("fsxnet"); got != "data/ftn/out" {
		t.Errorf("BinkdOutboundFor = %q, want the global path", got)
	}
}
