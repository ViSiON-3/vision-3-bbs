package qwkservice

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/message"
	"github.com/ViSiON-3/vision-3-bbs/internal/qwk"
)

func TestImportREP_PostsReplies(t *testing.T) {
	store := newFakeStore()
	store.addArea(&message.MessageArea{ID: 1, Tag: "GENERAL", Name: "General"})

	rep := makeREP(t, "VISION3", []qwk.PacketMessage{
		{Conference: 1, Number: 1, From: "tester", To: "SysOp", Subject: "Hi", DateTime: time.Now(), Body: "Reply body"},
	})

	svc := newTestService(t, store)
	res, err := svc.ImportREP(rep, ImportOptions{Handle: "tester"})
	if err != nil {
		t.Fatalf("ImportREP: %v", err)
	}
	if res.Posted != 1 || res.Skipped != 0 {
		t.Errorf("want posted=1 skipped=0, got %+v", res)
	}
	if len(store.posted) != 1 {
		t.Fatalf("want 1 posted message, got %d", len(store.posted))
	}
	if store.posted[0].from != "tester" || store.posted[0].to != "SysOp" {
		t.Errorf("posted from/to wrong: %+v", store.posted[0])
	}
}

func TestImportREP_AppendsSignature(t *testing.T) {
	store := newFakeStore()
	store.addArea(&message.MessageArea{ID: 1, Tag: "GENERAL", Name: "General"})
	rep := makeREP(t, "VISION3", []qwk.PacketMessage{
		{Conference: 1, Number: 1, To: "SysOp", Subject: "Hi", DateTime: time.Now(), Body: "Body"},
	})

	svc := newTestService(t, store)
	_, err := svc.ImportREP(rep, ImportOptions{Handle: "tester", Signature: "-- sig"})
	if err != nil {
		t.Fatalf("ImportREP: %v", err)
	}
	if got := store.posted[0].body; got != "Body\n\n-- sig" {
		t.Errorf("signature not appended: %q", got)
	}
}

func TestImportREP_SkipsUnknownConference(t *testing.T) {
	store := newFakeStore()
	store.addArea(&message.MessageArea{ID: 1, Tag: "GENERAL", Name: "General"})
	rep := makeREP(t, "VISION3", []qwk.PacketMessage{
		{Conference: 99, Number: 1, To: "SysOp", Subject: "Hi", DateTime: time.Now(), Body: "Body"},
	})

	svc := newTestService(t, store)
	res, _ := svc.ImportREP(rep, ImportOptions{Handle: "tester"})
	if res.Posted != 0 || res.Skipped != 1 {
		t.Errorf("want posted=0 skipped=1, got %+v", res)
	}
}

func TestImportREP_AuthorizeGate(t *testing.T) {
	store := newFakeStore()
	store.addArea(&message.MessageArea{ID: 1, Tag: "READONLY", Name: "Read Only"})
	rep := makeREP(t, "VISION3", []qwk.PacketMessage{
		{Conference: 1, Number: 1, To: "SysOp", Subject: "Hi", DateTime: time.Now(), Body: "Body"},
	})

	svc := newTestService(t, store)
	res, _ := svc.ImportREP(rep, ImportOptions{
		Handle:    "tester",
		Authorize: func(*message.MessageArea) bool { return false },
	})
	if res.Posted != 0 || res.Skipped != 1 {
		t.Errorf("want posted=0 skipped=1 (denied), got %+v", res)
	}
	if len(store.posted) != 0 {
		t.Error("nothing should be posted when Authorize denies")
	}
}

func TestImportREP_NotifyCalled(t *testing.T) {
	store := newFakeStore()
	store.addArea(&message.MessageArea{ID: 1, Tag: "GENERAL", Name: "General"})
	rep := makeREP(t, "VISION3", []qwk.PacketMessage{
		{Conference: 1, Number: 1, To: "SysOp", Subject: "Hi", DateTime: time.Now(), Body: "Body"},
	})

	var notified []string
	svc := newTestService(t, store)
	_, err := svc.ImportREP(rep, ImportOptions{
		Handle: "tester",
		Notify: func(a *message.MessageArea) { notified = append(notified, a.Name) },
	})
	if err != nil {
		t.Fatalf("ImportREP: %v", err)
	}
	if len(notified) != 1 || notified[0] != "General" {
		t.Errorf("Notify not called as expected: %v", notified)
	}
}

