package tosser

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/ftn"
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
	global, err := os.ReadDir(env.binkdDir)
	if err != nil {
		t.Fatalf("reading the global outbound: %v", err)
	}
	if len(global) > 0 {
		t.Errorf("global outbound %s must stay empty, got %v", env.binkdDir, global)
	}
}

// stagePacketTo writes a minimal echomail packet addressed to zone:net/node
// into the shared staging directory, the way the exporter leaves one.
func stagePacketTo(t *testing.T, env *testEnv, name string, zone, net, node uint16) {
	t.Helper()
	hdr := ftn.NewPacketHeader(21, 4, 158, 1, zone, net, node, 0, "")
	body := &ftn.ParsedBody{Area: "FSX_TEST", Text: "hello\r", Kludges: []string{"MSGID: 21:4/158.1 22222222"}}
	packed := &ftn.PackedMessage{
		MsgType: 2, OrigNode: 158, DestNode: node, OrigNet: 4, DestNet: net,
		DateTime: "01 Mar 26  12:00:00", To: "All", From: "Someone",
		Subject: "hi", Body: ftn.FormatPackedMessageBody(body),
	}
	var buf bytes.Buffer
	if err := ftn.WritePacket(&buf, hdr, []*ftn.PackedMessage{packed}); err != nil {
		t.Fatalf("WritePacket: %v", err)
	}
	if err := os.WriteFile(filepath.Join(env.outboundDir, name), buf.Bytes(), 0644); err != nil {
		t.Fatal(err)
	}
}

// The staging directory is shared by every network's tosser, and packets were
// matched to links by net/node alone. Two networks whose hubs share a net/node
// pair in different zones — 21:4/158 and 1337:4/158 — therefore let whichever
// tosser ran first claim the other network's packet, remove it, and bundle it
// into its own BSO for the wrong hub. The zone in the packet header is what
// tells them apart.
func TestPackOutboundLeavesOtherZonesPacketsAlone(t *testing.T) {
	env := setupTestEnv(t)

	netCfg := env.netCfg
	netCfg.Links = []linkConfig{{Address: "21:4/158", Name: "fsx hub", Flavour: "Crash"}}
	tosser, err := New("fsxnet", netCfg, env.globalCfg, env.dupeDB, env.msgMgr)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	stagePacketTo(t, env, "ours.pkt", 21, 4, 158)
	stagePacketTo(t, env, "theirs.pkt", 1337, 4, 158)

	res := tosser.PackOutbound()
	if res.BundlesCreated != 1 {
		t.Fatalf("BundlesCreated = %d, want 1 (only the zone-21 packet): %v", res.BundlesCreated, res.Errors)
	}
	if _, err := os.Stat(filepath.Join(env.outboundDir, "theirs.pkt")); err != nil {
		t.Errorf("the other zone's packet must stay staged for its own network's tosser: %v", err)
	}
	if _, err := os.Stat(filepath.Join(env.outboundDir, "ours.pkt")); !os.IsNotExist(err) {
		t.Errorf("our packet should have been bundled and removed, stat err = %v", err)
	}
}
