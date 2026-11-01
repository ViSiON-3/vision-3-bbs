package tosser

import (
	"testing"
)

// Header fields from another system are stored without control characters,
// whichever encoding the packet declares.
func TestTossCleansHeaderControls(t *testing.T) {
	env := setupTestEnv(t)
	tosser, err := New("testnet", env.netCfg, env.globalCfg, env.dupeDB, env.msgMgr)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	for i, pkt := range [][]byte{
		makePktKludges(t, "FSX_TEST", "Fr\x1bcom", "T\x07o", "Sub\x1b]0;t\x07ject", "Body\r",
			[]string{"MSGID: 21:4/100 00000001"}),
		makePktKludges(t, "FSX_TEST", "Fr\u009bom", "T\u009bo", "Sub\u009bject", "Body\r",
			[]string{"MSGID: 21:4/100 00000002", "CHRS: UTF-8 4"}),
		makePktKludges(t, "FSX_TEST", "Fr\xffom", "To", "\xff\u009b2J\x1b[2J", "Body\r",
			[]string{"MSGID: 21:4/100 00000003", "CHRS: UTF-8 4"}),
	} {
		if r := tossPkt(t, env, tosser, []string{"a.pkt", "b.pkt", "c.pkt"}[i], pkt); r.MessagesImported != 1 {
			t.Fatalf("packet %d: imported %d (errors: %v)", i, r.MessagesImported, r.Errors)
		}
	}
	base, err := env.msgMgr.GetBase(1)
	if err != nil {
		t.Fatalf("GetBase: %v", err)
	}
	defer func() { _ = base.Close() }()
	for num, want := range map[int][3]string{
		1: {"Fr com", "T o", "Sub ]0;t ject"},
		2: {"Fr om", "T o", "Sub ject"},
		3: {"Fr\xffom", "To", "\xff 2J [2J"},
	} {
		msg, err := base.ReadMessage(num)
		if err != nil {
			t.Fatalf("ReadMessage(%d): %v", num, err)
		}
		if got := [3]string{msg.From, msg.To, msg.Subject}; got != want {
			t.Errorf("msg %d: from/to/subject = %q, want %q", num, got, want)
		}
	}
}

// A message stored with control characters does not pass them on to the network.
func TestExportCleansHeaderControls(t *testing.T) {
	env := setupTestEnv(t)
	pm := exportOne(t, env, env.netCfg, "FSX_TEST", echoMsg("Fr\x1bcom", "T\x07o", "Sub\x1b[2Jject"))
	if pm.From != "Fr com" || pm.To != "T o" || pm.Subject != "Sub [2Jject" {
		t.Errorf("from/to/subject = %q/%q/%q", pm.From, pm.To, pm.Subject)
	}
}
