package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/message"
	"github.com/ViSiON-3/vision-3-bbs/internal/qwk"
)

// qwkJSON configures an enabled "dovenet" (hub VERT) and a disabled "fidoqwk"
// (hub FIDO). The hosts are never dialled: every test that reaches a
// connection does so with a cancelled context.
const qwkJSON = `{
  "inboundPath": "data/qwknet/in",
  "outboundPath": "data/qwknet/out",
  "tempPath": "data/qwknet/temp",
  "dupeDbPath": "data/qwknet/dupes.json",
  "networks": {
    "dovenet": {"enabled": true, "name": "DOVE-Net", "hubId": "VERT", "host": "127.0.0.1", "password": "pw", "tagline": "Test Node"},
    "fidoqwk": {"enabled": false, "name": "FidoQWK", "hubId": "FIDO", "host": "127.0.0.1", "password": "pw"}
  }
}`

// newQWKBBS builds a BBS carrying DOVE-Net conference 2001 as area 1.
func newQWKBBS(t *testing.T) *bbs {
	t.Helper()
	b := newBBS(t)
	b.writeConfig(t, "qwknet.json", qwkJSON)
	b.writeAreas(t,
		map[string]any{"id": 1, "tag": "DOVE_GEN", "name": "DOVE-Net General", "base_path": "msgbases/dove_gen",
			"area_type": "qwknet", "network": "dovenet", "qwk_conference": 2001},
		map[string]any{"id": 2, "tag": "GENERAL", "name": "General", "base_path": "msgbases/general", "area_type": "local"},
	)
	return b
}

// qwkInbound returns the path of name in the QWK inbound directory, creating
// the directory.
func (b *bbs) qwkInbound(t *testing.T, name string) string {
	t.Helper()
	dir := filepath.Join(b.dataDir, "qwknet", "in")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(dir, name)
}

