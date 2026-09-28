package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/filelock"
	"github.com/ViSiON-3/vision-3-bbs/internal/ftn"
	"github.com/ViSiON-3/vision-3-bbs/internal/tosser"
)

// ftnJSON configures network "testnet" (us 21:4/158.1, hub 21:4/158 with the
// given hostname) and a switched-off "offnet" whose hub is 21:9/9.
func ftnJSON(hostname string) string {
	return `{
  "dupe_db_path": "data/ftn/dupes.json",
  "inbound_path": "data/ftn/in",
  "outbound_path": "data/ftn/temp_out",
  "binkd_outbound_path": "data/ftn/out",
  "temp_path": "data/ftn/temp_in",
  "networks": {
    "testnet": {
      "internal_tosser_enabled": true,
      "own_address": "21:4/158.1",
      "links": [{"address": "21:4/158", "name": "Test Hub", "hostname": "` + hostname + `"}]
    },
    "offnet": {
      "internal_tosser_enabled": false,
      "own_address": "21:9/1",
      "links": [{"address": "21:9/9", "name": "Off Hub"}]
    }
  }
}`
}

// newFTNBBS builds a BBS carrying echo FSX_TEST on testnet (area 1) beside a
// local area (area 2).
func newFTNBBS(t *testing.T, hostname string) *bbs {
	t.Helper()
	b := newBBS(t)
	b.writeConfig(t, "ftn.json", ftnJSON(hostname))
	b.writeAreas(t,
		map[string]any{"id": 1, "tag": "FSX_TEST", "name": "FSX Test", "base_path": "msgbases/fsx_test",
			"area_type": "echomail", "echo_tag": "FSX_TEST", "network": "testnet"},
		map[string]any{"id": 2, "tag": "GENERAL", "name": "General", "base_path": "msgbases/general", "area_type": "local"},
	)
	for _, d := range []string{"in", "temp_out", "out", "temp_in"} {
		if err := os.MkdirAll(filepath.Join(b.dataDir, "ftn", d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return b
}

// inbound returns the path of name in the FTN inbound directory.
func (b *bbs) inbound(name string) string {
	return filepath.Join(b.dataDir, "ftn", "in", name)
}

// makePkt builds a Type-2+ packet from origNet/origNode in zone 21 to our
// point, carrying one echomail message for area.
func makePkt(t *testing.T, origNet, origNode int, area, subject, msgID string) []byte {
	t.Helper()
	hdr := ftn.NewPacketHeader(21, uint16(origNet), uint16(origNode), 0, 21, 4, 158, 1, "")
	body := &ftn.ParsedBody{
		Area:    area,
		Text:    "Hello from the hub\r",
		Kludges: []string{"MSGID: " + msgID},
		SeenBy:  []string{"4/158"},
		Path:    []string{"4/158"},
	}
	msg := &ftn.PackedMessage{
		MsgType: 2, OrigNode: uint16(origNode), DestNode: 158, OrigNet: uint16(origNet), DestNet: 4,
		DateTime: "21 Feb 26  12:00:00", To: "All", From: "Hub Sysop", Subject: subject,
		Body: ftn.FormatPackedMessageBody(body),
	}
	var buf bytes.Buffer
	if err := ftn.WritePacket(&buf, hdr, []*ftn.PackedMessage{msg}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// writeFile writes data to path, failing the test on error.
func writeFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

// listDir returns the names in dir, or nil when it does not exist.
func listDir(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

// toss imports an inbound packet from the hub into the echo's JAM base and
// removes it; the same packet again is caught as a dupe.
func TestCmdTossImportsPacket(t *testing.T) {
	b := newFTNBBS(t, "")
	pkt := makePkt(t, 4, 158, "FSX_TEST", "Hello echo", "21:4/158 11111111")
	writeFile(t, b.inbound("0001.pkt"), pkt)

	out, errOut := capture(t, func() { cmdToss(b.flags()) })
	wantContains(t, "toss", out, "[testnet] toss: 1 packets, 1 imported, 0 dupes",
		"Toss complete: 1 packets, 1 messages imported, 0 dupes skipped")
	if errOut != "" {
		t.Errorf("stderr = %q", errOut)
	}
	if _, err := os.Stat(b.inbound("0001.pkt")); !os.IsNotExist(err) {
		t.Error("tossed packet left in inbound")
	}
	base := openBase(t, filepath.Join(b.dataDir, "msgbases", "fsx_test"))
	msg, err := base.ReadMessage(1)
	if err != nil || msg.Subject != "Hello echo" || msg.From != "Hub Sysop" {
		t.Fatalf("tossed message = %+v, %v", msg, err)
	}
	_ = base.Close()

	writeFile(t, b.inbound("0002.pkt"), pkt)
	out, _ = capture(t, func() { cmdToss(append([]string{"-q"}, b.flags()...)) })
	if out != "" {
		t.Errorf("quiet toss printed %q", out)
	}
	if total, _ := counts(t, filepath.Join(b.dataDir, "msgbases", "fsx_test")); total != 1 {
		t.Errorf("dupe was imported: %d messages", total)
	}
}

// Mail from an address no network links to is left in place and reported;
// once it is older than a day it is quarantined and the run fails. Mail from
// a switched-off network's hub is held, not flagged.
func TestTossFTNUnclaimedMail(t *testing.T) {
	b := newFTNBBS(t, "")
	stranger := b.inbound("stranger.pkt")
	writeFile(t, stranger, makePkt(t, 7, 7, "FSX_TEST", "Who am I", "21:7/7 22222222"))
	writeFile(t, b.inbound("held.pkt"), makePkt(t, 9, 9, "FSX_TEST", "Waiting", "21:9/9 33333333"))

	ftnCfg, msgMgr, dupeDB, err := loadFTNDeps(b.configDir, b.dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = msgMgr.Close() }()

	var failed bool
	out, _ := capture(t, func() { failed = tossFTN(ftnCfg, msgMgr, dupeDB, "", false) })
	if failed {
		t.Error("freshly unclaimed mail failed the run")
	}
	wantContains(t, "toss", out, "1 inbound file(s) held for a network whose tosser is off",
		"WARNING: 1 inbound file(s) matched no configured network — from 21:7/7",
		"Check that each network's links list the address")
	if _, err := os.Stat(stranger); err != nil {
		t.Errorf("unclaimed packet moved too early: %v", err)
	}

	old := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(stranger, old, old); err != nil {
		t.Fatal(err)
	}
	out, _ = capture(t, func() { failed = tossFTN(ftnCfg, msgMgr, dupeDB, "", false) })
	if !failed {
		t.Error("quarantined backlog did not fail the run")
	}
	quarantine := filepath.Join(b.dataDir, "ftn", "temp_in", tosser.UnclaimedDirName)
	wantContains(t, "toss", out, "1 moved to "+quarantine)
	if names := listDir(t, quarantine); len(names) != 1 || names[0] != "stranger.pkt" {
		t.Errorf("quarantine holds %v, want stranger.pkt", names)
	}

	// Limited to one network the unclaimed check is skipped entirely.
	writeFile(t, stranger, makePkt(t, 7, 7, "FSX_TEST", "Again", "21:7/7 44444444"))
	if err := os.Chtimes(stranger, old, old); err != nil {
		t.Fatal(err)
	}
	out, _ = capture(t, func() { failed = tossFTN(ftnCfg, msgMgr, dupeDB, "testnet", false) })
	if failed || strings.Contains(out, "WARNING") {
		t.Errorf("--network run judged unclaimed mail: failed=%v\n%s", failed, out)
	}
}

// A network whose tosser cannot be built is reported and fails the run for
// toss, scan and ftn-pack alike; other networks are filtered by name.
func TestFTNCommandsReportBrokenNetwork(t *testing.T) {
	b := newFTNBBS(t, "")
	ftnCfg, msgMgr, dupeDB, err := loadFTNDeps(b.configDir, b.dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = msgMgr.Close() }()
	ftnCfg.Networks["broken"] = config.FTNNetworkConfig{InternalTosserEnabled: true, OwnAddress: "not-an-address"}

	for name, run := range map[string]func(string) bool{
		"toss":     func(n string) bool { return tossFTN(ftnCfg, msgMgr, dupeDB, n, true) },
		"scan":     func(n string) bool { return scanFTN(ftnCfg, msgMgr, dupeDB, n, true) },
		"ftn-pack": func(n string) bool { return packFTN(ftnCfg, msgMgr, dupeDB, n, true) },
	} {
		var failed bool
		_, errOut := capture(t, func() { failed = run("") })
		if !failed || !strings.Contains(errOut, "Error creating tosser for broken") {
			t.Errorf("%s: failed=%v stderr=%q", name, failed, errOut)
		}
		_, errOut = capture(t, func() { failed = run("testnet") })
		if failed || errOut != "" {
			t.Errorf("%s --network testnet: failed=%v stderr=%q", name, failed, errOut)
		}
	}
}

// scan exports a local post in the echo to a staged packet, and ftn-pack
// bundles that packet into the binkd outbound.
func TestCmdScanThenFtnPack(t *testing.T) {
	b := newFTNBBS(t, "")
	_, msgMgr, _, err := loadFTNDeps(b.configDir, b.dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := msgMgr.AddMessage(1, "Sysop", "All", "Outbound", "Going out", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := msgMgr.AddMessage(2, "Sysop", "All", "Local", "Stays home", ""); err != nil {
		t.Fatal(err)
	}
	_ = msgMgr.Close()

	out, _ := capture(t, func() { cmdScan(b.flags()) })
	wantContains(t, "scan", out, "[testnet] scan: 1 messages exported", "Scan complete: 1 messages exported to outbound")
	staged := listDir(t, filepath.Join(b.dataDir, "ftn", "temp_out"))
	if len(staged) != 1 || !strings.HasSuffix(strings.ToLower(staged[0]), ".pkt") {
		t.Fatalf("staged outbound = %v, want one .pkt", staged)
	}

	out, _ = capture(t, func() { cmdFtnPack(b.flags()) })
	wantContains(t, "ftn-pack", out, "[testnet] ftn-pack: 1 bundles created (1 packets)", "Pack complete: 1 bundles created, 1 packets packed")
	if left := listDir(t, filepath.Join(b.dataDir, "ftn", "temp_out")); len(left) != 0 {
		t.Errorf("staged packets left after pack: %v", left)
	}
	if bundles := listDir(t, filepath.Join(b.dataDir, "ftn", "out")); len(bundles) == 0 {
		t.Error("no bundle in the binkd outbound")
	}

	// Nothing new: a quiet scan prints nothing.
	out, _ = capture(t, func() { cmdScan(append([]string{"-q"}, b.flags()...)) })
	if out != "" {
		t.Errorf("quiet scan printed %q", out)
	}
}

// loadFTNDeps names the step that failed: reading ftn.json, validating it,
// or reading the dupe database; a blank dupe path defaults under data/ftn.
func TestLoadFTNDepsErrors(t *testing.T) {
	b := newFTNBBS(t, "")

	b.writeConfig(t, "ftn.json", "{broken")
	if _, _, _, err := loadFTNDeps(b.configDir, b.dataDir); err == nil || !strings.Contains(err.Error(), "load ftn config") {
		t.Errorf("bad JSON: err = %v", err)
	}

	b.writeConfig(t, "ftn.json", `{"networks":{"n":{"internal_tosser_enabled":true,"own_address":"1:1/1"}}}`)
	if _, _, _, err := loadFTNDeps(b.configDir, b.dataDir); err == nil || !strings.Contains(err.Error(), "ftn config invalid") {
		t.Errorf("missing paths: err = %v", err)
	}

	b.writeConfig(t, "ftn.json", ftnJSON(""))
	// A directory where the dupe file belongs cannot be read.
	if err := os.Mkdir(filepath.Join(b.dataDir, "ftn", "dupes.json"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := loadFTNDeps(b.configDir, b.dataDir); err == nil || !strings.Contains(err.Error(), "load dupe db") {
		t.Errorf("unreadable dupe db: err = %v", err)
	}

	b.writeConfig(t, "ftn.json", strings.Replace(ftnJSON(""), `"dupe_db_path": "data/ftn/dupes.json",`, "", 1))
	if err := os.Remove(filepath.Join(b.dataDir, "ftn", "dupes.json")); err != nil {
		t.Fatal(err)
	}
	cfg, msgMgr, dupeDB, err := loadFTNDeps(b.configDir, b.dataDir)
	if err != nil || dupeDB == nil {
		t.Fatalf("defaulted dupe path: %v", err)
	}
	_ = msgMgr.Close()
	if want := filepath.Join(b.root, "data", "ftn", "in"); cfg.InboundPath != want {
		t.Errorf("inbound resolved to %q, want %q", cfg.InboundPath, want)
	}
}

// toss, scan and ftn-pack exit 1 with the load error when ftn.json is broken.
func TestFTNCommandsExitOnBadConfig(t *testing.T) {
	b := newFTNBBS(t, "")
	b.writeConfig(t, "ftn.json", "{broken")
	for _, c := range []string{"toss", "scan", "ftn-pack"} {
		code, _, errOut := runV3mail(t, b.root, append([]string{c}, b.flags()...)...)
		if code != 1 || !strings.Contains(errOut, "Error: load ftn config") {
			t.Errorf("%s: exit %d, stderr %q", c, code, errOut)
		}
	}
}

// pollFTN with no enabled network does nothing, not even create the dupe
// database.
func TestPollFTNNoNetworks(t *testing.T) {
	b := newBBS(t)
	var failed bool
	out, _ := capture(t, func() {
		failed = pollFTN(context.Background(), b.configDir, b.dataDir, "", time.Minute, false)
	})
	if failed || !strings.Contains(out, "FTN: no enabled networks") {
		t.Errorf("failed=%v out=%q", failed, out)
	}
	if _, err := os.Stat(filepath.Join(b.dataDir, "ftn")); !os.IsNotExist(err) {
		t.Error("poll with no FTN networks created data/ftn")
	}
}

// A hub with no hostname cannot be called: pollFTN says so, and still packs
// and tosses what is waiting.
func TestPollFTNUncallableHub(t *testing.T) {
	b := newFTNBBS(t, "")
	writeFile(t, b.inbound("0001.pkt"), makePkt(t, 4, 158, "FSX_TEST", "Waiting", "21:4/158 55555555"))
	var failed bool
	out, _ := capture(t, func() {
		failed = pollFTN(context.Background(), b.configDir, b.dataDir, "", time.Minute, false)
	})
	if failed {
		t.Errorf("poll failed:\n%s", out)
	}
	wantContains(t, "poll", out, "== FTN ==", "[testnet] 21:4/158 has no hostname", "[testnet] scan:", "[testnet] ftn-pack:",
		"[testnet] toss: 1 packets, 1 imported")
}

// A callable hub without binkd.conf is not called, fails the poll, and the
// inbound is still tossed.
func TestPollFTNMissingBinkdConf(t *testing.T) {
	b := newFTNBBS(t, "hub.example")
	var failed bool
	out, errOut := capture(t, func() {
		failed = pollFTN(context.Background(), b.configDir, b.dataDir, "testnet", time.Minute, false)
	})
	if !failed {
		t.Error("poll without binkd.conf succeeded")
	}
	wantContains(t, "stderr", errOut, filepath.Join(b.root, "data", "ftn", "binkd.conf")+" not found")
	wantContains(t, "stdout", out, "Toss complete")
}

// With binkd.conf in place each hub is called with the configured binkd (a
// stand-in script here); a stop request skips the calls but still tosses.
func TestPollFTNCallsHub(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a shell script as a stand-in for binkd")
	}
	b := newFTNBBS(t, "hub.example")
	writeFile(t, filepath.Join(b.dataDir, "ftn", "binkd.conf"), []byte("# test\n"))
	if err := os.MkdirAll(filepath.Join(b.root, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	argsFile := filepath.Join(b.root, "args")
	script := "#!/bin/sh\necho \"$@\" > " + argsFile + "\necho '+ 01 Jan 00:00:00 [1] sent: x.pkt (1, 1.00 CPS, 21:4/158@testnet)'\n"
	if err := os.WriteFile(filepath.Join(b.root, "bin", "binkd"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	var failed bool
	out, _ := capture(t, func() {
		failed = pollFTN(context.Background(), b.configDir, b.dataDir, "", time.Minute, false)
	})
	if failed {
		t.Errorf("poll failed:\n%s", out)
	}
	wantContains(t, "poll", out, "[testnet] calling 21:4/158...", "[testnet] 21:4/158: sent 1, received 0 file(s)")
	got, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatal(err)
	}
	if want := "-p -P 21:4/158@testnet " + filepath.Join(b.root, "data", "ftn", "binkd.conf"); strings.TrimSpace(string(got)) != want {
		t.Errorf("binkd args = %q, want %q", strings.TrimSpace(string(got)), want)
	}

	if err := os.Remove(argsFile); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	out, _ = capture(t, func() { failed = pollFTN(ctx, b.configDir, b.dataDir, "", time.Minute, false) })
	if !failed {
		t.Error("cancelled poll reported success")
	}
	if _, err := os.Stat(argsFile); !os.IsNotExist(err) {
		t.Error("binkd was run after the poll was cancelled")
	}
	wantContains(t, "cancelled poll", out, "Toss complete")
}

// pollFTN reports a broken ftn.json rather than calling anything.
func TestPollFTNBadConfig(t *testing.T) {
	b := newFTNBBS(t, "")
	b.writeConfig(t, "ftn.json", `{"networks":{"n":{"internal_tosser_enabled":true,"own_address":"1:1/1"}}}`)
	var failed bool
	_, errOut := capture(t, func() {
		failed = pollFTN(context.Background(), b.configDir, b.dataDir, "", time.Minute, false)
	})
	if !failed || !strings.Contains(errOut, "FTN: ftn config invalid") {
		t.Errorf("failed=%v stderr=%q", failed, errOut)
	}
}

// poll runs the FTN exchange and reports no QWK networks when qwknet.json has
// none enabled; --network narrows it to the named FTN network.
func TestCmdPollFTNOnly(t *testing.T) {
	b := newFTNBBS(t, "")
	out, _ := capture(t, func() { cmdPoll(b.flags()) })
	wantContains(t, "poll", out, "== FTN ==", "Toss complete", "QWK: no enabled networks")
	if _, err := os.Stat(filepath.Join(b.dataDir, "qwknet")); !os.IsNotExist(err) {
		t.Error("poll with no QWK networks touched data/qwknet")
	}

	out, _ = capture(t, func() { cmdPoll(append([]string{"--network", "TESTNET"}, b.flags()...)) })
	wantContains(t, "poll --network", out, "== FTN ==")
	if strings.Contains(out, "QWK") {
		t.Errorf("FTN-only network also polled QWK:\n%s", out)
	}

	out, _ = capture(t, func() { cmdPoll(append([]string{"--qwk-only"}, b.flags()...)) })
	if strings.Contains(out, "FTN") || !strings.Contains(out, "QWK: no enabled networks") {
		t.Errorf("--qwk-only output:\n%s", out)
	}
}

// poll rejects contradictory or unknown selections, and a second poll while
// one holds the lock.
func TestCmdPollRejects(t *testing.T) {
	b := newFTNBBS(t, "")
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"--ftn-only", "--qwk-only"}, "--ftn-only and --qwk-only cannot be used together"},
		{[]string{"--network", "nosuchnet"}, `network "nosuchnet" is in neither ftn.json nor qwknet.json`},
		{[]string{"--network", "testnet", "--qwk-only"}, `network "testnet" is not the kind selected`},
	} {
		code, _, errOut := runV3mail(t, b.root, append(append([]string{"poll"}, tc.args...), b.flags()...)...)
		if code != 1 || !strings.Contains(errOut, tc.want) {
			t.Errorf("poll %v: exit %d, stderr %q; want %q", tc.args, code, errOut, tc.want)
		}
	}

	lock, err := filelock.Acquire(filepath.Join(b.dataDir, "v3mail_poll"), 0)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Release()
	code, _, errOut := runV3mail(t, b.root, append([]string{"poll"}, b.flags()...)...)
	if code != 1 || !strings.Contains(errOut, "another poll is already running") {
		t.Errorf("locked poll: exit %d, stderr %q", code, errOut)
	}
}
