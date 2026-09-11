package tosser

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
)

// A network with its own binkd_outbound_path must pack into that directory.
// Sharing one outbound across networks is unsafe because BSO bundle and flow
// filenames are built from the destination net/node with no zone component:
// two links in different networks that share a net/node pair collide on one
// filename, and one network's mail is handed to the other's hub.
func TestNewResolvesPerNetworkBinkdOutbound(t *testing.T) {
	env := setupTestEnv(t)
	ownDir := filepath.Join(env.dir, "out.tqw")

	globalCfg := env.globalCfg
	globalCfg.Networks = map[string]config.FTNNetworkConfig{
		"tqwnet": {BinkdOutboundPath: ownDir},
		"fsxnet": {},
	}

	tqw, err := New("tqwnet", env.netCfg, globalCfg, env.dupeDB, env.msgMgr)
	if err != nil {
		t.Fatalf("New(tqwnet): %v", err)
	}
	if tqw.paths.BinkdOutboundPath != ownDir {
		t.Errorf("tqwnet outbound = %q, want its own %q", tqw.paths.BinkdOutboundPath, ownDir)
	}

	fsx, err := New("fsxnet", env.netCfg, globalCfg, env.dupeDB, env.msgMgr)
	if err != nil {
		t.Fatalf("New(fsxnet): %v", err)
	}
	if fsx.paths.BinkdOutboundPath != env.binkdDir {
		t.Errorf("fsxnet outbound = %q, want the global %q", fsx.paths.BinkdOutboundPath, env.binkdDir)
	}
}

// A network with no override keeps sharing the global outbound, so existing
// single-network installs are unaffected.
func TestNewFallsBackToGlobalBinkdOutbound(t *testing.T) {
	env := setupTestEnv(t)
	tosser, err := New("testnet", env.netCfg, env.globalCfg, env.dupeDB, env.msgMgr)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if tosser.paths.BinkdOutboundPath != env.binkdDir {
		t.Errorf("outbound = %q, want the global %q", tosser.paths.BinkdOutboundPath, env.binkdDir)
	}
}

// End to end: the bundle and its flow file land in the network's own outbound,
// and nothing is written into the global one.
func TestPackOutboundUsesPerNetworkDir(t *testing.T) {
	env := setupTestEnv(t)
	ownDir := filepath.Join(env.dir, "out.tqw")

	globalCfg := env.globalCfg
	globalCfg.Networks = map[string]config.FTNNetworkConfig{
		"tqwnet": {BinkdOutboundPath: ownDir},
	}

	netCfg := env.netCfg
	netCfg.Links = []linkConfig{{Address: "21:4/158", Name: "Test Hub", Flavour: "Crash"}}

	tosser, err := New("tqwnet", netCfg, globalCfg, env.dupeDB, env.msgMgr)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// Stage a .pkt addressed to the link so PackOutbound has something to do.
	pkt := makePktSimple(t, "FSX_TEST", "Sender", "All", "Subject", "Body\r", "21:4/158 DEADBEEF")
	if err := os.WriteFile(filepath.Join(env.outboundDir, "out00001.pkt"), pkt, 0644); err != nil {
		t.Fatal(err)
	}

	res := tosser.PackOutbound()
	if len(res.Errors) > 0 {
		t.Fatalf("PackOutbound errors: %v", res.Errors)
	}
	if res.BundlesCreated != 1 {
		t.Fatalf("BundlesCreated = %d, want 1", res.BundlesCreated)
	}

	own, err := os.ReadDir(ownDir)
	if err != nil {
		t.Fatalf("reading the network's outbound: %v", err)
	}
	var bundles, flows int
	for _, e := range own {
		switch {
		case strings.HasSuffix(strings.ToLower(e.Name()), ".clo"):
			flows++
		default:
			bundles++
		}
	}
	if bundles != 1 || flows != 1 {
		t.Errorf("want 1 bundle + 1 flow file in %s, got %d and %d (%v)", ownDir, bundles, flows, own)
	}

	// The global outbound must be untouched: a stray bundle there is exactly
	// the cross-network mix-up the split prevents.
	if global, err := os.ReadDir(env.binkdDir); err == nil && len(global) > 0 {
		t.Errorf("global outbound %s must stay empty, got %v", env.binkdDir, global)
	}
}
