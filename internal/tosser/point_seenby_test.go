package tosser

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/ftn"
	"github.com/ViSiON-3/vision-3-bbs/internal/jam"
)

// SEEN-BY and PATH carry 2D net/node addresses. A point has no 2D address
// of its own: its net/node is its boss, so a point that stamps the lines
// writes the boss's address into them and the boss then rejects the packet
// as a loop. A point leaves both lines alone; a node stamps them.

// exportOneLocalMessage writes a local echomail to FSX_TEST as own, exports
// it, and returns the packed body from the outbound .PKT.
func exportOneLocalMessage(t *testing.T, env *testEnv, own string) string {
	t.Helper()

	base, err := env.msgMgr.GetBase(1)
	if err != nil {
		t.Fatalf("GetBase: %v", err)
	}
	msg := jam.NewMessage()
	msg.From = "Local User"
	msg.To = "All"
	msg.Subject = "SEEN-BY test"
	msg.Text = "Hello from " + own + "\r"
	msg.OrigAddr = own
	area, _ := env.msgMgr.GetAreaByTag("FSX_TEST")
	msgType := jam.DetermineMessageType(area.AreaType, area.EchoTag)
	if _, err = base.WriteMessageExt(msg, msgType, area.EchoTag, "TestBBS"); err != nil {
		t.Fatalf("WriteMessageExt: %v", err)
	}
	base.Close()

	env.netCfg.OwnAddress = own
	tosser := newTestTosser(t, env)
	if result := tosser.ScanAndExport(); result.MessagesExported != 1 {
		t.Fatalf("expected 1 exported, got %d (errors: %v)", result.MessagesExported, result.Errors)
	}

	var pktPath string
	for _, name := range dirNames(t, env.outboundDir) {
		if strings.EqualFold(filepath.Ext(name), ".pkt") {
			pktPath = filepath.Join(env.outboundDir, name)
		}
	}
	if pktPath == "" {
		t.Fatal("no .PKT written to outbound")
	}
	f, err := os.Open(pktPath)
	if err != nil {
		t.Fatalf("open pkt: %v", err)
	}
	defer f.Close()
	_, pktMsgs, err := ftn.ReadPacket(f)
	if err != nil {
		t.Fatalf("ReadPacket: %v", err)
	}
	if len(pktMsgs) != 1 {
		t.Fatalf("expected 1 message in packet, got %d", len(pktMsgs))
	}
	return pktMsgs[0].Body
}

func TestPointExportLeavesSeenByAndPathEmpty(t *testing.T) {
	env := setupTestEnv(t)
	body := exportOneLocalMessage(t, env, "21:4/158.1")

	if strings.Contains(body, "SEEN-BY:") {
		t.Errorf("point export must not carry SEEN-BY: %q", body)
	}
	if strings.Contains(body, "\x01PATH:") {
		t.Errorf("point export must not carry PATH: %q", body)
	}
}

func TestNodeExportStampsSeenByAndPath(t *testing.T) {
	env := setupTestEnv(t)
	body := exportOneLocalMessage(t, env, "21:4/200")

	if !strings.Contains(body, "SEEN-BY: 4/200\r") {
		t.Errorf("node export must carry its SEEN-BY: %q", body)
	}
	if !strings.Contains(body, "\x01PATH: 4/200\r") {
		t.Errorf("node export must carry its PATH: %q", body)
	}
}

// importOneMessage tosses a hub packet (SEEN-BY and PATH both "4/100") as
// own and returns the stored message.
func importOneMessage(t *testing.T, env *testEnv, own string) *jam.Message {
	t.Helper()

	env.netCfg.OwnAddress = own
	tosser := newTestTosser(t, env)
	pkt := makePktSimple(t, "FSX_TEST", "Sender", "All", "Subject", "Body\r", "21:4/100 0000BEEF")
	if err := os.WriteFile(filepath.Join(env.inboundDir, "hub00001.pkt"), pkt, 0644); err != nil {
		t.Fatal(err)
	}
	if result := tosser.ProcessInbound(); result.MessagesImported != 1 {
		t.Fatalf("expected 1 imported, got %d (errors: %v)", result.MessagesImported, result.Errors)
	}

	base, err := env.msgMgr.GetBase(1)
	if err != nil {
		t.Fatalf("GetBase: %v", err)
	}
	defer base.Close()
	msg, err := base.ReadMessage(1)
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}
	return msg
}

func TestPointImportLeavesSeenByAndPathAlone(t *testing.T) {
	env := setupTestEnv(t)
	msg := importOneMessage(t, env, "21:4/158.1")

	if msg.SeenBy != "4/100" {
		t.Errorf("SEEN-BY = %q, want %q", msg.SeenBy, "4/100")
	}
	if msg.Path != "4/100" {
		t.Errorf("PATH = %q, want %q", msg.Path, "4/100")
	}
}

func TestNodeImportStampsSeenByAndPath(t *testing.T) {
	env := setupTestEnv(t)
	msg := importOneMessage(t, env, "21:4/200")

	if msg.SeenBy != "4/100 200" {
		t.Errorf("SEEN-BY = %q, want %q", msg.SeenBy, "4/100 200")
	}
	if msg.Path != "4/100 200" {
		t.Errorf("PATH = %q, want %q", msg.Path, "4/100 200")
	}
}
