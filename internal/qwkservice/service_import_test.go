package qwkservice

import (
	"fmt"
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

	svc := New(store, "VISION3", "ViSiON/3 BBS", "SysOp")
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

	svc := New(store, "VISION3", "ViSiON/3 BBS", "SysOp")
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

	svc := New(store, "VISION3", "ViSiON/3 BBS", "SysOp")
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

	svc := New(store, "VISION3", "ViSiON/3 BBS", "SysOp")
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
	svc := New(store, "VISION3", "ViSiON/3 BBS", "SysOp")
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

	svc := New(store, "VISION3", "ViSiON/3 BBS", "SysOp")
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
	svc := New(store, "VISION3", "ViSiON/3 BBS", "SysOp")
	_, err := svc.ImportREP([]byte("not a zip"), ImportOptions{Handle: "tester"})
	if err == nil {
		t.Fatal("expected error for a non-zip REP packet")
	}
}
