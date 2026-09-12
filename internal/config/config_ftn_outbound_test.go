package config

import (
	"path/filepath"
	"strings"
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
	// t.TempDir, not a "/bbs" literal: on Windows a rooted path is only
	// absolute with a drive letter, so filepath.IsAbs("\\bbs\\...") is false
	// there and the "absolute override" case silently became a relative one
	// that got joined against root twice.
	root := t.TempDir()
	abs := filepath.Join(t.TempDir(), "out.ago")
	// The premise of the absolute-override case, asserted rather than assumed:
	// a "/bbs" literal satisfies this on Linux and fails it on Windows.
	if !filepath.IsAbs(abs) {
		t.Fatalf("test needs a genuinely absolute path, got %q", abs)
	}
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

// binkd refuses to start when the base outbound name carries an extension
// ("there should be no extension for the base outbound name"), because it
// derives zone outbounds by appending the zone as lowercase hex. Writing such
// a path into binkd.conf does not fail the save — it crash-loops the mailer
// afterwards with all mail stopped — so it has to be rejected up front.
func TestValidateBinkdOutboundPath(t *testing.T) {
	for _, tc := range []struct {
		path    string
		wantErr bool
	}{
		{"", false},                   // unset: falls back to the default
		{"data/ftn/out", false},       // the shipped default
		{"data/ftn/out_fsx", false},   // underscore is fine
		{"data/ftn/out-fsx", false},   // so is a hyphen
		{"/abs/path/outbound", false}, // absolute, no extension
		{"data/ftn/out.fsx", true},    // the one that crash-looped binkd
		{"data/ftn/out.tqw", true},    // likewise
		{"data/ftn/out.016", true},    // collides with binkd's zone suffix
		{"out.", true},                // trailing dot is still an extension
		{"data/ftn.d/out", false},     // a dot in a parent dir is binkd's business, not ours
	} {
		err := ValidateBinkdOutboundPath(tc.path)
		if tc.wantErr && err == nil {
			t.Errorf("ValidateBinkdOutboundPath(%q) = nil, want an error", tc.path)
		}
		if !tc.wantErr && err != nil {
			t.Errorf("ValidateBinkdOutboundPath(%q) = %v, want nil", tc.path, err)
		}
	}
}

// The error should hand the sysop a usable replacement rather than just
// saying no.
func TestValidateBinkdOutboundPathSuggestsFix(t *testing.T) {
	err := ValidateBinkdOutboundPath("data/ftn/out.tqw")
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "out_tqw") {
		t.Errorf("error should suggest the underscored form, got: %v", err)
	}
}

// ValidateFTNConfig runs at startup, so a hand-edited ftn.json is caught
// before the supervisor writes it into binkd.conf.
func TestValidateFTNConfigRejectsDottedOutbound(t *testing.T) {
	base := FTNConfig{
		InboundPath:       "data/ftn/in",
		OutboundPath:      "data/ftn/outbound",
		BinkdOutboundPath: "data/ftn/out",
		TempPath:          "data/ftn/temp",
	}

	ok := base
	ok.Networks = map[string]FTNNetworkConfig{
		"fsxnet": {InternalTosserEnabled: true, BinkdOutboundPath: "data/ftn/out_fsx"},
	}
	if err := ValidateFTNConfig(ok); err != nil {
		t.Errorf("valid config rejected: %v", err)
	}

	bad := base
	bad.Networks = map[string]FTNNetworkConfig{
		"tqwnet": {InternalTosserEnabled: true, BinkdOutboundPath: "data/ftn/out.tqw"},
	}
	err := ValidateFTNConfig(bad)
	if err == nil {
		t.Fatal("a dotted per-network outbound must be rejected")
	}
	if !strings.Contains(err.Error(), "tqwnet") {
		t.Errorf("error should name the network, got: %v", err)
	}

	badGlobal := base
	badGlobal.BinkdOutboundPath = "data/ftn/out.shared"
	badGlobal.Networks = map[string]FTNNetworkConfig{"fsxnet": {InternalTosserEnabled: true}}
	if err := ValidateFTNConfig(badGlobal); err == nil {
		t.Error("a dotted global outbound must be rejected too")
	}
}
