package tosser

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/ftn"
	"github.com/ViSiON-3/vision-3-bbs/internal/jam"
	"github.com/ViSiON-3/vision-3-bbs/internal/message"
)

// newTestTosser builds a tosser for the single-link test network.
func newTestTosser(t *testing.T, env *testEnv) *Tosser {
	t.Helper()
	tosser, err := New("testnet", env.netCfg, env.globalCfg, env.dupeDB, env.msgMgr)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return tosser
}

// bundlePackets zips the named packets from dir into an inbound bundle and
// removes the loose copies, leaving only the bundle.
func bundlePackets(t *testing.T, bundlePath string, pktPaths ...string) {
	t.Helper()
	if _, err := ftn.CreateBundle(bundlePath, pktPaths); err != nil {
		t.Fatalf("CreateBundle: %v", err)
	}
	for _, p := range pktPaths {
		if err := os.Remove(p); err != nil {
			t.Fatal(err)
		}
	}
}

// dirNames lists the entries of dir, or nil if it does not exist.
func dirNames(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

// captureLog routes slog to a buffer for the duration of the test.
func captureLog(t *testing.T) func() string {
	t.Helper()
	var mu sync.Mutex
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(lockedWriter{&mu, &buf}, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return func() string {
		mu.Lock()
		defer mu.Unlock()
		return buf.String()
	}
}

type lockedWriter struct {
	mu  *sync.Mutex
	buf *bytes.Buffer
}

func (w lockedWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.Write(p)
}

// --- inbound bundles ---

// A bundle whose packets all come from an address none of this network's
// links use belongs to another network: it stays where it is, and the copies
// extracted to look inside are cleaned up.
func TestForeignBundleIsLeftInPlace(t *testing.T) {
	env := setupTestEnv(t)
	tosser := newTestTosser(t, env)

	staging := t.TempDir()
	bundlePath := filepath.Join(env.inboundDir, "0279ab38.mo0")
	bundlePackets(t, bundlePath,
		writeGoodPacket(t, staging, "aaaa0001.pkt", 3, 633, 2744),
		writeGoodPacket(t, staging, "aaaa0002.pkt", 3, 633, 2744))

	result := tosser.ProcessInbound()
	if result.PacketsProcessed != 0 || result.MessagesImported != 0 || len(result.Errors) != 0 {
		t.Errorf("result = %+v, want nothing processed and no errors", result)
	}
	if _, err := os.Stat(bundlePath); err != nil {
		t.Errorf("foreign bundle must stay in the inbound directory: %v", err)
	}
	if left := dirNames(t, filepath.Join(env.tempDir, "unpack")); len(left) != 0 {
		t.Errorf("extracted packets left in unpack dir: %v", left)
	}
	// Both declined packets are attributed to the bundle they came in.
	if got := result.SkippedByFile[bundlePath]["3:633/2744"]; got != 2 {
		t.Errorf("skipped packets recorded for the bundle = %d, want 2 (%v)", got, result.SkippedByFile)
	}
}

// A bundle mixing our hub's packet with another network's: ours is tossed,
// the bundle is consumed, and the other packet goes back to the inbound
// directory for its own network's tosser instead of stranding in unpack/.
func TestMixedBundleRequeuesForeignPacket(t *testing.T) {
	env := setupTestEnv(t)
	tosser := newTestTosser(t, env)

	staging := t.TempDir()
	bundlePath := filepath.Join(env.inboundDir, "0004009e.tu0")
	bundlePackets(t, bundlePath,
		writeGoodPacket(t, staging, "ours0001.pkt", 21, 4, 158),
		writeGoodPacket(t, staging, "them0001.pkt", 3, 633, 2744))

	result := tosser.ProcessInbound()
	if result.PacketsProcessed != 1 || result.MessagesImported != 1 || len(result.Errors) != 0 {
		t.Errorf("result = %+v, want 1 packet, 1 message, no errors", result)
	}
	if _, err := os.Stat(bundlePath); !os.IsNotExist(err) {
		t.Errorf("processed bundle should be removed, stat err = %v", err)
	}
	if got := dirNames(t, env.inboundDir); len(got) != 1 || got[0] != "them0001.pkt" {
		t.Errorf("inbound dir = %v, want only the re-queued them0001.pkt", got)
	}
	if left := dirNames(t, filepath.Join(env.tempDir, "unpack")); len(left) != 0 {
		t.Errorf("packets left in unpack dir: %v", left)
	}
	// The re-queued packet is intact and still from the other network.
	hdr, msgs, err := readPacketFile(t, filepath.Join(env.inboundDir, "them0001.pkt"))
	if err != nil || len(msgs) != 1 || hdr.OrigNet != 633 || hdr.OrigNode != 2744 {
		t.Errorf("re-queued packet: header %+v, %d messages, err %v", hdr, len(msgs), err)
	}

	base, err := env.msgMgr.GetBase(1)
	if err != nil {
		t.Fatalf("GetBase: %v", err)
	}
	defer base.Close()
	if n, _ := base.GetMessageCount(); n != 1 {
		t.Errorf("messages in FSX_TEST = %d, want 1", n)
	}
}

// A file with a bundle name and ZIP magic that does not unzip is moved to the
// temp path for inspection rather than retried every cycle.
func TestCorruptBundleIsMovedAside(t *testing.T) {
	env := setupTestEnv(t)
	tosser := newTestTosser(t, env)

	bundlePath := filepath.Join(env.inboundDir, "0004009e.we0")
	if err := os.WriteFile(bundlePath, []byte("PK\x03\x04 this is not really a zip archive"), 0644); err != nil {
		t.Fatal(err)
	}

	result := tosser.ProcessInbound()
	if len(result.Errors) != 1 || !strings.Contains(result.Errors[0], "extract bundle 0004009e.we0") {
		t.Errorf("errors = %v, want one extract failure naming the bundle", result.Errors)
	}
	if result.PacketsProcessed != 0 {
		t.Errorf("PacketsProcessed = %d, want 0", result.PacketsProcessed)
	}
	if _, err := os.Stat(bundlePath); !os.IsNotExist(err) {
		t.Errorf("bad bundle still in inbound, stat err = %v", err)
	}
	moved, err := os.ReadFile(filepath.Join(env.tempDir, "0004009e.we0"))
	if err != nil || !strings.HasPrefix(string(moved), "PK\x03\x04 this is not") {
		t.Errorf("bad bundle not preserved in temp path: %q, err %v", moved, err)
	}
}

// Flow files share the bundle extensions but are plain text; they, stray
// directories and unrelated files are not the tosser's to touch.
func TestInboundIgnoresNonMailFiles(t *testing.T) {
	env := setupTestEnv(t)
	tosser := newTestTosser(t, env)

	files := map[string]string{
		"0004009e.out": "^/var/spool/ftn/somefile.pkt\n", // text flow file
		"0004009e.th0": "x",                              // too short to be a ZIP
		"readme.txt":   "not mail",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(env.inboundDir, name), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(env.inboundDir, "subdir.pkt"), 0755); err != nil {
		t.Fatal(err)
	}

	result := tosser.ProcessInbound()
	if result.PacketsProcessed != 0 || len(result.Errors) != 0 {
		t.Errorf("result = %+v, want nothing processed and no errors", result)
	}
	if got := dirNames(t, env.inboundDir); len(got) != 4 {
		t.Errorf("inbound dir = %v, want all four entries untouched", got)
	}
	for name, content := range files {
		if data, err := os.ReadFile(filepath.Join(env.inboundDir, name)); err != nil || string(data) != content {
			t.Errorf("%s changed: %q, err %v", name, data, err)
		}
	}
}

func TestProcessInboundMissingDirIsNotAnError(t *testing.T) {
	env := setupTestEnv(t)
	env.globalCfg.InboundPath = filepath.Join(env.dir, "never-created")
	tosser := newTestTosser(t, env)

	result := tosser.ProcessInbound()
	if result.PacketsProcessed != 0 || len(result.Errors) != 0 {
		t.Errorf("result = %+v, want an empty result", result)
	}
}

// --- RunOnce / PurgeDupes ---

func TestRunOnceImportsThenExports(t *testing.T) {
	env := setupTestEnv(t)

	// One message waiting to go out...
	base, err := env.msgMgr.GetBase(1)
	if err != nil {
		t.Fatalf("GetBase: %v", err)
	}
	msg := jam.NewMessage()
	msg.From, msg.To, msg.Subject = "Local User", "All", "Outgoing"
	msg.Text = "Export me.\r"
	msg.MsgID = "21:4/158.1 0BADF00D"
	area, _ := env.msgMgr.GetAreaByTag("FSX_TEST")
	if _, err := base.WriteMessageExt(msg, jam.DetermineMessageType(area.AreaType, area.EchoTag), area.EchoTag, "TestBBS"); err != nil {
		t.Fatalf("WriteMessageExt: %v", err)
	}
	base.Close()

	// ...one packet coming in, and one repeat of it.
	writeGoodPacket(t, env.inboundDir, "in000001.pkt", 21, 4, 158)
	writeGoodPacket(t, env.inboundDir, "in000002.pkt", 21, 4, 158)

	result := newTestTosser(t, env).RunOnce()
	if len(result.Errors) != 0 {
		t.Fatalf("RunOnce errors: %v", result.Errors)
	}
	if result.PacketsProcessed != 2 || result.MessagesImported != 1 || result.DupesSkipped != 1 || result.MessagesExported != 1 {
		t.Errorf("result = %+v, want 2 packets, 1 imported, 1 dupe, 1 exported", result)
	}
	if left := dirNames(t, env.inboundDir); len(left) != 0 {
		t.Errorf("inbound not emptied: %v", left)
	}
	staged := dirNames(t, env.outboundDir)
	if len(staged) != 1 || !strings.EqualFold(filepath.Ext(staged[0]), ".pkt") {
		t.Fatalf("staging dir = %v, want one outbound packet", staged)
	}
	// The exported packet carries the local message, not the one just tossed.
	_, msgs, err := readPacketFile(t, filepath.Join(env.outboundDir, staged[0]))
	if err != nil || len(msgs) != 1 || msgs[0].Subject != "Outgoing" {
		t.Errorf("exported packet: %d messages, err %v", len(msgs), err)
	}
}

func TestPurgeDupesDropsOldEntries(t *testing.T) {
	env := setupTestEnv(t)
	db, err := NewDupeDBFromPath(env.dupeDBPath)
	if err != nil {
		t.Fatalf("NewDupeDBFromPath: %v", err)
	}
	if db.maxAge != 30*24*time.Hour {
		t.Errorf("dupe history = %v, want 30 days", db.maxAge)
	}
	db.Add("recent")
	db.Add("ancient")
	db.entries["ancient"] = time.Now().Add(-31 * 24 * time.Hour).Unix()

	tosser, err := New("testnet", env.netCfg, env.globalCfg, db, env.msgMgr)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := tosser.PurgeDupes(); err != nil {
		t.Fatalf("PurgeDupes: %v", err)
	}
	if !db.IsDupe("recent") || db.IsDupe("ancient") {
		t.Errorf("after purge: recent=%v ancient=%v, want true and false", db.IsDupe("recent"), db.IsDupe("ancient"))
	}

	// The purge is saved: a fresh DB on the same file sees the same thing.
	reloaded, err := NewDupeDBFromPath(env.dupeDBPath)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if reloaded.Count() != 1 || !reloaded.IsDupe("recent") {
		t.Errorf("reloaded DB has %d entries, recent=%v; want only recent", reloaded.Count(), reloaded.IsDupe("recent"))
	}
}

func TestNewRejectsInvalidOwnAddress(t *testing.T) {
	env := setupTestEnv(t)
	cfg := env.netCfg
	cfg.OwnAddress = "not-an-address"
	if _, err := New("testnet", cfg, env.globalCfg, env.dupeDB, env.msgMgr); err == nil ||
		!strings.Contains(err.Error(), `tosser[testnet]: invalid own_address "not-an-address"`) {
		t.Errorf("New = %v, want invalid own_address error", err)
	}
}

// --- outbound packing ---

func TestPackOutboundNothingToDo(t *testing.T) {
	t.Run("paths not configured", func(t *testing.T) {
		env := setupTestEnv(t)
		env.globalCfg.BinkdOutboundPath = ""
		res := newTestTosser(t, env).PackOutbound()
		if len(res.Errors) != 1 || !strings.Contains(res.Errors[0], "not configured") {
			t.Errorf("errors = %v, want a not-configured error", res.Errors)
		}
	})

	t.Run("staging directory missing", func(t *testing.T) {
		env := setupTestEnv(t)
		env.globalCfg.OutboundPath = filepath.Join(env.dir, "never-created")
		res := newTestTosser(t, env).PackOutbound()
		if res.BundlesCreated != 0 || len(res.Errors) != 0 {
			t.Errorf("result = %+v, want an empty result", res)
		}
	})

	t.Run("staging path is a file", func(t *testing.T) {
		env := setupTestEnv(t)
		env.globalCfg.OutboundPath = filepath.Join(env.dir, "plainfile")
		if err := os.WriteFile(env.globalCfg.OutboundPath, nil, 0644); err != nil {
			t.Fatal(err)
		}
		res := newTestTosser(t, env).PackOutbound()
		if len(res.Errors) != 1 || !strings.Contains(res.Errors[0], "read staging dir") {
			t.Errorf("errors = %v, want a staging dir read error", res.Errors)
		}
	})
}

// Packets that cannot be packed are reported and left staged; the ones that
// can are bundled regardless.
func TestPackOutboundReportsUnpackablePackets(t *testing.T) {
	env := setupTestEnv(t)
	// The first link's address is unusable; packing must get past it.
	env.netCfg.Links = []linkConfig{
		{Address: "garbage", Name: "Broken"},
		{Address: "21:4/158", Name: "Test Hub"},
	}
	tosser := newTestTosser(t, env)

	stagePacketTo(t, env, "good.pkt", 21, 4, 158)
	stagePacketTo(t, env, "nolink.pkt", 21, 1, 999)
	if err := os.WriteFile(filepath.Join(env.outboundDir, "short.pkt"), []byte("tiny"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(env.outboundDir, "notes.txt"), []byte("ignore me"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(env.outboundDir, "dir.pkt"), 0755); err != nil {
		t.Fatal(err)
	}

	res := tosser.PackOutbound()
	if res.BundlesCreated != 1 || res.PacketsPacked != 1 {
		t.Errorf("result = %+v, want 1 bundle holding 1 packet", res)
	}
	errs := strings.Join(res.Errors, "\n")
	if len(res.Errors) != 2 || !strings.Contains(errs, "read pkt header short.pkt") ||
		!strings.Contains(errs, "pkt nolink.pkt: no link found for dest 1/999") {
		t.Errorf("errors = %q, want the short packet and the unroutable packet reported", res.Errors)
	}

	left := strings.Join(dirNames(t, env.outboundDir), ",")
	if left != "dir.pkt,nolink.pkt,notes.txt,short.pkt" {
		t.Errorf("staging dir = %s, want everything but good.pkt left behind", left)
	}
	// A Normal-flavour link gets a bundle and no flow file.
	out := dirNames(t, env.binkdDir)
	if len(out) != 1 || !strings.HasPrefix(out[0], "0004009e.") {
		t.Fatalf("binkd outbound = %v, want one bundle for 4/158", out)
	}
	pkts, err := ftn.ExtractBundle(filepath.Join(env.binkdDir, out[0]), t.TempDir())
	if err != nil || len(pkts) != 1 || filepath.Base(pkts[0]) != "good.pkt" {
		t.Errorf("bundle contents = %v, err %v; want good.pkt", pkts, err)
	}
}

func TestResolveUniqueBundlePath(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, "0004009e.mo0")
	touch := func(path string) {
		t.Helper()
		if err := os.WriteFile(path, nil, 0644); err != nil {
			t.Fatal(err)
		}
	}

	if got := resolveUniqueBundlePath(base); got != base {
		t.Errorf("free name: got %q, want %q", got, base)
	}

	// An earlier bundle for the same link and day must not be overwritten.
	touch(base)
	if got, want := resolveUniqueBundlePath(base), filepath.Join(dir, "0004009e.mo1"); got != want {
		t.Errorf("first collision: got %q, want %q", got, want)
	}
	touch(filepath.Join(dir, "0004009e.mo1"))
	touch(filepath.Join(dir, "0004009e.mo2"))
	if got, want := resolveUniqueBundlePath(base), filepath.Join(dir, "0004009e.mo3"); got != want {
		t.Errorf("third collision: got %q, want %q", got, want)
	}

	// With .mo0-.mo9 all taken the name still has to be new, and still has
	// to be recognised as a bundle for that day.
	for _, d := range "3456789" {
		touch(filepath.Join(dir, "0004009e.mo"+string(d)))
	}
	got := resolveUniqueBundlePath(base)
	if _, err := os.Stat(got); !os.IsNotExist(err) {
		t.Errorf("fallback name %q already exists", got)
	}
	name := filepath.Base(got)
	if !strings.HasPrefix(name, "0004009e_") || !strings.HasSuffix(name, ".mo0") || !ftn.BundleExtension(name) {
		t.Errorf("fallback name = %q, want 0004009e_<n>.mo0", name)
	}
}

func TestWriteFlowFile(t *testing.T) {
	dest := &jam.FidoAddress{Zone: 21, Net: 4, Node: 158}
	tests := []struct {
		flavour  string
		wantFile string // "" = no flow file
	}{
		{"Crash", "0004009e.clo"},
		{"hold", "0004009e.hlo"},
		{"DIRECT", "0004009e.dlo"},
		{"Normal", ""},
		{"", ""},
	}
	for _, tt := range tests {
		t.Run("flavour "+tt.flavour, func(t *testing.T) {
			dir := t.TempDir()
			bundle := filepath.Join(dir, "0004009e.mo0")
			// Two bundles for one link append to the same flow file.
			for _, b := range []string{bundle, bundle + "x"} {
				if err := writeFlowFile(dir, dest, tt.flavour, b); err != nil {
					t.Fatalf("writeFlowFile: %v", err)
				}
			}
			names := dirNames(t, dir)
			if tt.wantFile == "" {
				if len(names) != 0 {
					t.Errorf("flow files written for %q delivery: %v", tt.flavour, names)
				}
				return
			}
			if len(names) != 1 || names[0] != tt.wantFile {
				t.Fatalf("flow files = %v, want %s", names, tt.wantFile)
			}
			// ^ tells binkd to delete the bundle once it is sent.
			data, err := os.ReadFile(filepath.Join(dir, tt.wantFile))
			if want := "^" + bundle + "\n^" + bundle + "x\n"; err != nil || string(data) != want {
				t.Errorf("flow file = %q, err %v; want %q", data, err, want)
			}
		})
	}

	if err := writeFlowFile(filepath.Join(t.TempDir(), "missing"), dest, "Crash", "b"); err == nil {
		t.Error("writeFlowFile into a missing directory returned no error")
	}
}

// --- link selection ---

func TestLinkSelection(t *testing.T) {
	env := setupTestEnv(t)
	env.netCfg.Links = []linkConfig{
		{Address: "bogus", Name: "Unparseable"},
		{Address: "21:1/100", Name: "Hub"},
		{Address: "21:4/158", Name: "Direct"},
	}
	tosser := newTestTosser(t, env)

	// Netmail for a linked node goes to that link; anything else is routed
	// through the first configured link.
	if link := tosser.findLinkForNetmail(&jam.FidoAddress{Zone: 21, Net: 4, Node: 158, Point: 7}); link == nil || link.Name != "Direct" {
		t.Errorf("netmail for a linked node routed via %+v, want Direct", link)
	}
	if link := tosser.findLinkForNetmail(&jam.FidoAddress{Zone: 21, Net: 9, Node: 9}); link == nil || link.Name != "Unparseable" {
		t.Errorf("netmail for an unlinked node routed via %+v, want the first link", link)
	}
	if link := tosser.findLink("21:1/100"); link == nil || link.Name != "Hub" {
		t.Errorf("findLink(21:1/100) = %+v, want Hub", link)
	}
	if link := tosser.findLink("21:1/101"); link != nil {
		t.Errorf("findLink for an unknown address = %+v, want nil", link)
	}

	env.netCfg.Links = nil
	if link := newTestTosser(t, env).findLinkForNetmail(&jam.FidoAddress{Zone: 21, Net: 4, Node: 158}); link != nil {
		t.Errorf("with no links configured got %+v, want nil", link)
	}
}

func TestLinkMsgAttr(t *testing.T) {
	tests := map[string]uint16{
		"Crash":  ftn.MsgAttrLocal | ftn.MsgAttrCrash,
		"HOLD":   ftn.MsgAttrLocal | ftn.MsgAttrHold,
		"direct": ftn.MsgAttrLocal,
		"Normal": ftn.MsgAttrLocal,
		"":       ftn.MsgAttrLocal,
	}
	for flavour, want := range tests {
		if got := linkMsgAttr(flavour); got != want {
			t.Errorf("linkMsgAttr(%q) = %#04x, want %#04x", flavour, got, want)
		}
	}
}

// With no usable link address there is nothing to compare a packet's origin
// against; refusing everything would stall all inbound mail, so it is accepted.
func TestPacketAcceptedWhenNoLinkAddressParses(t *testing.T) {
	env := setupTestEnv(t)
	env.netCfg.Links = []linkConfig{{Address: "bogus", Name: "Unparseable"}}
	tosser := newTestTosser(t, env)

	writeGoodPacket(t, env.inboundDir, "any00001.pkt", 3, 633, 2744)
	result := tosser.ProcessInbound()
	if result.PacketsProcessed != 1 || result.MessagesImported != 1 || len(result.Errors) != 0 {
		t.Errorf("result = %+v, want the packet tossed", result)
	}
}

// --- operator-facing log lines ---

func TestWarnOrphanFTNAreas(t *testing.T) {
	areas := []*message.MessageArea{
		{Tag: "zer0net_netmail", AreaType: "netmail", Network: "zer0net"},
		{Tag: "0N-BBS", AreaType: "echomail", Network: "zeronet"},
		{Tag: "AAA_ORPHAN", AreaType: "echo", Network: "gone"},
	}

	// No FTN networks at all means FTN is not in use: say nothing.
	logs := captureLog(t)
	WarnOrphanFTNAreas(config.FTNConfig{}, areas)
	if out := logs(); out != "" {
		t.Errorf("logged with no networks configured: %s", out)
	}

	cfg := config.FTNConfig{Networks: map[string]config.FTNNetworkConfig{"zeronet": {}, "fsxnet": {}}}
	WarnOrphanFTNAreas(cfg, areas)
	lines := strings.Split(strings.TrimSpace(logs()), "\n")
	if len(lines) != 2 {
		t.Fatalf("logged %d lines, want one per orphan area:\n%s", len(lines), logs())
	}
	for i, want := range []string{"area=AAA_ORPHAN", "area=zer0net_netmail"} {
		if !strings.Contains(lines[i], "level=WARN") || !strings.Contains(lines[i], want) ||
			!strings.Contains(lines[i], "ftn_networks=fsxnet,zeronet") {
			t.Errorf("line %d = %s\nwant a warning for %s listing the configured networks", i, lines[i], want)
		}
	}
	if !strings.Contains(lines[1], "network=zer0net") || !strings.Contains(lines[1], "type=netmail") {
		t.Errorf("warning does not name the area's network and type: %s", lines[1])
	}
}

func TestUnclaimedReportLog(t *testing.T) {
	oldest := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

	t.Run("nothing to report", func(t *testing.T) {
		logs := captureLog(t)
		UnclaimedReport{}.Log()
		if out := logs(); out != "" {
			t.Errorf("empty report logged: %s", out)
		}
	})

	t.Run("held mail only", func(t *testing.T) {
		logs := captureLog(t)
		UnclaimedReport{Held: []string{"a.pkt", "b.pkt"}}.Log()
		out := strings.TrimSpace(logs())
		if strings.Count(out, "\n") != 0 || !strings.Contains(out, "level=INFO") || !strings.Contains(out, "files=2") {
			t.Errorf("held mail should be one Info line counting 2 files, got:\n%s", out)
		}
	})

	t.Run("unclaimed and quarantined", func(t *testing.T) {
		logs := captureLog(t)
		UnclaimedReport{
			Files:       []string{"/in/x.mo0"},
			Origins:     map[string]int{"21:1/100": 3, "3:633/2744": 5, "1:1/1": 3},
			Oldest:      oldest,
			Quarantined: []string{filepath.Join("temp", UnclaimedDirName, "y.mo0"), filepath.Join("temp", UnclaimedDirName, "z.mo0")},
		}.Log()
		out := strings.TrimSpace(logs())
		if strings.Count(out, "\n") != 0 || !strings.Contains(out, "level=WARN") {
			t.Fatalf("want a single warning, got:\n%s", out)
		}
		for _, want := range []string{
			"files=3",
			// Busiest origin first, ties in address order.
			`origins="3:633/2744 (5 packets), 1:1/1 (3 packets), 21:1/100 (3 packets)"`,
			"oldest=2026-01-02T03:04:05Z",
			"quarantined=2",
			"quarantine_dir=" + filepath.Join("temp", UnclaimedDirName),
		} {
			if !strings.Contains(out, want) {
				t.Errorf("warning missing %s:\n%s", want, out)
			}
		}
	})

	t.Run("quarantined only", func(t *testing.T) {
		logs := captureLog(t)
		UnclaimedReport{Quarantined: []string{filepath.Join("temp", UnclaimedDirName, "y.mo0")}}.Log()
		out := logs()
		if !strings.Contains(out, "level=WARN") || !strings.Contains(out, "files=1") || strings.Contains(out, "origins=") || strings.Contains(out, "oldest=") {
			t.Errorf("want a warning for 1 file with no origins or age, got:\n%s", out)
		}
	})
}
