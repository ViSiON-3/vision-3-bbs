package tosser

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/ftn"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// netmailTo builds a packet from the hub holding one netmail to name at
// 21:4/<destNode>.<destPoint>.
func netmailTo(t *testing.T, name string, destNode, destPoint uint16, seq int) []byte {
	t.Helper()
	hdr := ftn.NewPacketHeader(21, 4, 158, 0, 21, 4, 158, 1, "")
	pm := &ftn.PackedMessage{
		MsgType: 2, OrigNet: 4, OrigNode: 100, DestNet: 4, DestNode: destNode,
		Attr: 0x0001, DateTime: "21 Feb 26  12:00:00",
		To: name, From: "Remote User", Subject: "Hello",
		Body: ftn.FormatPackedMessageBody(&ftn.ParsedBody{
			Text:    "Body\r",
			Kludges: []string{fmt.Sprintf("MSGID: 21:4/100 %08x", seq), fmt.Sprintf("TOPT %d", destPoint)},
		}),
	}
	var buf bytes.Buffer
	if err := ftn.WritePacket(&buf, hdr, []*ftn.PackedMessage{pm}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// TestTossNetmailAddressedByHandle pins the #467 regression for FTN netmail:
// netmail for this system (21:4/158.1) to a local user's real name or to
// "Sysop" is stored addressed to that user's handle; an unknown name is kept
// as received, as is everything when no resolver is set, and netmail for
// another node passing through is never readdressed.
func TestTossNetmailAddressedByHandle(t *testing.T) {
	users := user.NewUserMgrForTest(
		&user.User{ID: 1, Handle: "Hermit", RealName: "Sam Sysop"},
		&user.User{ID: 2, Handle: "Bob", RealName: "Bob Builder"},
	)
	for _, tc := range []struct {
		name     string
		resolver user.RecipientResolver
		to       string
		node, pt uint16
		wantTo   string
	}{
		{"real name", users, "Bob Builder", 158, 1, "Bob"},
		{"sysop", users, "Sysop", 158, 1, "Hermit"},
		{"handle case", users, "BOB", 158, 1, "Bob"},
		{"unknown", users, "Nobody Known", 158, 1, "Nobody Known"},
		{"no resolver", nil, "Bob Builder", 158, 1, "Bob Builder"},
		{"typed-nil manager", (*user.UserMgr)(nil), "Bob Builder", 158, 1, "Bob Builder"},
		{"other node", users, "Bob Builder", 200, 0, "Bob Builder"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env, extCfg := setupExtendedTestEnv(t)
			tss, err := New("testnet", extCfg, env.globalCfg, env.dupeDB, env.msgMgr)
			if err != nil {
				t.Fatal(err)
			}
			tss.SetRecipientResolver(tc.resolver)
			if err := os.WriteFile(filepath.Join(env.inboundDir, "n.pkt"), netmailTo(t, tc.to, tc.node, tc.pt, 1), 0o644); err != nil {
				t.Fatal(err)
			}
			if res := tss.ProcessInbound(); res.MessagesImported != 1 {
				t.Fatalf("imported %d, want 1 (errors: %v)", res.MessagesImported, res.Errors)
			}
			base, err := env.msgMgr.GetBase(2) // NETMAIL
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = base.Close() }()
			msg, err := base.ReadMessage(1)
			if err != nil {
				t.Fatal(err)
			}
			if msg.To != tc.wantTo {
				t.Errorf("stored To %q, want %q", msg.To, tc.wantTo)
			}
			if !msg.IsPrivate() {
				t.Error("netmail not stored private")
			}
		})
	}
}

// TestTossEchomailNotReaddressed pins that echomail, which is public, keeps
// its To even when it names a local user by real name.
func TestTossEchomailNotReaddressed(t *testing.T) {
	env := setupTestEnv(t)
	tss, err := New("testnet", env.netCfg, env.globalCfg, env.dupeDB, env.msgMgr)
	if err != nil {
		t.Fatal(err)
	}
	tss.SetRecipientResolver(user.NewUserMgrForTest(&user.User{ID: 2, Handle: "Bob", RealName: "Bob Builder"}))
	pkt := makePktSimple(t, "FSX_TEST", "Remote User", "Bob Builder", "Hi", "Body\r", "21:4/100 00000001")
	if err := os.WriteFile(filepath.Join(env.inboundDir, "e.pkt"), pkt, 0o644); err != nil {
		t.Fatal(err)
	}
	if res := tss.ProcessInbound(); res.MessagesImported != 1 {
		t.Fatalf("imported %d, want 1 (errors: %v)", res.MessagesImported, res.Errors)
	}
	base, err := env.msgMgr.GetBase(1)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = base.Close() }()
	msg, err := base.ReadMessage(1)
	if err != nil {
		t.Fatal(err)
	}
	if msg.To != "Bob Builder" {
		t.Errorf("echomail To readdressed to %q", msg.To)
	}
}
