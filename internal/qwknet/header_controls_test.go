package qwknet

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/jam"
	"github.com/ViSiON-3/vision-3-bbs/internal/qwk"
)

// A hub's packet cannot put control characters in stored headers, and raw CP437
// in them survives.
func TestToss_CleansHeaderControls(t *testing.T) {
	e := newEnv(t)
	n := e.node(t)
	pkt := hubPacket(t, qwk.PacketMessage{Conference: 2001, Number: 500, From: "Fr\x1bcom", To: "All",
		Subject: "Caf\x82\x1b[2J", DateTime: time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC), Body: "x"})
	if err := os.WriteFile(filepath.Join(e.paths.InboundPath, "VERT.QWK"), pkt, 0o644); err != nil {
		t.Fatal(err)
	}
	if res := n.Toss(); res.Imported != 1 {
		t.Fatalf("toss: %+v", res)
	}
	base, err := e.msgMgr.GetBase(1)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = base.Close() }()
	msg, err := base.ReadMessage(1)
	if err != nil {
		t.Fatal(err)
	}
	if msg.From != "Fr com" || msg.Subject != "Caf\x82 [2J" {
		t.Errorf("from/subject = %q/%q", msg.From, msg.Subject)
	}
}

// A message stored with control characters does not pass them on to the hub.
func TestScan_CleansHeaderControls(t *testing.T) {
	e := newEnv(t)
	base, err := e.msgMgr.GetBase(1)
	if err != nil {
		t.Fatal(err)
	}
	msg := jam.NewMessage()
	msg.From, msg.To, msg.Subject = "Fr\x1bcom", "T\x07o", "Sub\x1b[2Jject\r\nTo: x"
	msg.Text = "body"
	area, ok := e.msgMgr.GetAreaByID(1)
	if !ok {
		t.Fatal("no area 1")
	}
	// WriteMessageExt, as a local post is written: WriteMessage marks a message
	// processed, and the scan would pass over it.
	if _, err := base.WriteMessageExt(msg, jam.DetermineMessageType(area.AreaType, area.EchoTag), area.EchoTag, "TestBBS"); err != nil {
		t.Fatal(err)
	}
	_ = base.Close()

	res := e.node(t).Scan()
	if res.Exported != 1 {
		t.Fatalf("scan: %+v", res)
	}
	msgs := readREP(t, res.REPPath)
	if len(msgs) != 1 {
		t.Fatalf("REP holds %d messages", len(msgs))
	}
	if msgs[0].From != "Fr com" || msgs[0].To != "T o" || msgs[0].Subject != "Sub [2Jject  To: x" {
		t.Errorf("from/to/subject = %q/%q/%q", msgs[0].From, msgs[0].To, msgs[0].Subject)
	}
}
