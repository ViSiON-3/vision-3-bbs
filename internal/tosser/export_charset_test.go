package tosser

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/ftn"
	"github.com/ViSiON-3/vision-3-bbs/internal/jam"
)

// exportOne writes msg to the area with the given tag as a local post,
// runs a scan, and returns the single message in the resulting packet.
func exportOne(t *testing.T, env *testEnv, cfg networkConfig, tag string, msg *jam.Message) *ftn.PackedMessage {
	t.Helper()
	area, ok := env.msgMgr.GetAreaByTag(tag)
	if !ok {
		t.Fatalf("no area %s", tag)
	}
	base, err := env.msgMgr.GetBase(area.ID)
	if err != nil {
		t.Fatalf("GetBase: %v", err)
	}
	msgType := jam.DetermineMessageType(area.AreaType, area.EchoTag)
	if _, err := base.WriteMessageExt(msg, msgType, area.EchoTag, "TestBBS"); err != nil {
		t.Fatalf("WriteMessageExt: %v", err)
	}
	_ = base.Close()

	tosser, err := New("testnet", cfg, env.globalCfg, env.dupeDB, env.msgMgr)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if result := tosser.ScanAndExport(); result.MessagesExported != 1 {
		t.Fatalf("expected 1 exported, got %d (errors: %v)", result.MessagesExported, result.Errors)
	}

	entries, _ := os.ReadDir(env.outboundDir)
	for _, e := range entries {
		if !strings.EqualFold(filepath.Ext(e.Name()), ".pkt") {
			continue
		}
		f, err := os.Open(filepath.Join(env.outboundDir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		_, msgs, err := ftn.ReadPacket(f)
		if err != nil || len(msgs) != 1 {
			t.Fatalf("ReadPacket: %d messages, %v", len(msgs), err)
		}
		return msgs[0]
	}
	t.Fatal("no .PKT written to outbound")
	return nil
}

func echoMsg(from, to, subject string, kludges ...string) *jam.Message {
	msg := jam.NewMessage()
	msg.From, msg.To, msg.Subject = from, to, subject
	msg.Text = "body\r"
	msg.OrigAddr = "21:4/158.1"
	msg.Kludges = kludges
	return msg
}

// Headers are stored as UTF-8; a packet without CHRS UTF-8 must carry them
// as CP437, the FTS default, so other systems decode them correctly (#681).
func TestExportEchomailHeadersEncodedCP437(t *testing.T) {
	env := setupTestEnv(t)
	pm := exportOne(t, env, env.netCfg, "FSX_TEST", echoMsg("José Müller", "Ärger", "Café ─ news"))

	if pm.From != "Jos\x82 M\x81ller" || pm.To != "\x8erger" || pm.Subject != "Caf\x82 \xc4 news" {
		t.Errorf("headers not CP437: from=%q to=%q subj=%q", pm.From, pm.To, pm.Subject)
	}
	// What a receiving tosser (ours) decodes is what was posted.
	if got := ftn.DecodeFTNString(pm.Subject, ""); got != "Café ─ news" {
		t.Errorf("decoded subject = %q", got)
	}
}

// A message that declares UTF-8 keeps UTF-8 headers.
func TestExportEchomailHeadersKeepDeclaredUTF8(t *testing.T) {
	env := setupTestEnv(t)
	pm := exportOne(t, env, env.netCfg, "FSX_TEST", echoMsg("José", "All", "Café ☃", "CHRS: UTF-8 4"))
	if pm.From != "José" || pm.Subject != "Café ☃" {
		t.Errorf("UTF-8 headers changed: from=%q subj=%q", pm.From, pm.Subject)
	}
}

// A subject of 71 two-byte characters is 142 bytes as UTF-8 and was cut
// mid-character by the 72-byte field limit; as CP437 it fits whole.
func TestExportLongMultibyteSubjectNotSplit(t *testing.T) {
	env := setupTestEnv(t)
	subject := strings.Repeat("é", 71)
	pm := exportOne(t, env, env.netCfg, "FSX_TEST", echoMsg("Sysop", "All", subject))
	if pm.Subject != strings.Repeat("\x82", 71) {
		t.Errorf("subject = %q (%d bytes)", pm.Subject, len(pm.Subject))
	}
}

func TestExportNetmailHeadersEncodedCP437(t *testing.T) {
	env, cfg := setupExtendedTestEnv(t)
	msg := echoMsg("Sysöp", "Sean Dénnis", "Ünïcode subject")
	msg.DestAddr = "21:4/158"
	pm := exportOne(t, env, cfg, "NETMAIL", msg)
	if pm.From != "Sys\x94p" || pm.To != "Sean D\x82nnis" || pm.Subject != "\x9an\x8bcode subject" {
		t.Errorf("headers not CP437: from=%q to=%q subj=%q", pm.From, pm.To, pm.Subject)
	}
}