func TestImportREP_PostErrorCounted(t *testing.T) {
	store := newFakeStore()
	store.addArea(&message.MessageArea{ID: 1, Tag: "GENERAL", Name: "General"})
	store.addErr[1] = fmt.Errorf("disk full")
	rep := makeREP(t, "VISION3", []qwk.PacketMessage{
		{Conference: 1, Number: 1, To: "SysOp", Subject: "Hi", DateTime: time.Now(), Body: "Body"},
	})

	svc := newTestService(t, store)
	res, err := svc.ImportREP(rep, ImportOptions{Handle: "tester"})
	if err != nil {
		t.Fatalf("ImportREP should not fail wholesale on a single post error: %v", err)
	}
	if res.Posted != 0 || res.Skipped != 1 {
		t.Errorf("want posted=0 skipped=1, got %+v", res)
	}
}

func TestImportREP_BadPacketErrors(t *testing.T) {
	store := newFakeStore()
	svc := newTestService(t, store)
	_, err := svc.ImportREP([]byte("not a zip"), ImportOptions{Handle: "tester"})
	if err == nil {
		t.Fatal("expected error for a non-zip REP packet")
	}
}

func TestImportREP_PrivateConferenceRoutesToPrivate(t *testing.T) {
	store := newFakeStore()
	store.addArea(&message.MessageArea{ID: 3, Tag: "PRIVMAIL", Name: "Private Mail"})

	// Conference 0 is the private-mail conference after Sync.
	rep := makeREP(t, "VISION3", []qwk.PacketMessage{
		{Conference: 0, Number: 1, From: "tester", To: "friend", Subject: "Hey", DateTime: time.Now(), Body: "private reply"},
	})

	svc := newTestService(t, store)
	res, err := svc.ImportREP(rep, ImportOptions{Handle: "tester"})
	if err != nil {
		t.Fatalf("ImportREP: %v", err)
	}
	if res.Posted != 1 {
		t.Errorf("want posted=1, got %+v", res)
	}
	if len(store.privPosted) != 1 {
		t.Fatalf("want 1 private post, got %d", len(store.privPosted))
	}
	if len(store.posted) != 0 {
		t.Errorf("private reply must not go through the public path, got %d public posts", len(store.posted))
	}
	if store.privPosted[0].to != "friend" {
		t.Errorf("private post To: want 'friend', got %q", store.privPosted[0].to)
	}
}

func TestImportREP_PublicConferenceRoutesToPublic(t *testing.T) {
	store := newFakeStore()
	store.addArea(&message.MessageArea{ID: 5, Tag: "GENERAL", Name: "General"})

	rep := makeREP(t, "VISION3", []qwk.PacketMessage{
		{Conference: 5, Number: 1, To: "All", Subject: "Hi", DateTime: time.Now(), Body: "public reply"},
	})

	svc := newTestService(t, store)
	res, err := svc.ImportREP(rep, ImportOptions{Handle: "tester"})
	if err != nil {
		t.Fatalf("ImportREP: %v", err)
	}
	if res.Posted != 1 || len(store.posted) != 1 || len(store.privPosted) != 0 {
		t.Errorf("public reply should use AddMessage: res=%+v public=%d priv=%d", res, len(store.posted), len(store.privPosted))
	}
}

func TestImportREP_UnmappedNumberFallsBackToPublic(t *testing.T) {
	store := newFakeStore()
	store.addArea(&message.MessageArea{ID: 5, Tag: "GENERAL", Name: "General"})

	// Pre-seed a map that assigns GENERAL a number (9) different from its ID (5),
	// so a REP using the old area.ID (5) misses the map and exercises the
	// GetAreaByID fallback.
	dir := t.TempDir()
	preMap := `[{"qwk_number":9,"area_tag":"GENERAL","kind":"public"}]` + "\n"
	if err := os.WriteFile(filepath.Join(dir, "qwk_conferences.json"), []byte(preMap), 0o644); err != nil {
		t.Fatal(err)
	}

	rep := makeREP(t, "VISION3", []qwk.PacketMessage{
		{Conference: 5, Number: 1, To: "All", Subject: "Hi", DateTime: time.Now(), Body: "legacy-numbered reply"},
	})

	svc := New(store, "VISION3", "ViSiON/3 BBS", "SysOp", dir)
	res, err := svc.ImportREP(rep, ImportOptions{Handle: "tester"})
	if err != nil {
		t.Fatalf("ImportREP: %v", err)
	}
	if res.Posted != 1 || len(store.posted) != 1 {
		t.Errorf("unmapped number should fall back to GetAreaByID and post public: res=%+v public=%d", res, len(store.posted))
	}
}
