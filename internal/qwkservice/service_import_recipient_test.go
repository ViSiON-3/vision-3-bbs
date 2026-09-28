package qwkservice

import (
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/message"
	"github.com/ViSiON-3/vision-3-bbs/internal/qwk"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// TestImportREP_PrivateReplyAddressedByHandle pins the #467 regression for
// REP uploads: a private reply whose To is the recipient's real name, the
// handle uppercased (as QWK readers write it), or "Sysop" is stored addressed
// to the recipient's handle, which is all a reader is matched by. A name no
// account has is kept as written, and public replies are never touched.
func TestImportREP_PrivateReplyAddressedByHandle(t *testing.T) {
	store := newFakeStore()
	store.addArea(&message.MessageArea{ID: 1, Tag: "GENERAL", Name: "General"})
	store.addArea(&message.MessageArea{ID: 3, Tag: "PRIVMAIL", Name: "Private Mail"})
	rep := makeREP(t, "VISION3", []qwk.PacketMessage{
		{Conference: 0, Number: 1, To: "Bob Builder", Subject: "a", DateTime: time.Now(), Body: "x"},
		{Conference: 0, Number: 2, To: "DARK KNIGHT", Subject: "b", DateTime: time.Now(), Body: "x"},
		{Conference: 0, Number: 3, To: "SYSOP", Subject: "c", DateTime: time.Now(), Body: "x"},
		{Conference: 0, Number: 4, To: "STRANGER", Subject: "d", DateTime: time.Now(), Body: "x"},
		{Conference: 1, Number: 5, To: "BOB BUILDER", Subject: "e", DateTime: time.Now(), Body: "x"},
	})

	svc := newTestService(t, store)
	svc.SetRecipientResolver(user.NewUserMgrForTest(
		&user.User{ID: 1, Handle: "Hermit", RealName: "Sam Sysop"},
		&user.User{ID: 2, Handle: "Bob", RealName: "Bob Builder"},
		&user.User{ID: 3, Handle: "Dark Knight", RealName: "Dan Knight"},
	))
	if _, err := svc.ImportREP(rep, ImportOptions{Handle: "tester"}); err != nil {
		t.Fatal(err)
	}
	if len(store.privPosted) != 4 {
		t.Fatalf("want 4 private posts, got %d", len(store.privPosted))
	}
	for i, want := range []string{"Bob", "Dark Knight", "Hermit", "STRANGER"} {
		if got := store.privPosted[i].to; got != want {
			t.Errorf("private reply %d stored To %q, want %q", i+1, got, want)
		}
	}
	if len(store.posted) != 1 || store.posted[0].to != "BOB BUILDER" {
		t.Errorf("public reply readdressed: %+v", store.posted)
	}
}

// TestImportREP_NoResolverKeepsTo pins the nil-resolver fallback: without the
// accounts, a private reply's To is stored as the reader wrote it.
func TestImportREP_NoResolverKeepsTo(t *testing.T) {
	store := newFakeStore()
	store.addArea(&message.MessageArea{ID: 3, Tag: "PRIVMAIL", Name: "Private Mail"})
	rep := makeREP(t, "VISION3", []qwk.PacketMessage{
		{Conference: 0, Number: 1, To: "Bob Builder", Subject: "a", DateTime: time.Now(), Body: "x"},
	})
	svc := newTestService(t, store)
	if _, err := svc.ImportREP(rep, ImportOptions{Handle: "tester"}); err != nil {
		t.Fatal(err)
	}
	if len(store.privPosted) != 1 || store.privPosted[0].to != "Bob Builder" {
		t.Errorf("want To kept as written, got %+v", store.privPosted)
	}
}
