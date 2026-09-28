package qwknet

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/qwk"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// TestToss_PrivateMailAddressedByHandle pins the #467 regression for QWK
// network imports: private hub mail to a local user's real name, uppercased
// handle or "Sysop" is stored addressed to that user's handle, the only name
// a reader is matched by. Unknown names and public mail keep their To.
func TestToss_PrivateMailAddressedByHandle(t *testing.T) {
	e := newEnv(t)
	n := e.node(t)
	n.SetRecipientResolver(user.NewUserMgrForTest(
		&user.User{ID: 1, Handle: "Hermit", RealName: "Sam Sysop"},
		&user.User{ID: 2, Handle: "Bob", RealName: "Bob Builder"},
	))
	when := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	pkt := hubPacket(t,
		qwk.PacketMessage{Conference: 2001, Number: 1, From: "Remote", To: "Bob Builder", Subject: "a", DateTime: when, Body: "x", Private: true},
		qwk.PacketMessage{Conference: 2001, Number: 2, From: "Remote", To: "BOB", Subject: "b", DateTime: when, Body: "x", Private: true},
		qwk.PacketMessage{Conference: 2001, Number: 3, From: "Remote", To: "SYSOP", Subject: "c", DateTime: when, Body: "x", Private: true},
		qwk.PacketMessage{Conference: 2001, Number: 4, From: "Remote", To: "Stranger", Subject: "d", DateTime: when, Body: "x", Private: true},
		qwk.PacketMessage{Conference: 2001, Number: 5, From: "Remote", To: "Bob Builder", Subject: "e", DateTime: when, Body: "x"},
	)
	if err := os.WriteFile(filepath.Join(e.paths.InboundPath, "VERT.QWK"), pkt, 0o644); err != nil {
		t.Fatal(err)
	}
	if res := n.Toss(); len(res.Errors) != 0 || res.Imported != 5 {
		t.Fatalf("toss: %+v", res)
	}
	for i, want := range []string{"Bob", "Bob", "Hermit", "Stranger", "Bob Builder"} {
		dm, err := e.msgMgr.GetMessage(1, i+1)
		if err != nil {
			t.Fatal(err)
		}
		if dm.To != want {
			t.Errorf("message %d (private=%v) stored To %q, want %q", i+1, dm.IsPrivate, dm.To, want)
		}
	}
}
