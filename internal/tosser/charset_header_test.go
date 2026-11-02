package tosser

import (
	"testing"

	"golang.org/x/text/encoding/charmap"
)

// #744: a packet labelled "CHRS: CP866 2" carries its header fields in CP866.
// They are stored decoded as the Cyrillic they are, not as CP437 mojibake, and
// a message carrying that label goes back out in CP866.
func TestTossDecodesHeadersByCHRS(t *testing.T) {
	enc := func(s string) string {
		out, err := charmap.CodePage866.NewEncoder().String(s)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	env := setupTestEnv(t)
	tosser, err := New("testnet", env.netCfg, env.globalCfg, env.dupeDB, env.msgMgr)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	pkt := makePktKludges(t, "FSX_TEST", enc("Иван Петров"), enc("Всем"), enc("Привет"), "Body\r",
		[]string{"MSGID: 21:4/100 00000001", "CHRS: CP866 2"})
	if r := tossPkt(t, env, tosser, "a.pkt", pkt); r.MessagesImported != 1 {
		t.Fatalf("imported %d (errors: %v)", r.MessagesImported, r.Errors)
	}
	base, err := env.msgMgr.GetBase(1)
	if err != nil {
		t.Fatalf("GetBase: %v", err)
	}
	defer func() { _ = base.Close() }()
	msg, err := base.ReadMessage(1)
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}
	if got, want := [3]string{msg.From, msg.To, msg.Subject}, [3]string{"Иван Петров", "Всем", "Привет"}; got != want {
		t.Errorf("stored from/to/subject = %q, want %q", got, want)
	}

	pm := exportOne(t, env, env.netCfg, "FSX_TEST", echoMsg("Иван Петров", "Всем", "Привет", "CHRS: CP866 2"))
	if pm.From != enc("Иван Петров") || pm.To != enc("Всем") || pm.Subject != enc("Привет") {
		t.Errorf("exported headers not CP866: from=%q to=%q subj=%q", pm.From, pm.To, pm.Subject)
	}
}
