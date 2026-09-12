package tosser

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/ftn"
)

// writeGoodPacket writes a one-message packet from the given origin into dir.
func writeGoodPacket(t *testing.T, dir, name string, origZone, origNet, origNode uint16) string {
	t.Helper()
	hdr := ftn.NewPacketHeader(origZone, origNet, origNode, 0, 21, 4, 158, 1, "")
	body := &ftn.ParsedBody{
		Area:    "FSX_TEST",
		Text:    "hello\r",
		Kludges: []string{"MSGID: 21:4/158 11111111"},
	}
	packed := &ftn.PackedMessage{
		MsgType: 2, OrigNode: origNode, DestNode: 158,
		OrigNet: origNet, DestNet: 4,
		DateTime: "01 Mar 26  12:00:00", To: "All", From: "Someone",
		Subject: "hi", Body: ftn.FormatPackedMessageBody(body),
	}
	var buf bytes.Buffer
	if err := ftn.WritePacket(&buf, hdr, []*ftn.PackedMessage{packed}); err != nil {
		t.Fatalf("WritePacket: %v", err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, buf.Bytes(), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

// packetBytes builds a packet from our hub carrying n identical messages.
func packetBytes(t *testing.T, n int) []byte {
	t.Helper()
	hdr := ftn.NewPacketHeader(21, 4, 158, 0, 21, 4, 158, 1, "")
	body := &ftn.ParsedBody{
		Area:    "FSX_TEST",
		Text:    strings.Repeat("padding to make the message long enough to cut into\r", 4),
		Kludges: []string{"MSGID: 21:4/158 22222222"},
	}
	msgs := make([]*ftn.PackedMessage, 0, n)
	for i := 0; i < n; i++ {
		msgs = append(msgs, &ftn.PackedMessage{
			MsgType: 2, OrigNode: 158, DestNode: 158, OrigNet: 4, DestNet: 4,
			DateTime: "01 Mar 26  12:00:00", To: "All", From: "Someone",
			Subject: "hi", Body: ftn.FormatPackedMessageBody(body),
		})
	}
	var buf bytes.Buffer
	if err := ftn.WritePacket(&buf, hdr, msgs); err != nil {
		t.Fatalf("WritePacket: %v", err)
	}
	return buf.Bytes()
}

// readPacketFile parses a packet from disk, for asserting on test fixtures.
func readPacketFile(t *testing.T, path string) (*ftn.PacketHeader, []*ftn.PackedMessage, error) {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	return ftn.ReadPacket(f)
}

// TestCorruptPacketIsQuarantinedNotPartiallyImported covers a packet whose
// parse fails partway. Messages read before the failing offset used to be
// imported and the packet then deleted as processed — but a corrupt packet can
// desync the reader well before it notices, so those "messages" may be
// mid-body bytes. One reached a live sysop's netmail inbox with a strip of
// ANSI art as its subject (#276). Nothing should be imported, and the packet
// must survive for inspection.
func TestCorruptPacketIsQuarantinedNotPartiallyImported(t *testing.T) {
	env := setupTestEnv(t)
	tsr, err := New("testnet", env.netCfg, env.globalCfg, env.dupeDB, env.msgMgr)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// Two messages from our own hub, truncated inside the second so the
	// reader parses the first cleanly and then runs off the end. That prefix
	// is exactly what the old code imported.
	oneMsg := packetBytes(t, 1)
	twoMsg := packetBytes(t, 2)
	// A packet ends with a two-byte terminator, so the single-message packet's
	// length less that terminator is where the second message begins.
	cut := len(oneMsg) - 2 + 16
	if cut >= len(twoMsg) {
		t.Fatalf("truncation offset %d is not inside the second message (packet is %d bytes)", cut, len(twoMsg))
	}
	full := filepath.Join(env.inboundDir, "corrupt.pkt")
	if err := os.WriteFile(full, twoMsg[:cut], 0644); err != nil {
		t.Fatal(err)
	}

	// Guard the guard: the truncated packet must still yield a parse error
	// with at least one message recovered, or this test proves nothing.
	if _, msgs, err := readPacketFile(t, full); err == nil || len(msgs) == 0 {
		t.Fatalf("test fixture is wrong: want a partial parse, got err=%v msgs=%d", err, len(msgs))
	}

	result := tsr.ProcessInbound()

	if result.MessagesImported != 0 {
		t.Errorf("imported %d message(s) from a packet that failed to parse; want 0",
			result.MessagesImported)
	}
	if len(result.Errors) == 0 {
		t.Error("a packet that failed to parse should be reported as an error")
	}
	if _, err := os.Stat(full); !os.IsNotExist(err) {
		t.Error("the corrupt packet should have been moved out of the inbound directory")
	}
	quarantined := filepath.Join(env.tempDir, "corrupt.pkt")
	if _, err := os.Stat(quarantined); err != nil {
		t.Errorf("the corrupt packet should be held for inspection at %s: %v", quarantined, err)
	}
}

// TestSkippedOriginsAreRecorded covers the bookkeeping that makes unclaimed
// mail visible. Skipping another network's packet is routine and must stay
// quiet per packet, but the origin has to be recorded so a full pass can
// report what nobody took.
func TestSkippedOriginsAreRecorded(t *testing.T) {
	env, extCfg := setupExtendedTestEnv(t)
	tsr, err := New("testnet", extCfg, env.globalCfg, env.dupeDB, env.msgMgr)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	path := writeGoodPacket(t, env.inboundDir, "foreign.pkt", 1337, 3, 123)

	result := tsr.ProcessInbound()

	if got := result.SkippedByFile[path]["1337:3/123"]; got != 1 {
		t.Errorf("SkippedByFile[%s][1337:3/123] = %d, want 1 (got %v)",
			path, got, result.SkippedByFile)
	}
}

// TestSkippedOriginsAreDroppedForClaimedFiles covers the reporting rule that
// matters when networks share an inbound directory. Every network declines the
// others' mail, so an origin recorded during one pass says nothing on its own.
// Only files still present after the whole pass are unclaimed, and only their
// origins may be named — otherwise a single genuinely stuck file would drag
// every address any network ever declined into the warning.
func TestSkippedOriginsAreDroppedForClaimedFiles(t *testing.T) {
	env := setupTestEnv(t)

	claimed := filepath.Join(env.inboundDir, "claimed.pkt")
	stuck := writeGoodPacket(t, env.inboundDir, "stuck.pkt", 1337, 3, 123)

	// Pretend one network declined both files, then another took the first —
	// which is what removing it from the inbound directory represents.
	skipped := map[string]map[string]int{
		claimed: {"21:4/158": 9},
		stuck:   {"1337:3/123": 2},
	}

	report := FindUnclaimed(env.globalCfg, skipped)

	if _, named := report.Origins["21:4/158"]; named {
		t.Errorf("origin of a file that was claimed must not be reported: %v", report.Origins)
	}
	if got := report.Origins["1337:3/123"]; got != 2 {
		t.Errorf("Origins[1337:3/123] = %d, want 2: %v", got, report.Origins)
	}
}

// TestSkippedCountsAreNotMultipliedByNetworkCount pins the counting rule. Each
// enabled network passes over the same packets, so summing their reports would
// multiply the total by the number of networks configured.
func TestSkippedCountsAreNotMultipliedByNetworkCount(t *testing.T) {
	env := setupTestEnv(t)
	stuck := writeGoodPacket(t, env.inboundDir, "stuck.pkt", 1337, 3, 123)

	report := FindUnclaimed(env.globalCfg, map[string]map[string]int{
		stuck: {"1337:3/123": 5},
	})

	if got := report.Origins["1337:3/123"]; got != 5 {
		t.Errorf("Origins[1337:3/123] = %d, want the per-file count of 5", got)
	}
}

// TestFindUnclaimedReportsLeftoverMail covers the whole-pass check: whatever is
// still in the inbound directory once every network has run is mail no
// configured network would take.
func TestFindUnclaimedReportsLeftoverMail(t *testing.T) {
	env := setupTestEnv(t)
	writeGoodPacket(t, env.inboundDir, "leftover.pkt", 1337, 3, 123)

	report := FindUnclaimed(env.globalCfg, map[string]map[string]int{
		filepath.Join(env.inboundDir, "leftover.pkt"): {"1337:3/123": 42},
	})

	if report.Empty() {
		t.Fatal("expected the leftover packet to be reported")
	}
	if len(report.Files) != 1 {
		t.Errorf("got %d files, want 1: %v", len(report.Files), report.Files)
	}
	if !strings.Contains(report.OriginList(), "1337:3/123 (42 packets)") {
		t.Errorf("origin list should name the address and count, got %q", report.OriginList())
	}
	if report.Oldest.IsZero() {
		t.Error("report should carry the oldest file's timestamp")
	}
}

// TestQuarantineStaleMovesOnlyOldFiles covers the age gate. Mail that arrived
// while a network was briefly misconfigured must stay put so a same-day fix
// still tosses it; only a genuine backlog is moved aside, and moved rather
// than deleted so it can be recovered.
func TestQuarantineStaleMovesOnlyOldFiles(t *testing.T) {
	env := setupTestEnv(t)
	fresh := writeGoodPacket(t, env.inboundDir, "fresh.pkt", 1337, 3, 123)
	stale := writeGoodPacket(t, env.inboundDir, "stale.pkt", 1337, 3, 123)

	old := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(stale, old, old); err != nil {
		t.Fatal(err)
	}

	report := FindUnclaimed(env.globalCfg, nil)
	report.QuarantineStale(env.tempDir)

	if len(report.Quarantined) != 1 {
		t.Fatalf("quarantined %d file(s), want 1: %v", len(report.Quarantined), report.Quarantined)
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Errorf("a file younger than the cutoff must be left in place: %v", err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Error("the stale file should have been moved out of the inbound directory")
	}
	moved := filepath.Join(env.tempDir, UnclaimedDirName, "stale.pkt")
	if _, err := os.Stat(moved); err != nil {
		t.Errorf("the stale file should be recoverable at %s: %v", moved, err)
	}
	if len(report.Files) != 1 || filepath.Base(report.Files[0]) != "fresh.pkt" {
		t.Errorf("report should still list the file left behind, got %v", report.Files)
	}
}

// disabledNetCfg returns a global config carrying one enabled and one disabled
// network, the shape a sysop has while setting a new network up.
func disabledNetCfg(env *testEnv) config.FTNConfig {
	cfg := env.globalCfg
	cfg.Networks = map[string]config.FTNNetworkConfig{
		"fsxnet": {
			InternalTosserEnabled: true,
			Links:                 []config.FTNLinkConfig{{Address: "21:4/158"}},
		},
		"tqwnet": {
			InternalTosserEnabled: false,
			Links:                 []config.FTNLinkConfig{{Address: "1337:3/123"}},
		},
	}
	return cfg
}

// Mail for a network the sysop switched off is waiting on them, not
// unclaimed. Ageing it out at 24h would quarantine exactly the mail someone
// deliberately paused while setting up areas — a weekend is longer than the
// cutoff.
func TestDisabledNetworkMailIsHeldNotQuarantined(t *testing.T) {
	env := setupTestEnv(t)
	stale := writeGoodPacket(t, env.inboundDir, "tqw.pkt", 1337, 3, 123)
	old := time.Now().Add(-72 * time.Hour)
	if err := os.Chtimes(stale, old, old); err != nil {
		t.Fatal(err)
	}

	report := FindUnclaimed(disabledNetCfg(env), map[string]map[string]int{
		stale: {"1337:3/123": 7},
	})
	report.QuarantineStale(env.tempDir)

	if len(report.Held) != 1 || filepath.Base(report.Held[0]) != "tqw.pkt" {
		t.Fatalf("want the packet reported as held, got Held=%v Files=%v", report.Held, report.Files)
	}
	if len(report.Quarantined) != 0 {
		t.Errorf("held mail must never be quarantined, got %v", report.Quarantined)
	}
	if _, err := os.Stat(stale); err != nil {
		t.Errorf("held mail must stay in the inbound directory: %v", err)
	}
	if !report.Empty() {
		t.Error("held mail must not count as unclaimed")
	}
}

// The #276 protection has to survive: mail from an address no network claims
// is still quarantined even while some other network is disabled.
func TestUnclaimedMailStillQuarantinedAlongsideDisabledNetwork(t *testing.T) {
	env := setupTestEnv(t)
	stale := writeGoodPacket(t, env.inboundDir, "nobody.pkt", 99, 1, 1)
	old := time.Now().Add(-72 * time.Hour)
	if err := os.Chtimes(stale, old, old); err != nil {
		t.Fatal(err)
	}

	report := FindUnclaimed(disabledNetCfg(env), map[string]map[string]int{
		stale: {"99:1/1": 3},
	})
	report.QuarantineStale(env.tempDir)

	if len(report.Held) != 0 {
		t.Errorf("mail from an unknown address is not held, got %v", report.Held)
	}
	if len(report.Quarantined) != 1 {
		t.Fatalf("want the stale unclaimed packet quarantined, got %v", report.Quarantined)
	}
	if !strings.Contains(report.OriginList(), "99:1/1") {
		t.Errorf("origin should still be named, got %q", report.OriginList())
	}
}

// With every network enabled nothing is held and the original ageing applies
// unchanged.
func TestAllNetworksEnabledHoldsNothing(t *testing.T) {
	env := setupTestEnv(t)
	stale := writeGoodPacket(t, env.inboundDir, "stale.pkt", 1337, 3, 123)
	old := time.Now().Add(-72 * time.Hour)
	if err := os.Chtimes(stale, old, old); err != nil {
		t.Fatal(err)
	}

	cfg := env.globalCfg
	cfg.Networks = map[string]config.FTNNetworkConfig{
		"fsxnet": {InternalTosserEnabled: true, Links: []config.FTNLinkConfig{{Address: "21:4/158"}}},
	}

	report := FindUnclaimed(cfg, map[string]map[string]int{stale: {"1337:3/123": 1}})
	report.QuarantineStale(env.tempDir)

	if len(report.Held) != 0 {
		t.Errorf("nothing should be held with every network enabled, got %v", report.Held)
	}
	if len(report.Quarantined) != 1 {
		t.Errorf("stale unclaimed mail should still be quarantined, got %v", report.Quarantined)
	}
}

// Nothing parsed the file — which is what happens when every network is
// disabled — so it cannot be attributed. Mail is not aged out on a guess.
func TestUnattributableMailIsHeldWhenEveryNetworkIsDisabled(t *testing.T) {
	env := setupTestEnv(t)
	stale := writeGoodPacket(t, env.inboundDir, "mystery.pkt", 1337, 3, 123)
	old := time.Now().Add(-72 * time.Hour)
	if err := os.Chtimes(stale, old, old); err != nil {
		t.Fatal(err)
	}

	cfg := disabledNetCfg(env)
	for name, nc := range cfg.Networks {
		nc.InternalTosserEnabled = false
		cfg.Networks[name] = nc
	}
	report := FindUnclaimed(cfg, nil) // no origins recorded
	report.QuarantineStale(env.tempDir)

	if len(report.Held) != 1 {
		t.Fatalf("unattributable mail should be held, got Held=%v Files=%v", report.Held, report.Files)
	}
	if len(report.Quarantined) != 0 {
		t.Errorf("unattributable mail must not be quarantined, got %v", report.Quarantined)
	}
}

// With an enabled network in the mix, a file with no recorded origin is one
// the enabled tosser saw and could not attribute — unreadable, say. Holding it
// because some unrelated network is switched off would let it bypass the
// 24-hour quarantine indefinitely.
func TestUnattributableMailIsNotHeldWhileANetworkIsEnabled(t *testing.T) {
	env := setupTestEnv(t)
	stale := writeGoodPacket(t, env.inboundDir, "mystery.pkt", 1337, 3, 123)
	old := time.Now().Add(-72 * time.Hour)
	if err := os.Chtimes(stale, old, old); err != nil {
		t.Fatal(err)
	}

	report := FindUnclaimed(disabledNetCfg(env), nil) // fsxnet enabled, tqwnet off; no origins
	report.QuarantineStale(env.tempDir)

	if len(report.Held) != 0 {
		t.Errorf("origin-less mail must not be held on an unrelated disabled network, got %v", report.Held)
	}
	if len(report.Quarantined) != 1 {
		t.Errorf("stale origin-less mail should be quarantined, got Files=%v Quarantined=%v",
			report.Files, report.Quarantined)
	}
}
