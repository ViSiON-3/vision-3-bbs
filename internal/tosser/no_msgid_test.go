package tosser

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// tossPkt writes pktData into the inbound dir under name and tosses it.
func tossPkt(t *testing.T, env *testEnv, tosser *Tosser, name string, pktData []byte) TossResult {
	t.Helper()
	if err := os.WriteFile(filepath.Join(env.inboundDir, name), pktData, 0644); err != nil {
		t.Fatalf("write packet: %v", err)
	}
	return tosser.ProcessInbound()
}

// TestTossNoMsgIDDuplicateDetection covers TriToss echomail, which carries no
// MSGID: a hub forwarding it twice (each copy with its own TID/DBID) must not
// import it twice, while a different message from the same author still gets
// in.
func TestTossNoMsgIDDuplicateDetection(t *testing.T) {
	env := setupTestEnv(t)
	tosser, err := New("testnet", env.netCfg, env.globalCfg, env.dupeDB, env.msgMgr)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	body := "Testing external editors with TriBBS.\r\r--- TriToss\r * Origin: Camelot BBS (21:4/100.0)\r"
	first := makePktKludges(t, "FSX_TEST", "Merlin", "All", "Testing editors", body,
		[]string{"NOTE: DCTEdit v0.04 [0]", "TID: clrghouz c59d02ba", "DBID: 1061332"})
	second := makePktKludges(t, "FSX_TEST", "Merlin", "All", "Testing editors", body,
		[]string{"NOTE: DCTEdit v0.04 [0]", "TID: clrghouz c59d02ba", "DBID: 1061333"})
	other := makePktKludges(t, "FSX_TEST", "Merlin", "All", "Testing editors", "A different message.\r",
		[]string{"NOTE: DCTEdit v0.04 [0]"})

	if r := tossPkt(t, env, tosser, "first.pkt", first); r.MessagesImported != 1 {
		t.Fatalf("first copy: imported %d, want 1 (errors: %v)", r.MessagesImported, r.Errors)
	}
	if r := tossPkt(t, env, tosser, "second.pkt", second); r.DupesSkipped != 1 || r.MessagesImported != 0 {
		t.Errorf("second copy: imported %d, dupes %d; want 0 imported, 1 dupe", r.MessagesImported, r.DupesSkipped)
	}
	if r := tossPkt(t, env, tosser, "other.pkt", other); r.MessagesImported != 1 {
		t.Errorf("different message: imported %d, want 1 (dupes %d)", r.MessagesImported, r.DupesSkipped)
	}
}

// TestTossKeepsReceivedKludges verifies an imported message is stored with
// the kludges it arrived with: no MSGID invented for one that had none, and
// none of our PID/TID added to either.
func TestTossKeepsReceivedKludges(t *testing.T) {
	env := setupTestEnv(t)
	tosser, err := New("testnet", env.netCfg, env.globalCfg, env.dupeDB, env.msgMgr)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	noID := makePktKludges(t, "FSX_TEST", "Merlin", "All", "No MSGID", "Body one\r",
		[]string{"NOTE: DCTEdit v0.04 [0]"})
	withID := makePktKludges(t, "FSX_TEST", "Joakim", "All", "Has MSGID", "Body two\r",
		[]string{"MSGID: 21:4/100 4b58405b", "PID: Mystic 1.12", "TID: Mystic BBS 1.12 A48"})
	for i, pkt := range [][]byte{noID, withID} {
		if r := tossPkt(t, env, tosser, []string{"a.pkt", "b.pkt"}[i], pkt); r.MessagesImported != 1 {
			t.Fatalf("packet %d: imported %d, want 1 (errors: %v)", i, r.MessagesImported, r.Errors)
		}
	}

	base, err := env.msgMgr.GetBase(1)
	if err != nil {
		t.Fatalf("GetBase: %v", err)
	}
	defer base.Close()

	tests := []struct {
		num     int
		msgID   string
		kludges []string
	}{
		{1, "", []string{"NOTE: DCTEdit v0.04 [0]"}},
		{2, "21:4/100 4b58405b", []string{"PID: Mystic 1.12", "TID: Mystic BBS 1.12 A48"}},
	}
	for _, tt := range tests {
		msg, err := base.ReadMessage(tt.num)
		if err != nil {
			t.Fatalf("ReadMessage(%d): %v", tt.num, err)
		}
		if msg.MsgID != tt.msgID {
			t.Errorf("msg %d: MSGID = %q, want %q", tt.num, msg.MsgID, tt.msgID)
		}
		if msg.PID != "" {
			t.Errorf("msg %d: PID subfield = %q, want none", tt.num, msg.PID)
		}
		if strings.Join(msg.Kludges, "|") != strings.Join(tt.kludges, "|") {
			t.Errorf("msg %d: kludges = %q, want %q", tt.num, msg.Kludges, tt.kludges)
		}
	}
}