// hubPacket builds the QWK packet hub VERT would send.
func hubPacket(t *testing.T, msgs ...qwk.PacketMessage) []byte {
	t.Helper()
	pw := qwk.NewPacketWriter("VERT", "Vertrauen", "Digital Man")
	pw.AddConference(2001, "DOVE-Net General")
	pw.AddConference(2006, "Programming")
	for _, m := range msgs {
		pw.AddMessage(m)
	}
	var buf bytes.Buffer
	if err := pw.WritePacket(&buf); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// addPost writes a local post into area areaID of the BBS.
func addPost(t *testing.T, b *bbs, areaID int, subject string) {
	t.Helper()
	mm, err := message.NewMessageManager(b.dataDir, b.configDir, "Test BBS", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = mm.Close() }()
	if _, err := mm.AddMessage(areaID, "Sysop", "All", subject, "body", ""); err != nil {
		t.Fatal(err)
	}
}

// qwk-scan packs new posts from the network's areas into the hub's REP and
// reports its path, counting what an earlier scan left pending; local areas are not exported.
func TestCmdQWKScan(t *testing.T) {
	b := newQWKBBS(t)
	addPost(t, b, 1, "To DOVE-Net")
	addPost(t, b, 2, "Local only")

	out, errOut := capture(t, func() { cmdQWKScan(b.flags()) })
	rep := filepath.Join(b.dataDir, "qwknet", "out", "VERT.REP")
	wantContains(t, "qwk-scan", out, "dovenet: packed 1 new message(s), 0 pending -> "+rep)
	if strings.Contains(out, "fidoqwk") {
		t.Errorf("disabled network was scanned:\n%s", out)
	}
	if errOut != "" {
		t.Errorf("stderr = %q", errOut)
	}
	if _, err := os.Stat(rep); err != nil {
		t.Errorf("REP not written: %v", err)
	}

	// A later scan appends to the REP still waiting for upload.
	addPost(t, b, 1, "Second")
	out, _ = capture(t, func() { cmdQWKScan(b.flags()) })
	wantContains(t, "second qwk-scan", out, "dovenet: packed 1 new message(s), 1 pending -> "+rep)

	// Named explicitly, a disabled network is still scanned.
	out, _ = capture(t, func() { cmdQWKScan(append([]string{"--network", "FidoQWK"}, b.flags()...)) })
	wantContains(t, "qwk-scan --network", out, "fidoqwk: packed 0 new message(s), 0 pending")
}

// qwk-toss imports a waiting hub packet into the mapped area and counts
// messages for conferences no area carries.
func TestCmdQWKToss(t *testing.T) {
	b := newQWKBBS(t)
	when := time.Date(2026, 2, 1, 12, 0, 0, 0, time.UTC)
	writeFile(t, b.qwkInbound(t, "VERT.QWK"), hubPacket(t,
		qwk.PacketMessage{Conference: 2001, Number: 10, From: "Digital Man", To: "All", Subject: "Welcome", DateTime: when, Body: "Hi"},
		qwk.PacketMessage{Conference: 2006, Number: 11, From: "Coder", To: "All", Subject: "Unmapped", DateTime: when, Body: "x"},
	))

	out, errOut := capture(t, func() { cmdQWKToss(append([]string{"--network", "dovenet"}, b.flags()...)) })
	wantContains(t, "qwk-toss", out, "dovenet: 1 packet(s), imported 1, dupes 0, unmapped 1, skipped 0")
	if errOut != "" {
		t.Errorf("stderr = %q", errOut)
	}
	msg, err := openBase(t, filepath.Join(b.dataDir, "msgbases", "dove_gen")).ReadMessage(1)
	if err != nil || msg.Subject != "Welcome" || msg.From != "Digital Man" {
		t.Errorf("tossed message = %+v, %v", msg, err)
	}
}

// qwk-conferences lists the conferences in a packet already waiting, without
// connecting to the hub.
func TestCmdQWKConferences(t *testing.T) {
	b := newQWKBBS(t)
	writeFile(t, b.qwkInbound(t, "VERT.QWK"), hubPacket(t))
	out, _ := capture(t, func() { cmdQWKConferences(append([]string{"--network", "dovenet"}, b.flags()...)) })
	wantContains(t, "qwk-conferences", out, "Conferences on hub VERT (dovenet):", "2001  DOVE-Net General", "2006  Programming")
}

// qwk-poll with no enabled network says there is nothing to do.
func TestCmdQWKPollNothingEnabled(t *testing.T) {
	b := newQWKBBS(t)
	b.writeConfig(t, "qwknet.json", strings.Replace(qwkJSON, `"enabled": true`, `"enabled": false`, 1))
	out, _ := capture(t, func() { cmdQWKPoll(b.flags()) })
	wantContains(t, "qwk-poll", out, "No enabled QWK networks in qwknet.json; nothing to poll.")
}

// pollQWK packs local posts before it connects, and reports a connection that
// could not be made (here, because the poll was already cancelled).
func TestPollQWKReportsConnectFailure(t *testing.T) {
	b := newQWKBBS(t)
	addPost(t, b, 1, "Outbound")
	nodes, cleanup, err := loadQWKNetNodes(b.configDir, b.dataDir, "")
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if len(nodes) != 1 || nodes[0].Key != "dovenet" {
		t.Fatalf("nodes = %v, want only the enabled dovenet", nodes)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var failed bool
	out, errOut := capture(t, func() { failed = pollQWK(ctx, nodes) })
	if !failed {
		t.Error("poll without a connection reported success")
	}
	wantContains(t, "stdout", out, "Polling dovenet (hub VERT as ", "packed 1 new (0 pending), uploaded=false, downloaded=false")
	wantContains(t, "stderr", errOut, "[dovenet] ERROR: connect to hub 127.0.0.1:21")
}

// loadQWKNetNodes rejects an unreadable qwknet.json, two networks sharing a
// hub, a network that fails validation, and an unknown --network key.
func TestLoadQWKNetNodesErrors(t *testing.T) {
	b := newQWKBBS(t)
	for _, tc := range []struct {
		name, body, only, want string
	}{
		{"bad JSON", "{broken", "", ""},
		{"shared hub", strings.Replace(qwkJSON, `"hubId": "FIDO"`, `"hubId": "VERT"`, 1), "", "both use hub ID VERT"},
		{"no host", strings.Replace(qwkJSON, `"host": "127.0.0.1", "password": "pw", "tagline"`, `"password": "pw", "tagline"`, 1), "", "dovenet"},
		{"unknown", qwkJSON, "nosuch", `QWK network "nosuch" is not configured`},
	} {
		b.writeConfig(t, "qwknet.json", tc.body)
		_, _, err := loadQWKNetNodes(b.configDir, b.dataDir, tc.only)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want %q", tc.name, err, tc.want)
		}
	}
}

// hasEnabledQWK is true when a network is named or any is enabled, and when
// the config cannot be read (so the loader reports why).
func TestHasEnabledQWK(t *testing.T) {
	b := newQWKBBS(t)
	if !hasEnabledQWK(b.configDir, "") {
		t.Error("enabled dovenet not seen")
	}
	b.writeConfig(t, "qwknet.json", strings.Replace(qwkJSON, `"enabled": true`, `"enabled": false`, 1))
	if hasEnabledQWK(b.configDir, "") {
		t.Error("all-disabled config reported an enabled network")
	}
	if !hasEnabledQWK(b.configDir, "fidoqwk") {
		t.Error("a named network should always count")
	}
	b.writeConfig(t, "qwknet.json", "{broken")
	if !hasEnabledQWK(b.configDir, "") {
		t.Error("unreadable config should defer to the loader")
	}
}

// The qwk-* commands exit 1 on a bad config, a missing --network for
// qwk-conferences, and a packet that cannot be tossed.
func TestQWKCommandsExitOnError(t *testing.T) {
	b := newQWKBBS(t)
	code, _, errOut := runV3mail(t, b.root, append([]string{"qwk-conferences"}, b.flags()...)...)
	if code != 1 || !strings.Contains(errOut, "qwk-conferences: --network is required") {
		t.Errorf("qwk-conferences without --network: exit %d, stderr %q", code, errOut)
	}
	code, _, errOut = runV3mail(t, b.root, append([]string{"qwk-conferences", "--network", "nosuch"}, b.flags()...)...)
	if code != 1 || !strings.Contains(errOut, `QWK network "nosuch" is not configured`) {
		t.Errorf("qwk-conferences unknown: exit %d, stderr %q", code, errOut)
	}

	writeFile(t, b.qwkInbound(t, "VERT.QWK"), []byte("not a zip"))
	code, out, errOut := runV3mail(t, b.root, append([]string{"qwk-toss"}, b.flags()...)...)
	if code != 1 || !strings.Contains(errOut, "[dovenet] ERROR:") || !strings.Contains(out, "dovenet: ") {
		t.Errorf("qwk-toss of a corrupt packet: exit %d, stdout %q, stderr %q", code, out, errOut)
	}

	b.writeConfig(t, "qwknet.json", "{broken")
	for _, c := range []string{"qwk-scan", "qwk-toss", "qwk-poll"} {
		code, _, errOut := runV3mail(t, b.root, append([]string{c}, b.flags()...)...)
		if code != 1 || !strings.HasPrefix(errOut, c+": ") {
			t.Errorf("%s with broken qwknet.json: exit %d, stderr %q", c, code, errOut)
		}
	}
}
